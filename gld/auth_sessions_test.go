package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
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
	localAuthStore := testStore(t)
	id, err := localAuthStore.AddUser("alice", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	return localAuthStore, id
}

func fullSession(t *testing.T, localAuthStore *authStore, id int64) (string, *Principal) {
	content, err := localAuthStore.NewSession(id, "203.0.113.5", "ua")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := localAuthStore.Session(content)
	if err != nil {
		t.Fatal(err)
	}
	content, _, err = localAuthStore.PromoteMFA(principal.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if principal, err = localAuthStore.Session(content); err != nil {
		t.Fatal(err)
	}
	return content, principal
}

func TestSessionExpiry(t *testing.T) {
	now := testClock(t)
	authStore, id := sessUser(t)
	content, _ := fullSession(t, authStore, id)
	// Activité toutes les 11 h : la session glisse jusqu'à l'échéance absolue de 30 jours.
	start := *now
	for now.Sub(start) < sessAbsolute-11*time.Hour {
		*now = now.Add(11 * time.Hour)
		principal, err := authStore.Session(content)
		if err != nil || !principal.MFA || principal.User.ID != id {
			t.Fatalf("à %v : %+v %v", now.Sub(start), principal, err)
		}
	}
	*now = start.Add(sessAbsolute)
	if _, err := authStore.Session(content); !errors.Is(err, errNoSession) {
		t.Fatalf("absolue : %v", err)
	}

	content, _ = fullSession(t, authStore, id)
	*now = now.Add(sessIdle)
	if _, err := authStore.Session(content); !errors.Is(err, errNoSession) {
		t.Fatalf("inactivité : %v", err)
	}
}

func TestSessionPartialExpiry(t *testing.T) {
	now := testClock(t)
	authStore, id := sessUser(t)
	content, err := authStore.NewSession(id, "", "")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(thMFAWindow - time.Second)
	if principal, err := authStore.Session(content); err != nil || principal.MFA {
		t.Fatalf("partielle valide : %+v %v", principal, err)
	}
	*now = now.Add(time.Second)
	if _, err := authStore.Session(content); !errors.Is(err, errNoSession) {
		t.Fatalf("partielle échue : %v", err)
	}
	content, localPrincipal := fullSession(t, authStore, id)
	*now = now.Add(time.Hour)
	if p2, err := authStore.Session(content); err != nil || !p2.MFA || p2.SessionID != localPrincipal.SessionID {
		t.Fatalf("après MFA : %+v %v", p2, err)
	}
}

func TestSessionTouchThrottle(t *testing.T) {
	now := testClock(t)
	authStore, id := sessUser(t)
	content, _ := fullSession(t, authStore, id)
	seen := func() (number int64) {
		authStore.db.QueryRow(`SELECT last_seen_at FROM sessions`).Scan(&number)
		return
	}
	t0 := seen()
	*now = now.Add(30 * time.Second)
	authStore.Session(content)
	if seen() != t0 {
		t.Fatal("écriture avant une minute")
	}
	*now = now.Add(30 * time.Second)
	authStore.Session(content)
	if seen() != now.Unix() {
		t.Fatal("last_seen_at non mis à jour")
	}
}

func TestSessionHashed(t *testing.T) {
	testClock(t)
	authStore, id := sessUser(t)
	content, _ := fullSession(t, authStore, id)
	rows, err := authStore.db.Query(`SELECT * FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for index := range vals {
			ptrs[index] = &vals[index]
		}
		rows.Scan(ptrs...)
		for index, value := range vals {
			var data []byte
			switch value := value.(type) {
			case []byte:
				data = value
			case string:
				data = []byte(value)
			}
			if bytes.Contains(data, []byte(content)) {
				t.Fatalf("cookie en clair dans %s", cols[index])
			}
		}
	}
	if _, err := authStore.Session(content + "x"); !errors.Is(err, errNoSession) {
		t.Fatal("cookie altéré accepté")
	}
}

func TestRevokeUser(t *testing.T) {
	testClock(t)
	authStore, id := sessUser(t)
	bob, _ := authStore.AddUser("bob", testPw, false)
	c1, _ := fullSession(t, authStore, id)
	c2, p2 := fullSession(t, authStore, id)
	cb, _ := fullSession(t, authStore, bob)
	if err := authStore.RevokeSession(p2.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.Session(c2); !errors.Is(err, errNoSession) {
		t.Fatal("session révoquée encore valide")
	}
	if _, err := authStore.Session(c1); err != nil {
		t.Fatal("autre session touchée")
	}
	if err := authStore.RevokeUser(id); err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.Session(c1); !errors.Is(err, errNoSession) {
		t.Fatal("RevokeUser sans effet")
	}
	if _, err := authStore.Session(cb); err != nil {
		t.Fatal("session d'un autre compte révoquée")
	}
}

func TestSessionUserTamperedOrDisabled(t *testing.T) {
	testClock(t)
	authStore, id := sessUser(t)
	content, _ := fullSession(t, authStore, id)
	if _, err := authStore.db.Exec(`UPDATE users SET is_admin=1 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if principal, err := authStore.Session(content); !errors.Is(err, errNoSession) || principal != nil {
		t.Fatalf("ligne altérée : %+v %v", principal, err)
	}
	if tamperEvents(t, authStore) != 1 {
		t.Fatal("altération non journalisée")
	}

	bob, _ := authStore.AddUser("bob", testPw, false)
	cb, _ := fullSession(t, authStore, bob)
	user, _ := authStore.UserByName("bob")
	user.Disabled = true
	// Désactivation valablement signée mais sans la révocation que fait SetDisabled.
	authStore.db.Exec(`UPDATE users SET disabled=1, sig=? WHERE id=?`, authStore.userSig(user), bob)
	if principal, err := authStore.Session(cb); !errors.Is(err, errNoSession) || principal != nil {
		t.Fatalf("compte désactivé : %+v %v", principal, err)
	}
}

func TestCSRFRequired(t *testing.T) {
	testClock(t)
	authStore, id := sessUser(t)
	content, principal := fullSession(t, authStore, id)
	_, p2 := fullSession(t, authStore, id)
	if again, err := authStore.Session(content); err != nil || again.CSRF != principal.CSRF {
		t.Fatal("CSRF instable pour une même session")
	}
	if principal.CSRF == p2.CSRF || len(principal.CSRF) != 64 {
		t.Fatalf("CSRF : %q %q", principal.CSRF, p2.CSRF)
	}
	handler := http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if !csrfOK(request, principal) {
			http.Error(responseWriter, "", http.StatusForbidden)
		}
	})
	for _, tc := range []struct {
		method, tok string
		want        int
	}{
		{"GET", "", 200}, {"HEAD", "", 200}, {"OPTIONS", "", 200},
		{"POST", "", 403}, {"POST", p2.CSRF, 403}, {"POST", strings.ToUpper(principal.CSRF), 403},
		{"POST", principal.CSRF, 200}, {"DELETE", "", 403}, {"PUT", principal.CSRF, 200},
	} {
		request := httptest.NewRequest(tc.method, "/x", nil)
		if tc.tok != "" {
			request.Header.Set(csrfHeader, tc.tok)
		}
		recorder := httptest.NewRecorder()
		handler(recorder, request)
		if recorder.Code != tc.want {
			t.Errorf("%s %q : %d", tc.method, tc.tok, recorder.Code)
		}
	}
	if csrfOK(httptest.NewRequest("POST", "/x", nil), &Principal{}) {
		t.Fatal("CSRF vide accepté")
	}
}

func TestSessionCookieFlags(t *testing.T) {
	exp := time.Unix(1_900_000_000, 0)
	get := func(request *http.Request) *http.Cookie {
		recorder := httptest.NewRecorder()
		setSessionCookie(recorder, request, "v", exp)
		cookie := recorder.Result().Cookies()[0]
		if cookie.Name != sessCookie || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" || !cookie.Expires.Equal(exp) {
			t.Fatalf("drapeaux : %+v", cookie)
		}
		return cookie
	}
	t.Setenv("GLD_TRUSTED_PROXIES", "10.0.0.1")
	localRequest := httptest.NewRequest("GET", "/", nil)
	if get(localRequest).Secure {
		t.Fatal("Secure en HTTP")
	}
	localRequest.TLS = &tls.ConnectionState{}
	if !get(localRequest).Secure {
		t.Fatal("pas Secure sous TLS")
	}
	localRequest = httptest.NewRequest("GET", "/", nil)
	localRequest.Header.Set("X-Forwarded-Proto", "https")
	localRequest.RemoteAddr = "198.51.100.9:1234"
	if get(localRequest).Secure {
		t.Fatal("XFP d'un pair non fiable cru")
	}
	localRequest.RemoteAddr = "10.0.0.1:1234"
	if !get(localRequest).Secure {
		t.Fatal("XFP du proxy de confiance ignoré")
	}
	localRecorder := httptest.NewRecorder()
	clearSessionCookie(localRecorder)
	if cookie := localRecorder.Result().Cookies()[0]; cookie.Name != sessCookie || cookie.MaxAge >= 0 {
		t.Fatalf("effacement : %+v", cookie)
	}
}

func TestEventIP(t *testing.T) {
	authStore, id := sessUser(t)
	authStore.eventIP(id, "login", "ok", "203.0.113.5")
	var ip string
	if err := authStore.db.QueryRow(`SELECT ip FROM auth_events WHERE kind='login'`).Scan(&ip); err != nil || ip != "203.0.113.5" {
		t.Fatalf("%q %v", ip, err)
	}
}

func TestPromoteMFA(t *testing.T) {
	now := testClock(t)
	authStore, id := sessUser(t)
	start := *now
	old, err := authStore.NewSession(id, "203.0.113.5", "ua")
	if err != nil {
		t.Fatal(err)
	}
	principal, _ := authStore.Session(old)
	*now = now.Add(5 * time.Minute)
	content, exp, err := authStore.PromoteMFA(principal.SessionID)
	if err != nil || !exp.Equal(start.Add(sessAbsolute)) {
		t.Fatalf("promotion : %v %v", exp, err)
	}
	if _, err := authStore.Session(old); !errors.Is(err, errNoSession) {
		t.Fatal("ancien cookie encore valide")
	}
	np, err := authStore.Session(content)
	if err != nil || !np.MFA || np.User.ID != id || !np.Expires.Equal(exp) || np.CSRF == principal.CSRF || np.SessionID == principal.SessionID {
		t.Fatalf("nouvelle session : %+v %v", np, err)
	}
	if _, _, err := authStore.PromoteMFA(np.SessionID); !errors.Is(err, errNoSession) {
		t.Fatalf("double promotion : %v", err)
	}
	if _, _, err := authStore.PromoteMFA(principal.SessionID); !errors.Is(err, errNoSession) {
		t.Fatalf("promotion de l'ancienne : %v", err)
	}
	late, _ := authStore.NewSession(id, "", "")
	lp, _ := authStore.Session(late)
	*now = now.Add(thMFAWindow)
	if _, _, err := authStore.PromoteMFA(lp.SessionID); !errors.Is(err, errNoSession) {
		t.Fatalf("partielle échue promue : %v", err)
	}
}

func TestRevokeClosedDB(t *testing.T) {
	authStore, id := sessUser(t)
	authStore.db.Close()
	if authStore.RevokeUser(id) == nil || authStore.RevokeSession("00") == nil {
		t.Fatal("erreur de base ignorée")
	}
}

func TestSessionRowSigned(t *testing.T) {
	now := testClock(t)
	authStore, id := sessUser(t)
	bob, err := authStore.AddUser("bob", testPw, true)
	if err != nil {
		t.Fatal(err)
	}
	// Une ligne insérée sans la clé : ni un SHA-256 du cookie ni la bonne empreinte sans signature ne valent.
	raw := bytes.Repeat([]byte{7}, 32)
	cookie := base64.RawURLEncoding.EncodeToString(raw)
	plain := sha256.Sum256(raw)
	count := now.Unix()
	authStore.db.Exec(`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at,mfa_at) VALUES(?,?,?,?,?,?)`, plain[:], bob, count, count, count+3600, count)
	if _, err := authStore.Session(cookie); !errors.Is(err, errNoSession) {
		t.Fatalf("SHA-256 brut accepté : %v", err)
	}
	authStore.db.Exec(`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at,mfa_at) VALUES(?,?,?,?,?,?)`, authStore.sessHash(raw), bob, count, count, count+3600, count)
	before := tamperEvents(t, authStore)
	if _, err := authStore.Session(cookie); !errors.Is(err, errNoSession) {
		t.Fatalf("ligne sans signature acceptée : %v", err)
	}
	if tamperEvents(t, authStore) != before+1 {
		t.Fatal("altération non journalisée")
	}

	content, principal := fullSession(t, authStore, id)
	data, _ := hex.DecodeString(principal.SessionID)
	var mfa sql.NullInt64
	var created int64
	var sig []byte
	authStore.db.QueryRow(`SELECT created_at,mfa_at,sig FROM sessions WHERE id_hash=?`, data).Scan(&created, &mfa, &sig)
	if !mfa.Valid || !authStore.sessOK(data, id, created, mfa, sig) {
		t.Fatal("ligne promue non signée")
	}
	var old int
	authStore.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id=? AND mfa_at IS NULL`, id).Scan(&old)
	if old != 0 {
		t.Fatal("session partielle conservée après promotion")
	}
	if _, err := authStore.Session(content); err != nil {
		t.Fatalf("cookie légitime refusé : %v", err)
	}
	authStore.db.Exec(`UPDATE sessions SET user_id=? WHERE id_hash=?`, bob, data)
	if _, err := authStore.Session(content); !errors.Is(err, errNoSession) {
		t.Fatalf("session retournée vers un autre compte acceptée : %v", err)
	}
}

func TestPurgeSessions(t *testing.T) {
	now := testClock(t)
	authStore, id := sessUser(t)
	content, _ := fullSession(t, authStore, id)
	authStore.NewSession(id, "", "")
	*now = now.Add(thMFAWindow)
	if count, err := authStore.PurgeSessions(); err != nil || count != 1 {
		t.Fatalf("partielle : %d %v", count, err)
	}
	if _, err := authStore.Session(content); err != nil {
		t.Fatal("session complète purgée")
	}
	*now = now.Add(sessIdle)
	if count, err := authStore.PurgeSessions(); err != nil || count != 1 {
		t.Fatalf("échue : %d %v", count, err)
	}
}
