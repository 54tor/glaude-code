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
	if w := pairReq(srv, "GET", "/v1/state", "", dev); w.Code != 200 {
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
	if w := pairReq(srv, "GET", "/v1/state", "", dev); w.Code != 200 {
		t.Fatalf("à 89 jours : %d", w.Code)
	}
	now = now.Add(89 * 24 * time.Hour)
	if w := pairReq(srv, "GET", "/v1/state", "", dev); w.Code != 200 {
		t.Fatalf("visite récente, fenêtre glissante : %d", w.Code)
	}

	id := list.Devices[0].ID
	if w := pairReq(srv, "POST", "/devices-revoke", `{"id":"`+id+`"}`, tok); w.Code != 200 {
		t.Fatalf("révocation : %d %s", w.Code, w.Body)
	}
	if w := pairReq(srv, "GET", "/v1/state", "", dev); w.Code != 401 {
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
	if w := pairReq(srv, "GET", "/v1/state", "", dev); w.Code != 200 {
		t.Fatalf("appareil neuf : %d", w.Code)
	}
	srv2 := withTokenOpen("fedcba9876543210fedc", h)
	if w := pairReq(srv2, "GET", "/v1/state", "", dev); w.Code != 401 {
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
	if !np.Expires.Equal(p.Expires) {
		t.Fatalf("échéance de la session jumelée %v, émettrice %v", np.Expires, p.Expires)
	}
	// Une session jumelée depuis la session jumelée dépend toujours de la première émettrice.
	code = pairCodeFrom(t, ask(nc.Value, np.CSRF))
	gc := cookieOf(confirm(t, srv, code), sessCookie)
	if gc == nil {
		t.Fatal("jumelage depuis une session jumelée refusé")
	}
	c0, p0 := fullSession(t, s, id) // nouvelle émettrice pour la suite du test
	if err := s.RevokeSession(p.SessionID); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{nc.Value, gc.Value} {
		if _, err := s.Session(v); err == nil {
			t.Fatal("session jumelée encore valide après la révocation de l'émettrice")
		}
	}
	c, p = c0, p0

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
	fresh, _ := fullSession(t, s, id)
	r.AddCookie(&http.Cookie{Name: sessCookie, Value: fresh})
	rw = httptest.NewRecorder()
	srv.ServeHTTP(rw, r)
	if rw.Code != 404 {
		t.Fatalf("GET /devices en mode comptes : %d", rw.Code)
	}
}

// GLD_PUBLIC_URL fixe l'adresse du QR : l'ordinateur qui l'affiche peut être sur 127.0.0.1 ou l'IP locale.
func TestPairPublicURL(t *testing.T) {
	resetAuth(t, nil)
	t.Setenv("GLD_AUTH_DIR", t.TempDir())
	h, _, _ := web(t)
	srv := withTokenOpen(modeTok, h)
	tok := &http.Cookie{Name: tokenCookie, Value: modeTok}
	for _, c := range []struct{ env, want string }{
		{"https://gld.example.org", "https://gld.example.org/pair?c="},
		{"https://gld.example.org:8443/", "https://gld.example.org:8443/pair?c="},
		{"pas une adresse", "http://" + testHost + "/pair?c="},
		{"", "http://" + testHost + "/pair?c="},
	} {
		t.Setenv("GLD_PUBLIC_URL", c.env)
		var out struct{ URL string }
		w := pairReq(srv, "POST", "/pair-code", "{}", tok)
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || !strings.HasPrefix(out.URL, c.want) {
			t.Errorf("GLD_PUBLIC_URL=%q : %q, attendu %q…", c.env, out.URL, c.want)
		}
	}
}

// Le code tapé sur « Connecter cet appareil » (page du jeton › /pair) connecte sans confirmation ; les codes faux
// épuisent les essais de l'adresse.
func TestPairTyped(t *testing.T) {
	resetAuth(t, nil)
	resetTokenTries()
	t.Cleanup(resetTokenTries)
	t.Setenv("GLD_AUTH_DIR", t.TempDir())
	h, _, _ := web(t)
	srv := withTokenOpen(modeTok, h)
	tok := &http.Cookie{Name: tokenCookie, Value: modeTok}
	post := func(c string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/pair", strings.NewReader("c="+url.QueryEscape(c)))
		r.Host = testHost
		r.Header.Set("Origin", "http://"+testHost)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `href="/pair"`) {
		t.Fatal("la page du jeton ne mène pas à « Connecter cet appareil »")
	}
	if w := pairReq(srv, "GET", "/pair", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `name="c"`) {
		t.Fatalf("formulaire du code : %d", w.Code)
	}
	code := pairCodeFrom(t, pairReq(srv, "POST", "/pair-code", "{}", tok))
	if len(code) != 8 || pairDigits(code) != code {
		t.Fatalf("code : %q", code)
	}
	w = post(code[:4] + " " + code[4:])
	if w.Code != http.StatusSeeOther || cookieOf(w, devCookie) == nil {
		t.Fatalf("code tapé : %d %v", w.Code, w.Result().Cookies())
	}
	for i := 0; i < tokenMaxTries; i++ {
		if w := post("00000000"); w.Code != http.StatusGone {
			t.Fatalf("code faux n°%d : %d", i+1, w.Code)
		}
	}
	code = pairCodeFrom(t, pairReq(srv, "POST", "/pair-code", "{}", tok))
	if w := post(code); w.Code != http.StatusTooManyRequests || cookieOf(w, devCookie) != nil {
		t.Fatalf("après %d essais : %d", tokenMaxTries, w.Code)
	}
}

func TestPairCodeCap(t *testing.T) {
	pairMu.Lock()
	pairCodes = map[string]pairCode{}
	pairMu.Unlock()
	t.Cleanup(func() { pairMu.Lock(); pairCodes = map[string]pairCode{}; pairMu.Unlock() })
	for i := 0; i < pairMaxPerSession; i++ {
		if newPairCode(1, "s") == "" {
			t.Fatalf("code %d refusé", i)
		}
	}
	if newPairCode(1, "s") != "" {
		t.Error("plafond par session dépassé")
	}
	if newPairCode(1, "autre") != "" {
		t.Error("le plafond vaut pour le compte, pas pour la session")
	}
	if newPairCode(2, "s") == "" {
		t.Error("un autre compte doit pouvoir émettre")
	}
}

func TestAppareilsDUnAutreJetonGardes(t *testing.T) {
	t.Setenv("GLD_AUTH_DIR", t.TempDir())
	devMu.Lock()
	old := devKey
	devMu.Unlock()
	t.Cleanup(func() { devMu.Lock(); devKey = old; devMu.Unlock() })
	setDevKey("jeton-a-xxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	c, err := addDevice("ua", "203.0.113.1")
	if err != nil || !deviceOK(c) {
		t.Fatal("appareil non admis :", err)
	}
	setDevKey("jeton-b-xxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	if deviceOK(c) {
		t.Fatal("appareil d'un autre jeton admis")
	}
	if _, err := addDevice("ua2", "203.0.113.2"); err != nil {
		t.Fatal(err)
	}
	setDevKey("jeton-a-xxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	if !deviceOK(c) {
		t.Fatal("appareil effacé pendant le passage sous un autre jeton")
	}
}
