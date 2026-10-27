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
	"sync"
	"time"
)

const (
	shareSeenSave = 10 * time.Second
	shareOnline   = 30 * time.Second
)

type Share struct {
	Session    string `json:"session"`
	Dir        string `json:"dir"`
	Mode       string `json:"mode"`
	TokenHash  string `json:"tokenHash"`
	CookieHash string `json:"cookieHash"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeen   int64  `json:"lastSeen"`
}

type shareStore struct {
	mu        sync.Mutex
	dir       string
	m         map[string]*Share
	lastSaved time.Time
}

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

func (s *shareStore) Create(session, dir string) string {
	tok := newSecret()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[session] = &Share{Session: session, Dir: dir, Mode: "rw", TokenHash: hashSecret(tok), CreatedAt: time.Now().Unix()}
	s.save()
	return tok
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

func (s *shareStore) ByCookie(cookie string) (Share, bool) {
	h := hashSecret(cookie)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sh := range s.m {
		if hashEq(sh.CookieHash, h) {
			now := time.Now()
			sh.LastSeen = now.Unix()
			if now.Sub(s.lastSaved) >= shareSeenSave {
				s.save()
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

func (s *shareStore) SetMode(session, mode string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.m[session]
	if !ok {
		return false
	}
	sh.Mode = mode
	s.save()
	return true
}

func (s *shareStore) End(session string) bool {
	s.mu.Lock()
	_, ok := s.m[session]
	delete(s.m, session)
	if ok {
		s.save()
	}
	s.mu.Unlock()
	if ok && queueSt != nil {
		queueSt.RemoveByAuthor(session, "guest")
	}
	return ok
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
	body := func(w http.ResponseWriter, r *http.Request) (struct{ ID, Dir, Mode string }, bool) {
		var req struct{ ID, Dir, Mode string }
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
		writeJSON(w, 200, map[string]any{"ok": true, "token": shareSt.Create(req.ID, req.Dir)})
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
		if !shareSt.SetMode(req.ID, req.Mode) {
			fail(w, 404, "srv.share.introuvable")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /share-end", func(w http.ResponseWriter, r *http.Request) {
		req, ok := body(w, r)
		if !ok {
			return
		}
		if !shareSt.End(req.ID) {
			fail(w, 404, "srv.share.introuvable")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
}
