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
func pairReq(handler http.Handler, method, path, body string, ck *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Host = testHost
	request.Header.Set("Origin", "http://"+testHost)
	request.Header.Set("Content-Type", "application/json")
	if ck != nil {
		request.AddCookie(ck)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func cookieOf(recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// confirm : la page de confirmation (sans effet) puis le POST de son bouton.
func confirm(t *testing.T, handler http.Handler, code string) *httptest.ResponseRecorder {
	t.Helper()
	if recorder := pairReq(handler, "GET", "/pair?c="+code, "", nil); recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `action="/pair"`) || len(recorder.Result().Cookies()) != 0 {
		t.Fatalf("page de confirmation : %d %v", recorder.Code, recorder.Result().Cookies())
	}
	request := httptest.NewRequest("POST", "/pair", strings.NewReader("c="+url.QueryEscape(code)))
	request.Host = testHost
	request.Header.Set("Origin", "http://"+testHost)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	localRecorder := httptest.NewRecorder()
	handler.ServeHTTP(localRecorder, request)
	return localRecorder
}

// pairCodeFrom : le code porté par l'adresse du QR renvoyée par POST /v1/pairing/codes.
func pairCodeFrom(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct{ URL string }
	if recorder.Code != 201 || json.Unmarshal(recorder.Body.Bytes(), &out) != nil {
		t.Fatalf("POST /v1/pairing/codes : %d %s", recorder.Code, recorder.Body)
	}
	uRL, err := url.Parse(out.URL)
	if err != nil || uRL.Host != testHost || uRL.Path != "/pair" || uRL.Query().Get("c") == "" {
		t.Fatalf("adresse du QR : %q", out.URL)
	}
	return uRL.Query().Get("c")
}

func TestPairTokenMode(t *testing.T) {
	resetAuth(t, nil)
	t.Setenv("GLD_AUTH_DIR", t.TempDir())
	now := time.Now()
	oNow := authNow
	authNow = func() time.Time { return now }
	t.Cleanup(func() { authNow = oNow })
	handler, _, _ := web(t)
	srv := withTokenOpen(modeTok, handler)
	tok := &http.Cookie{Name: tokenCookie, Value: modeTok}

	if recorder := pairReq(srv, "POST", "/v1/pairing/codes", "{}", nil); recorder.Code != 401 {
		t.Fatalf("code demandé sans jeton : %d", recorder.Code)
	}
	code := pairCodeFrom(t, pairReq(srv, "POST", "/v1/pairing/codes", "{}", tok))

	// Un POST venu d'une autre origine ne consomme rien.
	request := httptest.NewRequest("POST", "/pair", strings.NewReader("c="+code))
	request.Host = testHost
	request.Header.Set("Origin", "https://ailleurs.example")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	xw := httptest.NewRecorder()
	srv.ServeHTTP(xw, request)
	if xw.Code != 403 {
		t.Fatalf("POST d'une autre origine : %d", xw.Code)
	}
	localRecorder := confirm(t, srv, code)
	dev := cookieOf(localRecorder, devCookie)
	if localRecorder.Code != http.StatusSeeOther || localRecorder.Header().Get("Location") != "/" || dev == nil || !dev.HttpOnly || dev.SameSite != http.SameSiteStrictMode || cookieOf(localRecorder, tokenCookie) != nil {
		t.Fatalf("échange du code : %d %q %v", localRecorder.Code, localRecorder.Header().Get("Location"), localRecorder.Result().Cookies())
	}
	if recorder := pairReq(srv, "GET", "/pair?c="+code, "", nil); recorder.Code != http.StatusGone {
		t.Fatalf("code réutilisé : %d", recorder.Code)
	}
	if recorder := pairReq(srv, "GET", "/v1/state", "", dev); recorder.Code != 200 {
		t.Fatalf("appareil jumelé refusé : %d", recorder.Code)
	}

	localRecorder = pairReq(srv, "GET", "/v1/devices", "", tok)
	var list struct{ Devices []device }
	if localRecorder.Code != 200 || json.Unmarshal(localRecorder.Body.Bytes(), &list) != nil || len(list.Devices) != 1 {
		t.Fatalf("liste des appareils : %d %s", localRecorder.Code, localRecorder.Body)
	}
	if strings.Contains(localRecorder.Body.String(), dev.Value) {
		t.Fatal("la liste expose le cookie d'un appareil")
	}

	// 90 jours sans visite : la session tombe ; une visite avant la repousse.
	now = now.Add(89 * 24 * time.Hour)
	if recorder := pairReq(srv, "GET", "/v1/state", "", dev); recorder.Code != 200 {
		t.Fatalf("à 89 jours : %d", recorder.Code)
	}
	now = now.Add(89 * 24 * time.Hour)
	if recorder := pairReq(srv, "GET", "/v1/state", "", dev); recorder.Code != 200 {
		t.Fatalf("visite récente, fenêtre glissante : %d", recorder.Code)
	}

	id := list.Devices[0].ID
	if recorder := pairReq(srv, "DELETE", "/v1/devices/"+id, "", tok); recorder.Code != 204 {
		t.Fatalf("révocation : %d %s", recorder.Code, recorder.Body)
	}
	if recorder := pairReq(srv, "GET", "/v1/state", "", dev); recorder.Code != 401 {
		t.Fatalf("appareil révoqué encore admis : %d", recorder.Code)
	}

	// Code échu au bout de deux minutes.
	code = pairCodeFrom(t, pairReq(srv, "POST", "/v1/pairing/codes", "{}", tok))
	now = now.Add(pairTTL + time.Second)
	if recorder := pairReq(srv, "GET", "/pair?c="+code, "", nil); recorder.Code != http.StatusGone {
		t.Fatalf("code échu accepté : %d", recorder.Code)
	}

	// Changer de jeton coupe les appareils jumelés sous l'ancien.
	code = pairCodeFrom(t, pairReq(srv, "POST", "/v1/pairing/codes", "{}", tok))
	dev = cookieOf(confirm(t, srv, code), devCookie)
	if recorder := pairReq(srv, "GET", "/v1/state", "", dev); recorder.Code != 200 {
		t.Fatalf("appareil neuf : %d", recorder.Code)
	}
	srv2 := withTokenOpen("fedcba9876543210fedc", handler)
	if recorder := pairReq(srv2, "GET", "/v1/state", "", dev); recorder.Code != 401 {
		t.Fatalf("appareil admis sous un autre jeton : %d", recorder.Code)
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
	content, err := addDevice("iPhone", "192.0.2.1")
	if err != nil || !deviceOK(content) {
		t.Fatalf("appareil neuf : %v", err)
	}
	now = now.Add(devIdle + time.Minute)
	if deviceOK(content) {
		t.Fatal("appareil inactif depuis plus de 90 jours encore admis")
	}
	if ds, _ := loadDevices(); len(ds) != 0 {
		t.Fatalf("appareil échu conservé : %v", ds)
	}
}

func TestPairAccountMode(t *testing.T) {
	authStore, srv, _ := authWeb(t)
	id, err := authStore.AddUser("alice", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	content, principal := fullSession(t, authStore, id)
	ask := func(cookie, csrf string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/v1/pairing/codes", strings.NewReader("{}"))
		request.Host = testHost
		request.Header.Set("Origin", "http://"+testHost)
		request.Header.Set(csrfHeader, csrf)
		request.AddCookie(&http.Cookie{Name: sessCookie, Value: cookie})
		recorder := httptest.NewRecorder()
		srv.ServeHTTP(recorder, request)
		return recorder
	}
	code := pairCodeFrom(t, ask(content, principal.CSRF))
	if recorder := pairReq(srv, "GET", "/pair?c="+code, "", nil); !strings.Contains(recorder.Body.String(), "alice") {
		t.Fatalf("la confirmation ne nomme pas le compte : %s", recorder.Body)
	}
	localRecorder := confirm(t, srv, code)
	nc := cookieOf(localRecorder, sessCookie)
	if localRecorder.Code != http.StatusSeeOther || nc == nil {
		t.Fatalf("échange du code : %d %v", localRecorder.Code, localRecorder.Result().Cookies())
	}
	np, err := authStore.Session(nc.Value)
	if err != nil || np.User.ID != id || !np.MFA || np.SessionID == principal.SessionID {
		t.Fatalf("session jumelée : %+v %v", np, err)
	}
	if recorder := pairReq(srv, "GET", "/pair?c="+code, "", nil); recorder.Code != http.StatusGone {
		t.Fatalf("code réutilisé : %d", recorder.Code)
	}
	if !np.Expires.Equal(principal.Expires) {
		t.Fatalf("échéance de la session jumelée %v, émettrice %v", np.Expires, principal.Expires)
	}
	// Une session jumelée depuis la session jumelée dépend toujours de la première émettrice.
	code = pairCodeFrom(t, ask(nc.Value, np.CSRF))
	gc := cookieOf(confirm(t, srv, code), sessCookie)
	if gc == nil {
		t.Fatal("jumelage depuis une session jumelée refusé")
	}
	c0, p0 := fullSession(t, authStore, id) // nouvelle émettrice pour la suite du test
	if err := authStore.RevokeSession(principal.SessionID); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{nc.Value, gc.Value} {
		if _, err := authStore.Session(value); err == nil {
			t.Fatal("session jumelée encore valide après la révocation de l'émettrice")
		}
	}
	content, principal = c0, p0

	// Révoquer la session émettrice annule ses codes en attente.
	code = pairCodeFrom(t, ask(content, principal.CSRF))
	if err := authStore.RevokeSession(principal.SessionID); err != nil {
		t.Fatal(err)
	}
	localRequest := httptest.NewRequest("POST", "/pair", strings.NewReader("c="+url.QueryEscape(code)))
	localRequest.Host = testHost
	localRequest.Header.Set("Origin", "http://"+testHost)
	localRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	srv.ServeHTTP(rw, localRequest)
	if cookieOf(rw, sessCookie) != nil {
		t.Fatal("code d'une session révoquée accepté")
	}

	// Une session sans second facteur ne jumelle rien.
	partial, err := authStore.NewSession(id, "203.0.113.5", "ua")
	if err != nil {
		t.Fatal(err)
	}
	pp, _ := authStore.Session(partial)
	if recorder := ask(partial, pp.CSRF); recorder.Code == 200 {
		t.Fatal("code délivré à une session partielle")
	}
	// Les appareils du mode jeton ne regardent pas le mode comptes.
	localRequest = httptest.NewRequest("GET", "/v1/devices", nil)
	localRequest.Host = testHost
	fresh, _ := fullSession(t, authStore, id)
	localRequest.AddCookie(&http.Cookie{Name: sessCookie, Value: fresh})
	rw = httptest.NewRecorder()
	srv.ServeHTTP(rw, localRequest)
	if rw.Code != 404 {
		t.Fatalf("GET /v1/devices en mode comptes : %d", rw.Code)
	}
}

// GLD_PUBLIC_URL fixe l'adresse du QR : l'ordinateur qui l'affiche peut être sur 127.0.0.1 ou l'IP locale.
func TestPairPublicURL(t *testing.T) {
	resetAuth(t, nil)
	t.Setenv("GLD_AUTH_DIR", t.TempDir())
	handler, _, _ := web(t)
	srv := withTokenOpen(modeTok, handler)
	tok := &http.Cookie{Name: tokenCookie, Value: modeTok}
	for _, record := range []struct{ env, want string }{
		{"https://gld.example.org", "https://gld.example.org/pair?c="},
		{"https://gld.example.org:8443/", "https://gld.example.org:8443/pair?c="},
		{"pas une adresse", "http://" + testHost + "/pair?c="},
		{"", "http://" + testHost + "/pair?c="},
	} {
		t.Setenv("GLD_PUBLIC_URL", record.env)
		var out struct{ URL string }
		recorder := pairReq(srv, "POST", "/v1/pairing/codes", "{}", tok)
		if json.Unmarshal(recorder.Body.Bytes(), &out) != nil || !strings.HasPrefix(out.URL, record.want) {
			t.Errorf("GLD_PUBLIC_URL=%q : %q, attendu %q…", record.env, out.URL, record.want)
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
	handler, _, _ := web(t)
	srv := withTokenOpen(modeTok, handler)
	tok := &http.Cookie{Name: tokenCookie, Value: modeTok}
	post := func(content string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/pair", strings.NewReader("c="+url.QueryEscape(content)))
		request.Host = testHost
		request.Header.Set("Origin", "http://"+testHost)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		srv.ServeHTTP(recorder, request)
		return recorder
	}

	localRequest := httptest.NewRequest("GET", "/", nil)
	localRequest.Header.Set("Sec-Fetch-Mode", "navigate")
	localRecorder := httptest.NewRecorder()
	srv.ServeHTTP(localRecorder, localRequest)
	if !strings.Contains(localRecorder.Body.String(), `href="/pair"`) {
		t.Fatal("la page du jeton ne mène pas à « Connecter cet appareil »")
	}
	if recorder := pairReq(srv, "GET", "/pair", "", nil); recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `name="c"`) {
		t.Fatalf("formulaire du code : %d", recorder.Code)
	}
	code := pairCodeFrom(t, pairReq(srv, "POST", "/v1/pairing/codes", "{}", tok))
	if len(code) != 8 || pairDigits(code) != code {
		t.Fatalf("code : %q", code)
	}
	localRecorder = post(code[:4] + " " + code[4:])
	if localRecorder.Code != http.StatusSeeOther || cookieOf(localRecorder, devCookie) == nil {
		t.Fatalf("code tapé : %d %v", localRecorder.Code, localRecorder.Result().Cookies())
	}
	for index := 0; index < tokenMaxTries; index++ {
		if recorder := post("00000000"); recorder.Code != http.StatusGone {
			t.Fatalf("code faux n°%d : %d", index+1, recorder.Code)
		}
	}
	code = pairCodeFrom(t, pairReq(srv, "POST", "/v1/pairing/codes", "{}", tok))
	if recorder := post(code); recorder.Code != http.StatusTooManyRequests || cookieOf(recorder, devCookie) != nil {
		t.Fatalf("après %d essais : %d", tokenMaxTries, recorder.Code)
	}
}

func TestPairCodeCap(t *testing.T) {
	pairMu.Lock()
	pairCodes = map[string]pairCode{}
	pairMu.Unlock()
	t.Cleanup(func() { pairMu.Lock(); pairCodes = map[string]pairCode{}; pairMu.Unlock() })
	for index := 0; index < pairMaxPerSession; index++ {
		if newPairCode(1, "s") == "" {
			t.Fatalf("code %d refusé", index)
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
	content, err := addDevice("ua", "203.0.113.1")
	if err != nil || !deviceOK(content) {
		t.Fatal("appareil non admis :", err)
	}
	setDevKey("jeton-b-xxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	if deviceOK(content) {
		t.Fatal("appareil d'un autre jeton admis")
	}
	if _, err := addDevice("ua2", "203.0.113.2"); err != nil {
		t.Fatal(err)
	}
	setDevKey("jeton-a-xxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	if !deviceOK(content) {
		t.Fatal("appareil effacé pendant le passage sous un autre jeton")
	}
}
