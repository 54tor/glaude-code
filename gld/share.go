package main

// Partages de session par lien invité : un jeton à usage unique, puis un cookie réservé. Seuls les hachages sont stockés.

import (
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
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	shareSeenSave = 10 * time.Second
	shareOnline   = 30 * time.Second
	shareTitleMax = 120
)

type Share struct {
	Session    string `json:"session"`
	Dir        string `json:"dir"`
	Mode       string `json:"mode"`
	TokenHash  string `json:"tokenHash"`
	CookieHash string `json:"cookieHash"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeen   int64  `json:"lastSeen"`
	Title      string `json:"title"`
}

type shareStore struct {
	mu        sync.Mutex
	dir       string
	m         map[string]*Share
	lastSaved time.Time
}

var errShareNone = errors.New("aucun partage")

var (
	shareSt  *shareStore
	shareDir = cfgDir // les tests pointent ailleurs
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
	for k, v := range s.m {
		if v == nil {
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

// Create échoue fermé : si l'écriture échoue, l'ancien partage est rétabli et aucun jeton n'est rendu.
func (s *shareStore) Create(session, dir, title string) (string, error) {
	tok := newSecret()
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, had := s.m[session]
	s.m[session] = &Share{Session: session, Dir: dir, Mode: "rw", TokenHash: hashSecret(tok), CreatedAt: time.Now().Unix(), Title: shareTitle(title)}
	if err := s.save(); err != nil {
		if had {
			s.m[session] = prev
		} else {
			delete(s.m, session)
		}
		return "", err
	}
	return tok, nil
}

func (s *shareStore) Redeem(token string) (string, Share, bool) {
	h := hashSecret(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sh := range s.m {
		if sh.CookieHash == "" && hashEq(sh.TokenHash, h) {
			c := newSecret()
			sh.CookieHash = hashSecret(c)
			sh.LastSeen = time.Now().Unix()
			if s.save() != nil {
				sh.CookieHash = ""
				return "", Share{}, false
			}
			return c, *sh, true
		}
	}
	return "", Share{}, false
}

func (s *shareStore) ByCookie(cookie string) (Share, bool) { return s.byCookie(cookie, true) }

// ByCookieQuiet : sans toucher LastSeen ; l'hôte ne doit voir « connecté » que sur une vraie requête invitée.
func (s *shareStore) ByCookieQuiet(cookie string) (Share, bool) { return s.byCookie(cookie, false) }

func (s *shareStore) byCookie(cookie string, touch bool) (Share, bool) {
	h := hashSecret(cookie)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sh := range s.m {
		if hashEq(sh.CookieHash, h) {
			if !touch {
				return *sh, true
			}
			now := time.Now()
			sh.LastSeen = now.Unix()
			if now.Sub(s.lastSaved) >= shareSeenSave {
				if err := s.save(); err != nil {
					journal("partages : écriture de LastSeen : %v", err)
				}
			}
			return *sh, true
		}
	}
	return Share{}, false
}

func (s *shareStore) Get(session string) (Share, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh, ok := s.m[session]; ok {
		return *sh, true
	}
	return Share{}, false
}

func (s *shareStore) SetMode(session, mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.m[session]
	if !ok {
		return errShareNone
	}
	old := sh.Mode
	sh.Mode = mode
	if err := s.save(); err != nil {
		sh.Mode = old
		return err
	}
	return nil
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
		queueSt.RemoveByAuthor(session, "guest", false)
	}
	return err
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
	body := func(w http.ResponseWriter, r *http.Request) (struct{ ID, Dir, Mode, Title string }, bool) {
		var req struct{ ID, Dir, Mode, Title string }
		if json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req) != nil || !idRe.MatchString(req.ID) {
			fail(w, 400, "srv.http_util.requeteInvalide")
			return req, false
		}
		if shareSt == nil {
			fail(w, 503, "srv.share.indisponible")
			return req, false
		}
		return req, true
	}
	mux.HandleFunc("POST /share-create", func(w http.ResponseWriter, r *http.Request) {
		req, ok := body(w, r)
		if !ok {
			return
		}
		if _, found := transcript(req.Dir, req.ID); !found {
			fail(w, 404, "srv.share.sessionIntrouvable")
			return
		}
		tok, err := shareSt.Create(req.ID, req.Dir, req.Title)
		if err != nil {
			fail(w, 500, "srv.share.indisponible")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "token": tok})
	})
	mux.HandleFunc("GET /share-info", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if !idRe.MatchString(id) {
			fail(w, 400, "srv.http_util.requeteInvalide")
			return
		}
		if shareSt == nil {
			fail(w, 503, "srv.share.indisponible")
			return
		}
		sh, ok := shareSt.Get(id)
		writeJSON(w, 200, map[string]any{"shared": ok, "mode": sh.Mode,
			"connected": ok && time.Since(time.Unix(sh.LastSeen, 0)) < shareOnline})
	})
	mux.HandleFunc("POST /share-mode", func(w http.ResponseWriter, r *http.Request) {
		req, ok := body(w, r)
		if !ok {
			return
		}
		if req.Mode != "rw" && req.Mode != "ro" {
			fail(w, 400, "srv.http_util.requeteInvalide")
			return
		}
		switch err := shareSt.SetMode(req.ID, req.Mode); {
		case errors.Is(err, errShareNone):
			fail(w, 404, "srv.share.introuvable")
			return
		case err != nil:
			fail(w, 500, "srv.share.indisponible")
			return
		}
		if req.Mode == "ro" && queueSt != nil {
			if _, err := queueSt.RemoveByAuthor(req.ID, "guest", true); err != nil {
				fail(w, 500, "srv.share.indisponible")
				return
			}
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /share-end", func(w http.ResponseWriter, r *http.Request) {
		req, ok := body(w, r)
		if !ok {
			return
		}
		switch err := shareSt.End(req.ID); {
		case errors.Is(err, errShareNone):
			fail(w, 404, "srv.share.introuvable")
			return
		case err != nil:
			fail(w, 500, "srv.share.indisponible")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
}
