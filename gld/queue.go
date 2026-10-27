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

const queueMax = 50

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
	q := &queueStore{dir: dir, sess: map[string][]QItem{}}
	b, err := readTrustedIn(dir, filepath.Join(dir, "queue.json"))
	if errors.Is(err, os.ErrNotExist) {
		return q, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &q.sess); err != nil {
		return nil, err
	}
	// Un envoi interrompu a pu atteindre la session : on ne le rejoue pas sans l'accord de l'hôte.
	for _, l := range q.sess {
		for i := range l {
			if l[i].State == "sending" {
				l[i].State = "uncertain"
			}
		}
	}
	return q, nil
}

func (q *queueStore) save() error {
	b, err := json.Marshal(q.sess)
	if err != nil {
		return err
	}
	return rWriteAtomic(q.dir, filepath.Join(q.dir, "queue.json"), b, 0o600)
}

func (q *queueStore) Add(session, author, text string) (QItem, error) {
	if checkSendText(text) != nil || strings.ContainsRune(text, 0) {
		return QItem{}, errQueueText
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.sess[session]) >= queueMax {
		return QItem{}, errQueueFull
	}
	var r [8]byte
	rand.Read(r[:])
	it := QItem{ID: hex.EncodeToString(r[:]), Text: text, Author: author, State: "queued", At: time.Now().Unix()}
	q.sess[session] = append(q.sess[session], it)
	if err := q.save(); err != nil {
		q.sess[session] = q.sess[session][:len(q.sess[session])-1]
		return QItem{}, err
	}
	return it, nil
}

func (q *queueStore) List(session string) []QItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]QItem{}, q.sess[session]...)
}

func (q *queueStore) find(session, id string) int {
	for i, it := range q.sess[session] {
		if it.ID == id {
			return i
		}
	}
	return -1
}

// remove retire l'item et la clé de session devenue vide ; l'échec d'écriture est toléré (au pire l'item reparaît au redémarrage), contrairement à MarkSending dont la persistance garantit le au-plus-une-fois.
func (q *queueStore) remove(session string, i int) QItem {
	l := q.sess[session]
	it := l[i]
	if len(l) == 1 {
		delete(q.sess, session)
	} else {
		q.sess[session] = append(l[:i:i], l[i+1:]...)
	}
	q.save()
	return it
}

// Remove : retrait demandé par un utilisateur. Un item « sending » est refusé sous le verrou du magasin : il est peut-être déjà parti.
func (q *queueStore) Remove(session, itemID string) (QItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	i := q.find(session, itemID)
	if i < 0 {
		return QItem{}, errQueueGone
	}
	if q.sess[session][i].State == "sending" {
		return QItem{}, errQueueBusy
	}
	return q.remove(session, i), nil
}

func (q *queueStore) Retry(session, itemID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	i := q.find(session, itemID)
	if i < 0 || q.sess[session][i].State != "uncertain" {
		return false
	}
	q.sess[session][i].State = "queued"
	q.sess[session][i].SentAt = 0
	q.save()
	return true
}

func (q *queueStore) Head(session string) (QItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.sess[session] {
		switch it.State {
		case "queued":
			return it, true
		case "sending", "uncertain":
			return QItem{}, false
		}
	}
	return QItem{}, false
}

// MarkSending : compare-and-set queued -> sending. L'écriture précède l'envoi : si elle échoue, l'état revient à queued et
// l'appelant ne doit pas envoyer, sinon un crash rejouerait le message.
func (q *queueStore) MarkSending(session, itemID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	i := q.find(session, itemID)
	if i < 0 || q.sess[session][i].State != "queued" {
		return errQueueGone
	}
	q.sess[session][i].State = "sending"
	q.sess[session][i].SentAt = time.Now().Unix()
	if err := q.save(); err != nil {
		q.sess[session][i].State = "queued"
		q.sess[session][i].SentAt = 0
		return err
	}
	return nil
}

// Done : le drain retire l'item une fois l'envoi fini, quel que soit son état.
func (q *queueStore) Done(session, itemID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if i := q.find(session, itemID); i >= 0 {
		q.remove(session, i)
	}
}

func (q *queueStore) Sessions() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.sess))
	for s := range q.sess {
		out = append(out, s)
	}
	return out
}

// Requeue : sending -> queued après un envoi refusé (rien n'est parti, contrairement à « uncertain »).
func (q *queueStore) Requeue(session, itemID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	i := q.find(session, itemID)
	if i < 0 || q.sess[session][i].State != "sending" {
		return false
	}
	q.sess[session][i].State = "queued"
	q.sess[session][i].SentAt = 0
	q.save()
	return true
}

// RemoveByAuthor retire les items d'un auteur dans une session (fin de partage : les messages invités en file disparaissent).
func (q *queueStore) RemoveByAuthor(session, author string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	var keep []QItem
	n := 0
	for _, it := range q.sess[session] {
		if it.Author == author {
			n++
		} else {
			keep = append(keep, it)
		}
	}
	if n == 0 {
		return 0
	}
	if len(keep) == 0 {
		delete(q.sess, session)
	} else {
		q.sess[session] = keep
	}
	q.save()
	return n
}
