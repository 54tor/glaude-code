package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func tokenForm(h http.Handler, tok, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/token-login", strings.NewReader("token="+url.QueryEscape(tok)))
	r.Host = testHost
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Sans cookie, une page ouverte par le navigateur propose de saisir le jeton, avec l'icône et le manifeste (ajout à l'écran
// d'accueil d'iOS) ; l'API garde son 401.
func TestTokenPage(t *testing.T) {
	resetAuth(t, nil)
	resetTokenTries()
	h, _, _ := web(t)
	srv := withTokenOpen(modeTok, h)

	r := httptest.NewRequest("GET", "/memoire", nil)
	r.Host = testHost
	r.Header.Set("Accept", "text/html,application/xhtml+xml")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	body := w.Body.String()
	for _, want := range []string{`action="/token-login"`, `rel="apple-touch-icon"`, `rel="manifest"`, `type="password"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page du jeton sans %s : %d", want, w.Code)
		}
	}
	if strings.Contains(body, modeTok) {
		t.Fatal("la page contient le jeton")
	}
	if w := (modeReq{method: "GET", path: "/api"}).do(srv); w.Code != 401 {
		t.Fatalf("API sans cookie : %d", w.Code)
	}

	same := "http://" + testHost
	if w := tokenForm(srv, modeTok, "https://ailleurs.example"); w.Code != 403 {
		t.Fatalf("formulaire d'une autre origine : %d", w.Code)
	}
	if w := tokenForm(srv, "faux-jeton-0123456789", same); w.Code != 401 || cookieOf(w, tokenCookie) != nil {
		t.Fatalf("jeton faux : %d", w.Code)
	}
	w = tokenForm(srv, modeTok, same)
	c := cookieOf(w, tokenCookie)
	if w.Code != http.StatusSeeOther || c == nil || c.SameSite != http.SameSiteStrictMode || !c.HttpOnly {
		t.Fatalf("bon jeton : %d %v", w.Code, w.Result().Cookies())
	}

	// Essais limités : au-delà, même le bon jeton attend.
	for range tokenMaxTries {
		tokenForm(srv, "faux-jeton-0123456789", same)
	}
	if w := tokenForm(srv, modeTok, same); w.Code != http.StatusTooManyRequests {
		t.Fatalf("après %d échecs : %d", tokenMaxTries, w.Code)
	}
}
