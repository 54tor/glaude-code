package main

import (
	"bytes"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClock(t *testing.T) *time.Time {
	now := time.Unix(1_800_000_000, 0)
	old := authNow
	authNow = func() time.Time { return now }
	t.Cleanup(func() { authNow = old })
	return &now
}

func sessUser(t *testing.T) (*authStore, int64) {
	s := testStore(t)
	id, err := s.AddUser("alice", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	return s, id
}

func fullSession(t *testing.T, s *authStore, id int64) (string, *Principal) {
	c, err := s.NewSession(id, "203.0.113.5", "ua")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Session(c)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err = s.PromoteMFA(p.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if p, err = s.Session(c); err != nil {
		t.Fatal(err)
	}
	return c, p
}

func TestSessionExpiry(t *testing.T) {
	now := testClock(t)
	s, id := sessUser(t)
	c, _ := fullSession(t, s, id)
	// Activité toutes les 11 h : la session glisse jusqu'à l'échéance absolue de 30 jours.
	start := *now
	for now.Sub(start) < sessAbsolute-11*time.Hour {
		*now = now.Add(11 * time.Hour)
		p, err := s.Session(c)
		if err != nil || !p.MFA || p.User.ID != id {
			t.Fatalf("à %v : %+v %v", now.Sub(start), p, err)
		}
	}
	*now = start.Add(sessAbsolute)
	if _, err := s.Session(c); !errors.Is(err, errNoSession) {
		t.Fatalf("absolue : %v", err)
	}

	c, _ = fullSession(t, s, id)
	*now = now.Add(sessIdle)
	if _, err := s.Session(c); !errors.Is(err, errNoSession) {
		t.Fatalf("inactivité : %v", err)
	}
}

func TestSessionPartialExpiry(t *testing.T) {
	now := testClock(t)
	s, id := sessUser(t)
	c, err := s.NewSession(id, "", "")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(thMFAWindow - time.Second)
	if p, err := s.Session(c); err != nil || p.MFA {
		t.Fatalf("partielle valide : %+v %v", p, err)
	}
	*now = now.Add(time.Second)
	if _, err := s.Session(c); !errors.Is(err, errNoSession) {
		t.Fatalf("partielle échue : %v", err)
	}
	c, p := fullSession(t, s, id)
	*now = now.Add(time.Hour)
	if p2, err := s.Session(c); err != nil || !p2.MFA || p2.SessionID != p.SessionID {
		t.Fatalf("après MFA : %+v %v", p2, err)
	}
}

func TestSessionTouchThrottle(t *testing.T) {
	now := testClock(t)
	s, id := sessUser(t)
	c, _ := fullSession(t, s, id)
	seen := func() (v int64) {
		s.db.QueryRow(`SELECT last_seen_at FROM sessions`).Scan(&v)
		return
	}
	t0 := seen()
	*now = now.Add(30 * time.Second)
	s.Session(c)
	if seen() != t0 {
		t.Fatal("écriture avant une minute")
	}
	*now = now.Add(30 * time.Second)
	s.Session(c)
	if seen() != now.Unix() {
		t.Fatal("last_seen_at non mis à jour")
	}
}

func TestSessionHashed(t *testing.T) {
	testClock(t)
	s, id := sessUser(t)
	c, _ := fullSession(t, s, id)
	rows, err := s.db.Query(`SELECT * FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		for i, v := range vals {
			var b []byte
			switch x := v.(type) {
			case []byte:
				b = x
			case string:
				b = []byte(x)
			}
			if bytes.Contains(b, []byte(c)) {
				t.Fatalf("cookie en clair dans %s", cols[i])
			}
		}
	}
	if _, err := s.Session(c + "x"); !errors.Is(err, errNoSession) {
		t.Fatal("cookie altéré accepté")
	}
}

func TestRevokeUser(t *testing.T) {
	testClock(t)
	s, id := sessUser(t)
	bob, _ := s.AddUser("bob", testPw, false)
	c1, _ := fullSession(t, s, id)
	c2, p2 := fullSession(t, s, id)
	cb, _ := fullSession(t, s, bob)
	if err := s.RevokeSession(p2.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session(c2); !errors.Is(err, errNoSession) {
		t.Fatal("session révoquée encore valide")
	}
	if _, err := s.Session(c1); err != nil {
		t.Fatal("autre session touchée")
	}
	if err := s.RevokeUser(id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session(c1); !errors.Is(err, errNoSession) {
		t.Fatal("RevokeUser sans effet")
	}
	if _, err := s.Session(cb); err != nil {
		t.Fatal("session d'un autre compte révoquée")
	}
}

func TestSessionUserTamperedOrDisabled(t *testing.T) {
	testClock(t)
	s, id := sessUser(t)
	c, _ := fullSession(t, s, id)
	if _, err := s.db.Exec(`UPDATE users SET is_admin=1 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Session(c); !errors.Is(err, errNoSession) || p != nil {
		t.Fatalf("ligne altérée : %+v %v", p, err)
	}
	if tamperEvents(t, s) != 1 {
		t.Fatal("altération non journalisée")
	}

	bob, _ := s.AddUser("bob", testPw, false)
	cb, _ := fullSession(t, s, bob)
	u, _ := s.UserByName("bob")
	u.Disabled = true
	// Désactivation valablement signée mais sans la révocation que fait SetDisabled.
	s.db.Exec(`UPDATE users SET disabled=1, sig=? WHERE id=?`, s.userSig(u), bob)
	if p, err := s.Session(cb); !errors.Is(err, errNoSession) || p != nil {
		t.Fatalf("compte désactivé : %+v %v", p, err)
	}
}

func TestCSRFRequired(t *testing.T) {
	testClock(t)
	s, id := sessUser(t)
	c, p := fullSession(t, s, id)
	_, p2 := fullSession(t, s, id)
	if again, err := s.Session(c); err != nil || again.CSRF != p.CSRF {
		t.Fatal("CSRF instable pour une même session")
	}
	if p.CSRF == p2.CSRF || len(p.CSRF) != 64 {
		t.Fatalf("CSRF : %q %q", p.CSRF, p2.CSRF)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !csrfOK(r, p) {
			http.Error(w, "", http.StatusForbidden)
		}
	})
	for _, tc := range []struct {
		method, tok string
		want        int
	}{
		{"GET", "", 200}, {"HEAD", "", 200}, {"OPTIONS", "", 200},
		{"POST", "", 403}, {"POST", p2.CSRF, 403}, {"POST", strings.ToUpper(p.CSRF), 403},
		{"POST", p.CSRF, 200}, {"DELETE", "", 403}, {"PUT", p.CSRF, 200},
	} {
		r := httptest.NewRequest(tc.method, "/x", nil)
		if tc.tok != "" {
			r.Header.Set(csrfHeader, tc.tok)
		}
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != tc.want {
			t.Errorf("%s %q : %d", tc.method, tc.tok, w.Code)
		}
	}
	if csrfOK(httptest.NewRequest("POST", "/x", nil), &Principal{}) {
		t.Fatal("CSRF vide accepté")
	}
}

func TestSessionCookieFlags(t *testing.T) {
	exp := time.Unix(1_900_000_000, 0)
	get := func(r *http.Request) *http.Cookie {
		w := httptest.NewRecorder()
		setSessionCookie(w, r, "v", exp)
		c := w.Result().Cookies()[0]
		if c.Name != sessCookie || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" || !c.Expires.Equal(exp) {
			t.Fatalf("drapeaux : %+v", c)
		}
		return c
	}
	t.Setenv("GLD_TRUSTED_PROXIES", "10.0.0.1")
	r := httptest.NewRequest("GET", "/", nil)
	if get(r).Secure {
		t.Fatal("Secure en HTTP")
	}
	r.TLS = &tls.ConnectionState{}
	if !get(r).Secure {
		t.Fatal("pas Secure sous TLS")
	}
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "198.51.100.9:1234"
	if get(r).Secure {
		t.Fatal("XFP d'un pair non fiable cru")
	}
	r.RemoteAddr = "10.0.0.1:1234"
	if !get(r).Secure {
		t.Fatal("XFP du proxy de confiance ignoré")
	}
	w := httptest.NewRecorder()
	clearSessionCookie(w)
	if c := w.Result().Cookies()[0]; c.Name != sessCookie || c.MaxAge >= 0 {
		t.Fatalf("effacement : %+v", c)
	}
}

func TestEventIP(t *testing.T) {
	s, id := sessUser(t)
	s.eventIP(id, "login", "ok", "203.0.113.5")
	var ip string
	if err := s.db.QueryRow(`SELECT ip FROM auth_events WHERE kind='login'`).Scan(&ip); err != nil || ip != "203.0.113.5" {
		t.Fatalf("%q %v", ip, err)
	}
}

func TestPromoteMFA(t *testing.T) {
	now := testClock(t)
	s, id := sessUser(t)
	start := *now
	old, err := s.NewSession(id, "203.0.113.5", "ua")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.Session(old)
	*now = now.Add(5 * time.Minute)
	c, exp, err := s.PromoteMFA(p.SessionID)
	if err != nil || !exp.Equal(start.Add(sessAbsolute)) {
		t.Fatalf("promotion : %v %v", exp, err)
	}
	if _, err := s.Session(old); !errors.Is(err, errNoSession) {
		t.Fatal("ancien cookie encore valide")
	}
	np, err := s.Session(c)
	if err != nil || !np.MFA || np.User.ID != id || !np.Expires.Equal(exp) || np.CSRF == p.CSRF || np.SessionID == p.SessionID {
		t.Fatalf("nouvelle session : %+v %v", np, err)
	}
	if _, _, err := s.PromoteMFA(np.SessionID); !errors.Is(err, errNoSession) {
		t.Fatalf("double promotion : %v", err)
	}
	if _, _, err := s.PromoteMFA(p.SessionID); !errors.Is(err, errNoSession) {
		t.Fatalf("promotion de l'ancienne : %v", err)
	}
	late, _ := s.NewSession(id, "", "")
	lp, _ := s.Session(late)
	*now = now.Add(thMFAWindow)
	if _, _, err := s.PromoteMFA(lp.SessionID); !errors.Is(err, errNoSession) {
		t.Fatalf("partielle échue promue : %v", err)
	}
}

func TestRevokeClosedDB(t *testing.T) {
	s, id := sessUser(t)
	s.db.Close()
	if s.RevokeUser(id) == nil || s.RevokeSession("00") == nil {
		t.Fatal("erreur de base ignorée")
	}
}
