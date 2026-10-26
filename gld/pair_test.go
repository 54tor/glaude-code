package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// pairReq : une requête au serveur complet, avec au plus un cookie.
func pairReq(h http.Handler, method, path, body string, ck *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = testHost
	r.Header.Set("Origin", "http://"+testHost)
	r.Header.Set("Content-Type", "application/json")
	if ck != nil {
		r.AddCookie(ck)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func cookieOf(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// confirm : la page de confirmation (sans effet) puis le POST de son bouton.
func confirm(t *testing.T, h http.Handler, code string) *httptest.ResponseRecorder {
	t.Helper()
	if w := pairReq(h, "GET", "/pair?c="+code, "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `action="/pair"`) || len(w.Result().Cookies()) != 0 {
		t.Fatalf("page de confirmation : %d %v", w.Code, w.Result().Cookies())
	}
	r := httptest.NewRequest("POST", "/pair", strings.NewReader("c="+url.QueryEscape(code)))
	r.Host = testHost
	r.Header.Set("Origin", "http://"+testHost)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// pairCodeFrom : le code porté par l'adresse du QR renvoyée par POST /pair-code.
func pairCodeFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct{ URL string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("POST /pair-code : %d %s", w.Code, w.Body)
	}
	u, err := url.Parse(out.URL)
	if err != nil || u.Host != testHost || u.Path != "/pair" || u.Query().Get("c") == "" {
		t.Fatalf("adresse du QR : %q", out.URL)
	}
	return u.Query().Get("c")
}

func TestPairTokenMode(t *testing.T) {
	resetAuth(t, nil)
	t.Setenv("GLD_AUTH_DIR", t.TempDir())
	now := time.Now()
	oNow := authNow
	authNow = func() time.Time { return now }
	t.Cleanup(func() { authNow = oNow })
	h, _, _ := web(t)
	srv := withTokenOpen(modeTok, h)
	tok := &http.Cookie{Name: tokenCookie, Value: modeTok}

	if w := pairReq(srv, "POST", "/pair-code", "{}", nil); w.Code != 401 {
		t.Fatalf("code demandé sans jeton : %d", w.Code)
	}
	code := pairCodeFrom(t, pairReq(srv, "POST", "/pair-code", "{}", tok))

	// Un POST venu d'une autre origine ne consomme rien.
	r := httptest.NewRequest("POST", "/pair", strings.NewReader("c="+code))
	r.Host = testHost
	r.Header.Set("Origin", "https://ailleurs.example")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	xw := httptest.NewRecorder()
	srv.ServeHTTP(xw, r)
	if xw.Code != 403 {
		t.Fatalf("POST d'une autre origine : %d", xw.Code)
	}
	w := confirm(t, srv, code)
	dev := cookieOf(w, devCookie)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" || dev == nil || !dev.HttpOnly || dev.SameSite != http.SameSiteStrictMode || cookieOf(w, tokenCookie) != nil {
		t.Fatalf("échange du code : %d %q %v", w.Code, w.Header().Get("Location"), w.Result().Cookies())
	}
	if w := pairReq(srv, "GET", "/pair?c="+code, "", nil); w.Code != http.StatusGone {
		t.Fatalf("code réutilisé : %d", w.Code)
	}
	if w := pairReq(srv, "GET", "/api", "", dev); w.Code != 200 {
		t.Fatalf("appareil jumelé refusé : %d", w.Code)
	}

	w = pairReq(srv, "GET", "/devices", "", tok)
	var list struct{ Devices []device }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Devices) != 1 {
		t.Fatalf("liste des appareils : %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), dev.Value) {
		t.Fatal("la liste expose le cookie d'un appareil")
	}

	// 90 jours sans visite : la session tombe ; une visite avant la repousse.
	now = now.Add(89 * 24 * time.Hour)
	if w := pairReq(srv, "GET", "/api", "", dev); w.Code != 200 {
		t.Fatalf("à 89 jours : %d", w.Code)
	}
	now = now.Add(89 * 24 * time.Hour)
	if w := pairReq(srv, "GET", "/api", "", dev); w.Code != 200 {
		t.Fatalf("visite récente, fenêtre glissante : %d", w.Code)
	}

	id := list.Devices[0].ID
	if w := pairReq(srv, "POST", "/devices-revoke", `{"id":"`+id+`"}`, tok); w.Code != 200 {
		t.Fatalf("révocation : %d %s", w.Code, w.Body)
	}
	if w := pairReq(srv, "GET", "/api", "", dev); w.Code != 401 {
		t.Fatalf("appareil révoqué encore admis : %d", w.Code)
	}

	// Code échu au bout de deux minutes.
	code = pairCodeFrom(t, pairReq(srv, "POST", "/pair-code", "{}", tok))
	now = now.Add(pairTTL + time.Second)
	if w := pairReq(srv, "GET", "/pair?c="+code, "", nil); w.Code != http.StatusGone {
		t.Fatalf("code échu accepté : %d", w.Code)
	}

	// Changer de jeton coupe les appareils jumelés sous l'ancien.
	code = pairCodeFrom(t, pairReq(srv, "POST", "/pair-code", "{}", tok))
	dev = cookieOf(confirm(t, srv, code), devCookie)
	if w := pairReq(srv, "GET", "/api", "", dev); w.Code != 200 {
		t.Fatalf("appareil neuf : %d", w.Code)
	}
	srv2 := withTokenOpen("fedcba9876543210fedc", h)
	if w := pairReq(srv2, "GET", "/api", "", dev); w.Code != 401 {
		t.Fatalf("appareil admis sous un autre jeton : %d", w.Code)
	}
}

func TestDeviceIdleExpiry(t *testing.T) {
	resetAuth(t, nil)
	t.Setenv("GLD_AUTH_DIR", t.TempDir())
	now := time.Now()
	oNow := authNow
	authNow = func() time.Time { return now }
	t.Cleanup(func() { authNow = oNow })
	setDevKey(modeTok)
	c, err := addDevice("iPhone", "192.0.2.1")
	if err != nil || !deviceOK(c) {
		t.Fatalf("appareil neuf : %v", err)
	}
	now = now.Add(devIdle + time.Minute)
	if deviceOK(c) {
		t.Fatal("appareil inactif depuis plus de 90 jours encore admis")
	}
	if ds, _ := loadDevices(); len(ds) != 0 {
		t.Fatalf("appareil échu conservé : %v", ds)
	}
}

func TestPairAccountMode(t *testing.T) {
	s, srv, _ := authWeb(t)
	id, err := s.AddUser("alice", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	c, p := fullSession(t, s, id)
	ask := func(cookie, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/pair-code", strings.NewReader("{}"))
		r.Host = testHost
		r.Header.Set("Origin", "http://"+testHost)
		r.Header.Set(csrfHeader, csrf)
		r.AddCookie(&http.Cookie{Name: sessCookie, Value: cookie})
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}
	code := pairCodeFrom(t, ask(c, p.CSRF))
	if w := pairReq(srv, "GET", "/pair?c="+code, "", nil); !strings.Contains(w.Body.String(), "alice") {
		t.Fatalf("la confirmation ne nomme pas le compte : %s", w.Body)
	}
	w := confirm(t, srv, code)
	nc := cookieOf(w, sessCookie)
	if w.Code != http.StatusSeeOther || nc == nil {
		t.Fatalf("échange du code : %d %v", w.Code, w.Result().Cookies())
	}
	np, err := s.Session(nc.Value)
	if err != nil || np.User.ID != id || !np.MFA || np.SessionID == p.SessionID {
		t.Fatalf("session jumelée : %+v %v", np, err)
	}
	if w := pairReq(srv, "GET", "/pair?c="+code, "", nil); w.Code != http.StatusGone {
		t.Fatalf("code réutilisé : %d", w.Code)
	}

	// Révoquer la session émettrice annule ses codes en attente.
	code = pairCodeFrom(t, ask(c, p.CSRF))
	if err := s.RevokeSession(p.SessionID); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/pair", strings.NewReader("c="+url.QueryEscape(code)))
	r.Host = testHost
	r.Header.Set("Origin", "http://"+testHost)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	srv.ServeHTTP(rw, r)
	if cookieOf(rw, sessCookie) != nil {
		t.Fatal("code d'une session révoquée accepté")
	}

	// Une session sans second facteur ne jumelle rien.
	partial, err := s.NewSession(id, "203.0.113.5", "ua")
	if err != nil {
		t.Fatal(err)
	}
	pp, _ := s.Session(partial)
	if w := ask(partial, pp.CSRF); w.Code == 200 {
		t.Fatal("code délivré à une session partielle")
	}
	// Les appareils du mode jeton ne regardent pas le mode comptes.
	r = httptest.NewRequest("GET", "/devices", nil)
	r.Host = testHost
	r.AddCookie(&http.Cookie{Name: sessCookie, Value: nc.Value})
	rw = httptest.NewRecorder()
	srv.ServeHTTP(rw, r)
	if rw.Code != 404 {
		t.Fatalf("GET /devices en mode comptes : %d", rw.Code)
	}
}
