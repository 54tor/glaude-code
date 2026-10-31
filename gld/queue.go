package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	queueMax      = 50
	queueGuestMax = 30 // éléments d'invités par session, toutes invitations confondues : l'hôte garde toujours de la place
)

var (
	errQueueFull = errors.New("file pleine")
	errQueueText = errors.New("texte refusé")
	errQueueGone = errors.New("item absent ou déjà pris")
	errQueueBusy = errors.New("envoi en cours")
)

type QItem struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Author string `json:"author"`
	Guest  string `json:"guest,omitempty"` // invitation de l'auteur, seulement pour author == "guest"
	State  string `json:"state"`
	At     int64  `json:"at"`
	SentAt int64  `json:"sentAt,omitempty"`
}

type queueStore struct {
	mu   sync.Mutex
	dir  string
	sess map[string][]QItem
}

// Un fichier existant mais illisible ou corrompu fait échouer l'ouverture : il ne doit jamais être écrasé.
func openQueue(dir string) (*queueStore, error) {
	queue := &queueStore{dir: dir, sess: map[string][]QItem{}}
	content, err := readTrustedIn(dir, filepath.Join(dir, "queue.json"))
	if errors.Is(err, os.ErrNotExist) {
		return queue, nil
	}
	if err != nil {
		return nil, err
	}
	if decodeErr := json.Unmarshal(content, &queue.sess); decodeErr != nil {
		return nil, decodeErr
	}
	if queue.sess == nil { // un fichier « null » donnerait une carte nil : le prochain Add paniquerait
		queue.sess = map[string][]QItem{}
	}
	// Un envoi interrompu a pu atteindre la session : on ne le rejoue pas sans l'accord de l'hôte.
	for _, items := range queue.sess {
		for itemIndex := range items {
			if items[itemIndex].State == "sending" {
				items[itemIndex].State = "uncertain"
			}
		}
	}
	return queue, nil
}

func (queue *queueStore) save() error {
	content, err := json.Marshal(queue.sess)
	if err != nil {
		return err
	}
	return rWriteAtomic(queue.dir, filepath.Join(queue.dir, "queue.json"), content, 0o600)
}

// Add : un élément d'invité porte toujours son invitation (le vidage en tire l'étiquette) et laisse la place à la plus longue.
func (queue *queueStore) Add(session, author, guest, text string) (QItem, error) {
	if author != "guest" {
		guest = ""
	}
	isGuest := author == "guest"
	guestTextMaxLength := sendMax - guestLabelMax - len(followPrefix)
	isGuestTextRefused := isGuest && (guest == "" || len(text) > guestTextMaxLength)
	if checkSendText(text) != nil || strings.ContainsRune(text, 0) || isGuestTextRefused {
		return QItem{}, errQueueText
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.sess[session]) >= queueMax || isGuest && queue.guests(session) >= queueGuestMax {
		return QItem{}, errQueueFull
	}
	var randomID [8]byte
	rand.Read(randomID[:])
	newItem := QItem{
		ID:     hex.EncodeToString(randomID[:]),
		Text:   text,
		Author: author,
		Guest:  guest,
		State:  "queued",
		At:     time.Now().Unix(),
	}
	queue.sess[session] = append(queue.sess[session], newItem)
	if err := queue.save(); err != nil {
		queue.sess[session] = queue.sess[session][:len(queue.sess[session])-1]
		return QItem{}, err
	}
	return newItem, nil
}

func (queue *queueStore) guests(session string) int {
	guestCount := 0
	for _, queuedItem := range queue.sess[session] {
		if queuedItem.Author == "guest" {
			guestCount++
		}
	}
	return guestCount
}

func (queue *queueStore) List(session string) []QItem {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return append([]QItem{}, queue.sess[session]...)
}

func (queue *queueStore) find(session, itemID string) int {
	for itemIndex, queuedItem := range queue.sess[session] {
		if queuedItem.ID == itemID {
			return itemIndex
		}
	}
	return -1
}

// remove retire l'item et la clé de session devenue vide ; l'échec d'écriture est toléré (au pire l'item reparaît au redémarrage), contrairement à MarkSending dont la persistance garantit le au-plus-une-fois.
func (queue *queueStore) remove(session string, itemIndex int) QItem {
	items := queue.sess[session]
	removedItem := items[itemIndex]
	if len(items) == 1 {
		delete(queue.sess, session)
	} else {
		queue.sess[session] = append(items[:itemIndex:itemIndex], items[itemIndex+1:]...)
	}
	queue.save()
	return removedItem
}

// Remove : retrait demandé par un utilisateur. Un item « sending » est refusé sous le verrou du magasin : il est peut-être déjà parti.
func (queue *queueStore) Remove(session, itemID string) (QItem, error) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	itemIndex := queue.find(session, itemID)
	if itemIndex < 0 {
		return QItem{}, errQueueGone
	}
	if queue.sess[session][itemIndex].State == "sending" {
		return QItem{}, errQueueBusy
	}
	return queue.remove(session, itemIndex), nil
}

func (queue *queueStore) Retry(session, itemID string) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	itemIndex := queue.find(session, itemID)
	if itemIndex < 0 || queue.sess[session][itemIndex].State != "uncertain" {
		return false
	}
	queue.sess[session][itemIndex].State = "queued"
	queue.sess[session][itemIndex].SentAt = 0
	queue.save()
	return true
}

func (queue *queueStore) Head(session string) (QItem, bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for _, queuedItem := range queue.sess[session] {
		switch queuedItem.State {
		case "queued":
			return queuedItem, true
		case "sending", "uncertain":
			return QItem{}, false
		}
	}
	return QItem{}, false
}

// MarkSending : compare-and-set queued -> sending. L'écriture précède l'envoi : si elle échoue, l'état revient à queued et
// l'appelant ne doit pas envoyer, sinon un crash rejouerait le message.
func (queue *queueStore) MarkSending(session, itemID string) error {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	itemIndex := queue.find(session, itemID)
	if itemIndex < 0 || queue.sess[session][itemIndex].State != "queued" {
		return errQueueGone
	}
	queue.sess[session][itemIndex].State = "sending"
	queue.sess[session][itemIndex].SentAt = time.Now().Unix()
	if err := queue.save(); err != nil {
		queue.sess[session][itemIndex].State = "queued"
		queue.sess[session][itemIndex].SentAt = 0
		return err
	}
	return nil
}

// Done : le drain retire l'item une fois l'envoi fini, quel que soit son état.
func (queue *queueStore) Done(session, itemID string) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if itemIndex := queue.find(session, itemID); itemIndex >= 0 {
		queue.remove(session, itemIndex)
	}
}

func (queue *queueStore) Sessions() []string {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	sessionIDs := make([]string, 0, len(queue.sess))
	for sessionID := range queue.sess {
		sessionIDs = append(sessionIDs, sessionID)
	}
	return sessionIDs
}

// Requeue : sending -> queued après un envoi refusé (rien n'est parti, contrairement à « uncertain »).
func (queue *queueStore) Requeue(session, itemID string) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	itemIndex := queue.find(session, itemID)
	if itemIndex < 0 || queue.sess[session][itemIndex].State != "sending" {
		return false
	}
	queue.sess[session][itemIndex].State = "queued"
	queue.sess[session][itemIndex].SentAt = 0
	queue.save()
	return true
}

// RemoveByGuest retire les items d'invités d'une session, ceux d'une invitation ou tous (invite vide, fin du partage) : tous
// à la révocation, seulement ceux encore « queued » au passage en lecture seule (un item « sending » est peut-être déjà parti).
// Le retrait en mémoire tient même si l'écriture échoue.
func (queue *queueStore) RemoveByGuest(session, invite string, queuedOnly bool) (int, error) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	var kept []QItem
	removedCount := 0
	for _, queuedItem := range queue.sess[session] {
		if queuedItem.Author == "guest" && (invite == "" || queuedItem.Guest == invite) && (!queuedOnly || queuedItem.State == "queued") {
			removedCount++
		} else {
			kept = append(kept, queuedItem)
		}
	}
	if removedCount == 0 {
		return 0, nil
	}
	if len(kept) == 0 {
		delete(queue.sess, session)
	} else {
		queue.sess[session] = kept
	}
	return removedCount, queue.save()
}
