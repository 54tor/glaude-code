package main

// Partages de session par liens invités : par invitation, un jeton à usage unique puis un cookie réservé. Seuls les hachages sont stockés.

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	shareSeenSave = 10 * time.Second
	shareOnline   = 30 * time.Second
	shareTitleMax = 120
	inviteMax     = 10
	inviteNameMax = 24
	shareTextMax  = 200 // texte d'un message d'invité dans GET /v1/shares
)

// Un partage par session, porteur de N invitations ; chaque invitation a son lien, son cookie et son mode.
type Share struct {
	Session   string   `json:"session"`
	Dir       string   `json:"dir"`
	Title     string   `json:"title"`
	CreatedAt int64    `json:"createdAt"`
	Seq       int      `json:"seq"`
	Invites   []Invite `json:"invites"`
}

type Invite struct {
	ID         string `json:"id"`
	Seq        int    `json:"seq"`
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	TokenHash  string `json:"tokenHash"`
	CookieHash string `json:"cookieHash"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeen   int64  `json:"lastSeen"`
}

// defaultInviteMode : une invitation naît en lecture seule ; le mode écriture se donne ensuite, explicitement.
var defaultInviteMode = "ro"

// label : « Invité » reste fixe (pas de L) : c'est aussi le mot de l'étiquette envoyée à Claude.
func (invitation Invite) label() string {
	if invitation.Name != "" {
		return invitation.Name
	}
	return "Invité " + strconv.Itoa(invitation.Seq)
}

func (share *Share) inv(inviteID string) int {
	return slices.IndexFunc(share.Invites, func(candidate Invite) bool { return candidate.ID == inviteID })
}

func (share Share) invite(inviteID string) (Invite, bool) {
	if inviteIndex := share.inv(inviteID); inviteIndex >= 0 {
		return share.Invites[inviteIndex], true
	}
	return Invite{}, false
}

// clone : une copie rendue hors du verrou ne doit pas partager le tableau des invitations.
func (share *Share) clone() Share {
	copied := *share
	copied.Invites = slices.Clone(share.Invites)
	return copied
}

type shareStore struct {
	mu        sync.Mutex
	dir       string
	m         map[string]*Share
	lastSaved time.Time
}

var (
	errShareNone  = errors.New("aucun partage")
	errInviteNone = errors.New("aucune invitation")
	errInviteMax  = errors.New("trop d'invitations")
)

var (
	shareSt  *shareStore
	shareDir = cfgDir // les tests pointent ailleurs
	// shareOpen : la session tourne (tmux) ; sinon les messages de ses invités attendent son ouverture.
	shareOpen = func(id string) bool { _, open := sessionStatus(id); return open }
)

func newSecret() string {
	var randomBytes [32]byte
	rand.Read(randomBytes[:])
	return base64.RawURLEncoding.EncodeToString(randomBytes[:])
}

func hashSecret(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:])
}

// shareHandle : désigne le partage d'une session (nom du cookie invité, adresse /guest/<poignée>) ; jamais un secret.
func shareHandle(session string) string {
	digest := sha256.Sum256([]byte("gld-share-handle\x00" + session))
	return hex.EncodeToString(digest[:])[:16]
}

// inviteName : le nom entre dans l'étiquette « [Invité <nom>] » envoyée à Claude ; ni crochet, ni saut de ligne, ni caractère
// de format ne doit pouvoir en sortir ou maquiller l'auteur.
func inviteName(name string) string {
	name = strings.Map(func(character rune) rune {
		switch {
		case unicode.IsSpace(character):
			return ' '
		case unicode.IsLetter(character), unicode.IsDigit(character), strings.ContainsRune("-_.'", character):
			return character
		}
		return -1
	}, name)
	runes := []rune(strings.Join(strings.Fields(name), " "))
	if len(runes) > inviteNameMax {
		runes = runes[:inviteNameMax]
	}
	return strings.TrimSpace(string(runes))
}

// shareTitle : affiché à l'invité ; sans contrôle ni caractère de format (surcharges bidi, largeur nulle) qui maquilleraient
// le titre, espaces réduites à une seule.
func shareTitle(title string) string {
	title = strings.Map(func(character rune) rune {
		switch {
		case unicode.IsSpace(character):
			return ' '
		case unicode.IsControl(character), unicode.Is(unicode.Cf, character):
			return -1
		}
		return character
	}, title)
	runes := []rune(strings.Join(strings.Fields(title), " "))
	if len(runes) > shareTitleMax {
		runes = runes[:shareTitleMax]
	}
	return strings.TrimSpace(string(runes))
}

func hashEq(storedHash, candidateHash string) bool {
	return storedHash != "" && subtle.ConstantTimeCompare([]byte(storedHash), []byte(candidateHash)) == 1
}

// Un fichier existant mais illisible fait échouer l'ouverture : il n'est jamais écrasé.
func openShares(dir string) (*shareStore, error) {
	store := &shareStore{dir: dir, m: map[string]*Share{}}
	content, err := readTrustedIn(dir, filepath.Join(dir, "shares.json"))
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if decodeErr := json.Unmarshal(content, &store.m); decodeErr != nil {
		return nil, decodeErr
	}
	if store.m == nil {
		store.m = map[string]*Share{}
	}
	// Ancien format (un jeton par partage, sans invitations) : ignoré, jamais migré.
	for session, share := range store.m {
		if share == nil || len(share.Invites) == 0 {
			delete(store.m, session)
		}
	}
	return store, nil
}

func (store *shareStore) save() error {
	content, err := json.Marshal(store.m)
	if err != nil {
		return err
	}
	store.lastSaved = time.Now()
	return rWriteAtomic(store.dir, filepath.Join(store.dir, "shares.json"), content, 0o600)
}

// CreateInvite échoue fermé : si l'écriture échoue, le partage est rétabli tel quel et aucun jeton n'est rendu.
func (store *shareStore) CreateInvite(session, dir, title, name string) (string, Invite, error) {
	token := newSecret()
	var inviteID [8]byte
	rand.Read(inviteID[:])
	now := time.Now().Unix()
	store.mu.Lock()
	defer store.mu.Unlock()
	share, hadShare := store.m[session]
	var previous Share
	if hadShare {
		if len(share.Invites) >= inviteMax {
			return "", Invite{}, errInviteMax
		}
		previous = share.clone()
	} else {
		share = &Share{Session: session, CreatedAt: now}
		store.m[session] = share
	}
	if dir != "" {
		share.Dir = dir
	}
	if cleanTitle := shareTitle(title); cleanTitle != "" {
		share.Title = cleanTitle
	}
	share.Seq++
	invitation := Invite{ID: hex.EncodeToString(inviteID[:]), Seq: share.Seq, Name: inviteName(name), Mode: defaultInviteMode, TokenHash: hashSecret(token), CreatedAt: now}
	share.Invites = append(share.Invites, invitation)
	if err := store.save(); err != nil {
		if hadShare {
			*share = previous
		} else {
			delete(store.m, session)
		}
		return "", Invite{}, err
	}
	return token, invitation, nil
}

// edit applique change à l'invitation et rétablit l'ancienne si l'écriture échoue.
func (store *shareStore) edit(session, invite string, change func(*Invite)) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	share, found := store.m[session]
	if !found {
		return errInviteNone
	}
	inviteIndex := share.inv(invite)
	if inviteIndex < 0 {
		return errInviteNone
	}
	previous := share.Invites[inviteIndex]
	change(&share.Invites[inviteIndex])
	if err := store.save(); err != nil {
		share.Invites[inviteIndex] = previous
		return err
	}
	return nil
}

// Regen : nouveau jeton ; l'ancien lien et le cookie de cette invitation meurent, mode et nom restent. Un lien régénéré est
// souvent un lien fuité : ses éléments encore en file partent avec lui (pas ceux en cours d'envoi). Une purge non écrite
// reparaîtrait au redémarrage : l'erreur est rendue (le jeton reste changé, la purge tient en mémoire) pour que l'hôte réessaie.
func (store *shareStore) Regen(session, invite string) (string, error) {
	token := newSecret()
	err := store.edit(session, invite, func(invitation *Invite) { invitation.TokenHash, invitation.CookieHash = hashSecret(token), "" })
	if err != nil {
		return "", err
	}
	if queueSt != nil {
		if _, purgeErr := queueSt.RemoveByGuest(session, invite, true); purgeErr != nil {
			journal("partages : purge de la file après régénération non écrite : %v", purgeErr)
			return token, purgeErr
		}
	}
	return token, nil
}

func (store *shareStore) SetInviteMode(session, invite, mode string) error {
	return store.edit(session, invite, func(invitation *Invite) { invitation.Mode = mode })
}

func (store *shareStore) Redeem(token string) (string, Share, Invite, bool) {
	tokenHash := hashSecret(token)
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, share := range store.m {
		for inviteIndex := range share.Invites {
			invitation := &share.Invites[inviteIndex]
			if invitation.CookieHash != "" || !hashEq(invitation.TokenHash, tokenHash) {
				continue
			}
			cookie := newSecret()
			invitation.CookieHash, invitation.LastSeen = hashSecret(cookie), time.Now().Unix()
			if store.save() != nil {
				invitation.CookieHash = ""
				return "", Share{}, Invite{}, false
			}
			return cookie, share.clone(), *invitation, true
		}
	}
	return "", Share{}, Invite{}, false
}

// Reopened : le jeton déjà échangé, présenté avec le cookie de son invitation (même navigateur), rend la session partagée.
func (store *shareStore) Reopened(token, cookie string) (string, bool) {
	tokenHash, cookieHash := hashSecret(token), hashSecret(cookie)
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, share := range store.m {
		for _, invitation := range share.Invites {
			if invitation.CookieHash != "" && hashEq(invitation.TokenHash, tokenHash) && hashEq(invitation.CookieHash, cookieHash) {
				return share.Session, true
			}
		}
	}
	return "", false
}

func (store *shareStore) ByCookie(cookie string) (Share, Invite, bool) {
	return store.byCookie(cookie, true)
}

// ByCookieQuiet : sans toucher LastSeen ; l'hôte ne doit voir « connecté » que sur une vraie requête invitée.
func (store *shareStore) ByCookieQuiet(cookie string) (Share, Invite, bool) {
	return store.byCookie(cookie, false)
}

func (store *shareStore) byCookie(cookie string, touch bool) (Share, Invite, bool) {
	cookieHash := hashSecret(cookie)
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, share := range store.m {
		for inviteIndex := range share.Invites {
			invitation := &share.Invites[inviteIndex]
			if !hashEq(invitation.CookieHash, cookieHash) {
				continue
			}
			if touch {
				now := time.Now()
				invitation.LastSeen = now.Unix()
				if now.Sub(store.lastSaved) >= shareSeenSave {
					if err := store.save(); err != nil {
						journal("partages : écriture de LastSeen : %v", err)
					}
				}
			}
			return share.clone(), *invitation, true
		}
	}
	return Share{}, Invite{}, false
}

func (store *shareStore) Get(session string) (Share, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if share, found := store.m[session]; found {
		return share.clone(), true
	}
	return Share{}, false
}

// All : les partages, du plus ancien au plus récent.
func (store *shareStore) All() []Share {
	store.mu.Lock()
	shares := make([]Share, 0, len(store.m))
	for _, share := range store.m {
		shares = append(shares, share.clone())
	}
	store.mu.Unlock()
	slices.SortFunc(shares, func(left, right Share) int {
		return cmp.Or(cmp.Compare(left.CreatedAt, right.CreatedAt), strings.Compare(left.Session, right.Session))
	})
	return shares
}

// Revoke : comme End, la révocation en mémoire tient même si l'écriture échoue ; le dernier invité parti, le partage disparaît.
func (store *shareStore) Revoke(session, invite string) error {
	store.mu.Lock()
	share, found := store.m[session]
	inviteIndex := -1
	if found {
		inviteIndex = share.inv(invite)
	}
	if inviteIndex < 0 {
		store.mu.Unlock()
		return errInviteNone
	}
	share.Invites = slices.Delete(share.Invites, inviteIndex, inviteIndex+1)
	if len(share.Invites) == 0 {
		delete(store.m, session)
	}
	err := store.save()
	store.mu.Unlock()
	if queueSt != nil {
		queueSt.RemoveByGuest(session, invite, false)
	}
	return err
}

// End : la révocation en mémoire n'est jamais annulée, même si l'écriture échoue (l'erreur est rendue à l'appelant).
func (store *shareStore) End(session string) error {
	store.mu.Lock()
	_, found := store.m[session]
	if !found {
		store.mu.Unlock()
		return errShareNone
	}
	delete(store.m, session)
	err := store.save()
	store.mu.Unlock()
	if queueSt != nil {
		queueSt.RemoveByGuest(session, "", false)
	}
	return err
}

type shareQItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	State string `json:"state"`
	At    int64  `json:"at"`
}

type shareInviteView struct {
	ID       string       `json:"id"`
	Seq      int          `json:"seq"`
	Name     string       `json:"name"`
	Label    string       `json:"label"`
	Mode     string       `json:"mode"`
	State    string       `json:"state"`
	LastSeen int64        `json:"lastSeen"`
	Redeemed bool         `json:"redeemed"`
	Queue    []shareQItem `json:"queue"`
}

type shareView struct {
	ID      string            `json:"id"`
	Dir     string            `json:"dir"`
	Title   string            `json:"title"`
	Handle  string            `json:"handle"`
	Open    bool              `json:"open"`
	Invites []shareInviteView `json:"invites"`
}

// sharesView : vue d'ensemble de l'hôte ; seuls les éléments d'invités figurent dans les files, texte tronqué.
func sharesView() []shareView {
	now := time.Now()
	views := []shareView{}
	for _, share := range shareSt.All() {
		var items []QItem
		if queueSt != nil {
			items = queueSt.List(share.Session)
		}
		view := shareView{ID: share.Session, Dir: share.Dir, Title: share.Title, Handle: shareHandle(share.Session), Open: shareOpen(share.Session), Invites: []shareInviteView{}}
		for _, invitation := range share.Invites {
			inviteView := shareInviteView{ID: invitation.ID, Seq: invitation.Seq, Name: invitation.Name, Label: invitation.label(), Mode: invitation.Mode, LastSeen: invitation.LastSeen, Redeemed: invitation.CookieHash != "", Queue: []shareQItem{}}
			switch {
			case !inviteView.Redeemed:
				inviteView.State = "pending"
			case now.Sub(time.Unix(invitation.LastSeen, 0)) < shareOnline:
				inviteView.State = "connected"
			default:
				inviteView.State = "away"
			}
			for _, queueItem := range items {
				if queueItem.Author == "guest" && queueItem.Guest == invitation.ID {
					textRunes := []rune(queueItem.Text)
					inviteView.Queue = append(inviteView.Queue, shareQItem{ID: queueItem.ID, Text: string(textRunes[:min(len(textRunes), shareTextMax)]), State: queueItem.State, At: queueItem.At})
				}
			}
			view.Invites = append(view.Invites, inviteView)
		}
		views = append(views, view)
	}
	return views
}

func routesShare(mux *http.ServeMux) {
	store, err := openShares(shareDir())
	if err != nil {
		journal("partages : ouverture impossible : %v", err)
		store = nil
	}
	shareSt = store
	fail := func(responseWriter http.ResponseWriter, status int, code, key string) {
		writeErr(responseWriter, status, code, L(key))
	}
	type shareRequest struct{ ID, Dir, Title, Name, Mode string }
	// body : la session vient du corps (ou de ?id= pour un DELETE) ; withInvite exige l'invitation du chemin (16 hex).
	body := func(responseWriter http.ResponseWriter, request *http.Request, withInvite bool) (parsed shareRequest, invite string, isValid bool) {
		if request.Method == http.MethodDelete {
			parsed.ID = request.URL.Query().Get("id")
		} else if !decodeBody(responseWriter, request, &parsed, 4<<10) {
			return parsed, "", false
		}
		if shareValue := request.PathValue("share"); shareValue != "" {
			parsed.ID = shareValue
		}
		invite = request.PathValue("invite")
		if !idRe.MatchString(parsed.ID) || withInvite && !validHandle(invite) {
			fail(responseWriter, 400, "bad_request", "srv.http_util.requeteInvalide")
			return parsed, "", false
		}
		if shareSt == nil {
			fail(responseWriter, 503, "unavailable", "srv.share.indisponible")
			return parsed, "", false
		}
		return parsed, invite, true
	}
	storeErr := func(responseWriter http.ResponseWriter, operationErr error) bool {
		switch {
		case operationErr == nil:
			return false
		case errors.Is(operationErr, errInviteNone):
			fail(responseWriter, 404, "not_found", "srv.share.invitationIntrouvable")
		case errors.Is(operationErr, errShareNone):
			fail(responseWriter, 404, "not_found", "srv.share.introuvable")
		case errors.Is(operationErr, errInviteMax):
			fail(responseWriter, 409, "conflict", "srv.share.tropDInvitations")
		default:
			fail(responseWriter, 500, "internal", "srv.share.indisponible")
		}
		return true
	}
	mux.HandleFunc("POST /v1/invites", func(responseWriter http.ResponseWriter, request *http.Request) {
		parsed, _, isValid := body(responseWriter, request, false)
		if !isValid {
			return
		}
		if _, found := transcript(parsed.Dir, parsed.ID); !found {
			fail(responseWriter, 404, "not_found", "srv.share.sessionIntrouvable")
			return
		}
		token, invitation, createErr := shareSt.CreateInvite(parsed.ID, parsed.Dir, parsed.Title, parsed.Name)
		if storeErr(responseWriter, createErr) {
			return
		}
		writeJSON(responseWriter, 201, map[string]any{"token": token, "invite": map[string]any{"id": invitation.ID, "seq": invitation.Seq, "label": invitation.label()}})
	})
	mux.HandleFunc("PUT /v1/invites/{invite}/token", func(responseWriter http.ResponseWriter, request *http.Request) {
		parsed, inviteID, isValid := body(responseWriter, request, true)
		if !isValid {
			return
		}
		token, regenErr := shareSt.Regen(parsed.ID, inviteID)
		if storeErr(responseWriter, regenErr) {
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"token": token})
	})
	mux.HandleFunc("PATCH /v1/invites/{invite}", func(responseWriter http.ResponseWriter, request *http.Request) {
		parsed, inviteID, isValid := body(responseWriter, request, true)
		if !isValid {
			return
		}
		if parsed.Mode != "rw" && parsed.Mode != "ro" {
			fail(responseWriter, 400, "bad_request", "srv.http_util.requeteInvalide")
			return
		}
		if storeErr(responseWriter, shareSt.SetInviteMode(parsed.ID, inviteID, parsed.Mode)) {
			return
		}
		if parsed.Mode == "ro" && queueSt != nil {
			if _, purgeErr := queueSt.RemoveByGuest(parsed.ID, inviteID, true); purgeErr != nil {
				fail(responseWriter, 500, "internal", "srv.share.indisponible")
				return
			}
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /v1/invites/{invite}", func(responseWriter http.ResponseWriter, request *http.Request) {
		parsed, inviteID, isValid := body(responseWriter, request, true)
		if !isValid || storeErr(responseWriter, shareSt.Revoke(parsed.ID, inviteID)) {
			return
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /v1/shares/{share}", func(responseWriter http.ResponseWriter, request *http.Request) {
		parsed, _, isValid := body(responseWriter, request, false)
		if !isValid || storeErr(responseWriter, shareSt.End(parsed.ID)) {
			return
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/shares", func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Cache-Control", "no-store")
		if shareSt == nil {
			fail(responseWriter, 503, "unavailable", "srv.share.indisponible")
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"shares": sharesView()})
	})
}
