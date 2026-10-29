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
	"io"
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
	shareTextMax  = 200 // texte d'un message d'invité dans GET /shares
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

// label : « Invité » reste fixe (pas de L) : c'est aussi le mot de l'étiquette envoyée à Claude.
func (i Invite) label() string {
	if i.Name != "" {
		return i.Name
	}
	return "Invité " + strconv.Itoa(i.Seq)
}

func (sh *Share) inv(id string) int {
	return slices.IndexFunc(sh.Invites, func(i Invite) bool { return i.ID == id })
}

func (sh Share) invite(id string) (Invite, bool) {
	if i := sh.inv(id); i >= 0 {
		return sh.Invites[i], true
	}
	return Invite{}, false
}

// clone : une copie rendue hors du verrou ne doit pas partager le tableau des invitations.
func (sh *Share) clone() Share {
	c := *sh
	c.Invites = slices.Clone(sh.Invites)
	return c
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
	var b [32]byte
	rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func hashSecret(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// shareHandle : désigne le partage d'une session (nom du cookie invité, adresse /guest/<poignée>) ; jamais un secret.
func shareHandle(session string) string {
	h := sha256.Sum256([]byte("gld-share-handle\x00" + session))
	return hex.EncodeToString(h[:])[:16]
}

// inviteName : le nom entre dans l'étiquette « [Invité <nom>] » envoyée à Claude ; ni crochet, ni saut de ligne, ni caractère
// de format ne doit pouvoir en sortir ou maquiller l'auteur.
func inviteName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsLetter(r), unicode.IsDigit(r), strings.ContainsRune("-_.'", r):
			return r
		}
		return -1
	}, s)
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > inviteNameMax {
		r = r[:inviteNameMax]
	}
	return strings.TrimSpace(string(r))
}

// shareTitle : affiché à l'invité ; sans contrôle ni caractère de format (surcharges bidi, largeur nulle) qui maquilleraient
// le titre, espaces réduites à une seule.
func shareTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > shareTitleMax {
		r = r[:shareTitleMax]
	}
	return strings.TrimSpace(string(r))
}

func hashEq(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// Un fichier existant mais illisible fait échouer l'ouverture : il n'est jamais écrasé.
func openShares(dir string) (*shareStore, error) {
	s := &shareStore{dir: dir, m: map[string]*Share{}}
	b, err := readTrustedIn(dir, filepath.Join(dir, "shares.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.m); err != nil {
		return nil, err
	}
	if s.m == nil {
		s.m = map[string]*Share{}
	}
	// Ancien format (un jeton par partage, sans invitations) : ignoré, jamais migré.
	for k, v := range s.m {
		if v == nil || len(v.Invites) == 0 {
			delete(s.m, k)
		}
	}
	return s, nil
}

func (s *shareStore) save() error {
	b, err := json.Marshal(s.m)
	if err != nil {
		return err
	}
	s.lastSaved = time.Now()
	return rWriteAtomic(s.dir, filepath.Join(s.dir, "shares.json"), b, 0o600)
}

// CreateInvite échoue fermé : si l'écriture échoue, le partage est rétabli tel quel et aucun jeton n'est rendu.
func (s *shareStore) CreateInvite(session, dir, title, name string) (string, Invite, error) {
	tok := newSecret()
	var id [8]byte
	rand.Read(id[:])
	now := time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, had := s.m[session]
	var prev Share
	if had {
		if len(sh.Invites) >= inviteMax {
			return "", Invite{}, errInviteMax
		}
		prev = sh.clone()
	} else {
		sh = &Share{Session: session, CreatedAt: now}
		s.m[session] = sh
	}
	if dir != "" {
		sh.Dir = dir
	}
	if t := shareTitle(title); t != "" {
		sh.Title = t
	}
	sh.Seq++
	inv := Invite{ID: hex.EncodeToString(id[:]), Seq: sh.Seq, Name: inviteName(name), Mode: "rw", TokenHash: hashSecret(tok), CreatedAt: now}
	sh.Invites = append(sh.Invites, inv)
	if err := s.save(); err != nil {
		if had {
			*sh = prev
		} else {
			delete(s.m, session)
		}
		return "", Invite{}, err
	}
	return tok, inv, nil
}

// edit applique f à l'invitation et rétablit l'ancienne si l'écriture échoue.
func (s *shareStore) edit(session, invite string, f func(*Invite)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.m[session]
	if !ok {
		return errInviteNone
	}
	i := sh.inv(invite)
	if i < 0 {
		return errInviteNone
	}
	old := sh.Invites[i]
	f(&sh.Invites[i])
	if err := s.save(); err != nil {
		sh.Invites[i] = old
		return err
	}
	return nil
}

// Regen : nouveau jeton ; l'ancien lien et le cookie de cette invitation meurent, mode et nom restent. Un lien régénéré est
// souvent un lien fuité : ses éléments encore en file partent avec lui (pas ceux en cours d'envoi). Une purge non écrite
// reparaîtrait au redémarrage : l'erreur est rendue (le jeton reste changé, la purge tient en mémoire) pour que l'hôte réessaie.
func (s *shareStore) Regen(session, invite string) (string, error) {
	tok := newSecret()
	err := s.edit(session, invite, func(in *Invite) { in.TokenHash, in.CookieHash = hashSecret(tok), "" })
	if err != nil {
		return "", err
	}
	if queueSt != nil {
		if _, err := queueSt.RemoveByGuest(session, invite, true); err != nil {
			journal("partages : purge de la file après régénération non écrite : %v", err)
			return tok, err
		}
	}
	return tok, nil
}

func (s *shareStore) SetInviteMode(session, invite, mode string) error {
	return s.edit(session, invite, func(in *Invite) { in.Mode = mode })
}

func (s *shareStore) Redeem(token string) (string, Share, Invite, bool) {
	h := hashSecret(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sh := range s.m {
		for i := range sh.Invites {
			in := &sh.Invites[i]
			if in.CookieHash != "" || !hashEq(in.TokenHash, h) {
				continue
			}
			c := newSecret()
			in.CookieHash, in.LastSeen = hashSecret(c), time.Now().Unix()
			if s.save() != nil {
				in.CookieHash = ""
				return "", Share{}, Invite{}, false
			}
			return c, sh.clone(), *in, true
		}
	}
	return "", Share{}, Invite{}, false
}

// Reopened : le jeton déjà échangé, présenté avec le cookie de son invitation (même navigateur), rend la session partagée.
func (s *shareStore) Reopened(token, cookie string) (string, bool) {
	th, ch := hashSecret(token), hashSecret(cookie)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sh := range s.m {
		for _, in := range sh.Invites {
			if in.CookieHash != "" && hashEq(in.TokenHash, th) && hashEq(in.CookieHash, ch) {
				return sh.Session, true
			}
		}
	}
	return "", false
}

func (s *shareStore) ByCookie(cookie string) (Share, Invite, bool) { return s.byCookie(cookie, true) }

// ByCookieQuiet : sans toucher LastSeen ; l'hôte ne doit voir « connecté » que sur une vraie requête invitée.
func (s *shareStore) ByCookieQuiet(cookie string) (Share, Invite, bool) {
	return s.byCookie(cookie, false)
}

func (s *shareStore) byCookie(cookie string, touch bool) (Share, Invite, bool) {
	h := hashSecret(cookie)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sh := range s.m {
		for i := range sh.Invites {
			in := &sh.Invites[i]
			if !hashEq(in.CookieHash, h) {
				continue
			}
			if touch {
				now := time.Now()
				in.LastSeen = now.Unix()
				if now.Sub(s.lastSaved) >= shareSeenSave {
					if err := s.save(); err != nil {
						journal("partages : écriture de LastSeen : %v", err)
					}
				}
			}
			return sh.clone(), *in, true
		}
	}
	return Share{}, Invite{}, false
}

func (s *shareStore) Get(session string) (Share, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh, ok := s.m[session]; ok {
		return sh.clone(), true
	}
	return Share{}, false
}

// All : les partages, du plus ancien au plus récent.
func (s *shareStore) All() []Share {
	s.mu.Lock()
	out := make([]Share, 0, len(s.m))
	for _, sh := range s.m {
		out = append(out, sh.clone())
	}
	s.mu.Unlock()
	slices.SortFunc(out, func(a, b Share) int {
		return cmp.Or(cmp.Compare(a.CreatedAt, b.CreatedAt), strings.Compare(a.Session, b.Session))
	})
	return out
}

// Revoke : comme End, la révocation en mémoire tient même si l'écriture échoue ; le dernier invité parti, le partage disparaît.
func (s *shareStore) Revoke(session, invite string) error {
	s.mu.Lock()
	sh, ok := s.m[session]
	i := -1
	if ok {
		i = sh.inv(invite)
	}
	if i < 0 {
		s.mu.Unlock()
		return errInviteNone
	}
	sh.Invites = slices.Delete(sh.Invites, i, i+1)
	if len(sh.Invites) == 0 {
		delete(s.m, session)
	}
	err := s.save()
	s.mu.Unlock()
	if queueSt != nil {
		queueSt.RemoveByGuest(session, invite, false)
	}
	return err
}

// End : la révocation en mémoire n'est jamais annulée, même si l'écriture échoue (l'erreur est rendue à l'appelant).
func (s *shareStore) End(session string) error {
	s.mu.Lock()
	_, ok := s.m[session]
	if !ok {
		s.mu.Unlock()
		return errShareNone
	}
	delete(s.m, session)
	err := s.save()
	s.mu.Unlock()
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
	out := []shareView{}
	for _, sh := range shareSt.All() {
		var items []QItem
		if queueSt != nil {
			items = queueSt.List(sh.Session)
		}
		v := shareView{ID: sh.Session, Dir: sh.Dir, Title: sh.Title, Handle: shareHandle(sh.Session), Open: shareOpen(sh.Session), Invites: []shareInviteView{}}
		for _, in := range sh.Invites {
			iv := shareInviteView{ID: in.ID, Seq: in.Seq, Name: in.Name, Label: in.label(), Mode: in.Mode, LastSeen: in.LastSeen, Redeemed: in.CookieHash != "", Queue: []shareQItem{}}
			switch {
			case !iv.Redeemed:
				iv.State = "pending"
			case now.Sub(time.Unix(in.LastSeen, 0)) < shareOnline:
				iv.State = "connected"
			default:
				iv.State = "away"
			}
			for _, it := range items {
				if it.Author == "guest" && it.Guest == in.ID {
					t := []rune(it.Text)
					iv.Queue = append(iv.Queue, shareQItem{ID: it.ID, Text: string(t[:min(len(t), shareTextMax)]), State: it.State, At: it.At})
				}
			}
			v.Invites = append(v.Invites, iv)
		}
		out = append(out, v)
	}
	return out
}

func routesShare(mux *http.ServeMux) {
	st, err := openShares(shareDir())
	if err != nil {
		journal("partages : ouverture impossible : %v", err)
		st = nil
	}
	shareSt = st
	fail := func(w http.ResponseWriter, code int, key string) {
		writeJSON(w, code, map[string]any{"ok": false, "out": L(key)})
	}
	type req struct{ ID, Dir, Title, Name, Invite, Mode string }
	// body : withInvite exige un identifiant d'invitation (même format qu'une poignée : 16 hex).
	body := func(w http.ResponseWriter, r *http.Request, withInvite bool) (req, bool) {
		var q req
		if json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&q) != nil || !idRe.MatchString(q.ID) || withInvite && !validHandle(q.Invite) {
			fail(w, 400, "srv.http_util.requeteInvalide")
			return q, false
		}
		if shareSt == nil {
			fail(w, 503, "srv.share.indisponible")
			return q, false
		}
		return q, true
	}
	storeErr := func(w http.ResponseWriter, err error) bool {
		switch {
		case err == nil:
			return false
		case errors.Is(err, errInviteNone):
			fail(w, 404, "srv.share.invitationIntrouvable")
		case errors.Is(err, errShareNone):
			fail(w, 404, "srv.share.introuvable")
		case errors.Is(err, errInviteMax):
			fail(w, 409, "srv.share.tropDInvitations")
		default:
			fail(w, 500, "srv.share.indisponible")
		}
		return true
	}
	mux.HandleFunc("POST /invite-create", func(w http.ResponseWriter, r *http.Request) {
		q, ok := body(w, r, false)
		if !ok {
			return
		}
		if _, found := transcript(q.Dir, q.ID); !found {
			fail(w, 404, "srv.share.sessionIntrouvable")
			return
		}
		tok, in, err := shareSt.CreateInvite(q.ID, q.Dir, q.Title, q.Name)
		if storeErr(w, err) {
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "token": tok, "invite": map[string]any{"id": in.ID, "seq": in.Seq, "label": in.label()}})
	})
	mux.HandleFunc("POST /invite-regen", func(w http.ResponseWriter, r *http.Request) {
		q, ok := body(w, r, true)
		if !ok {
			return
		}
		tok, err := shareSt.Regen(q.ID, q.Invite)
		if storeErr(w, err) {
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "token": tok})
	})
	mux.HandleFunc("POST /invite-mode", func(w http.ResponseWriter, r *http.Request) {
		q, ok := body(w, r, true)
		if !ok {
			return
		}
		if q.Mode != "rw" && q.Mode != "ro" {
			fail(w, 400, "srv.http_util.requeteInvalide")
			return
		}
		if storeErr(w, shareSt.SetInviteMode(q.ID, q.Invite, q.Mode)) {
			return
		}
		if q.Mode == "ro" && queueSt != nil {
			if _, err := queueSt.RemoveByGuest(q.ID, q.Invite, true); err != nil {
				fail(w, 500, "srv.share.indisponible")
				return
			}
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /invite-revoke", func(w http.ResponseWriter, r *http.Request) {
		q, ok := body(w, r, true)
		if !ok || storeErr(w, shareSt.Revoke(q.ID, q.Invite)) {
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /share-end", func(w http.ResponseWriter, r *http.Request) {
		q, ok := body(w, r, false)
		if !ok || storeErr(w, shareSt.End(q.ID)) {
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /shares", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if shareSt == nil {
			fail(w, 503, "srv.share.indisponible")
			return
		}
		writeJSON(w, 200, map[string]any{"shares": sharesView()})
	})
}
