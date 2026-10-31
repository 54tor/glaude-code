package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func tokenForm(handler http.Handler, tok, origin string) *httptest.ResponseRecorder {
	return tokenFormFrom(handler, tok, origin, "192.0.2.1:1234")
}

func tokenFormFrom(handler http.Handler, tok, origin, addr string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("POST", "/token-login", strings.NewReader("token="+url.QueryEscape(tok)))
	request.Host = testHost
	request.RemoteAddr = addr
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// Sans cookie, une page ouverte par le navigateur propose de saisir le jeton, avec l'icône et le manifeste (ajout à l'écran
// d'accueil d'iOS) ; l'API garde son 401.
func TestTokenPage(t *testing.T) {
	resetAuth(t, nil)
	resetTokenTries()
	handler, _, _ := web(t)
	srv := withTokenOpen(modeTok, handler)

	request := httptest.NewRequest("GET", "/memoire", nil)
	request.Host = testHost
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	recorder := httptest.NewRecorder()
	srv.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	for _, want := range []string{`action="/token-login"`, `rel="apple-touch-icon"`, `rel="manifest"`, `type="password"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page du jeton sans %s : %d", want, recorder.Code)
		}
	}
	if strings.Contains(body, modeTok) {
		t.Fatal("la page contient le jeton")
	}
	if recorder := (modeReq{method: "GET", path: "/v1/state"}).do(srv); recorder.Code != 401 {
		t.Fatalf("API sans cookie : %d", recorder.Code)
	}

	same := "http://" + testHost
	if recorder := tokenForm(srv, modeTok, "https://ailleurs.example"); recorder.Code != 403 {
		t.Fatalf("formulaire d'une autre origine : %d", recorder.Code)
	}
	if recorder := tokenForm(srv, "faux-jeton-0123456789", same); recorder.Code != 401 || cookieOf(recorder, tokenCookie) != nil {
		t.Fatalf("jeton faux : %d", recorder.Code)
	}
	recorder = tokenForm(srv, modeTok, same)
	cookie := cookieOf(recorder, tokenCookie)
	if recorder.Code != http.StatusSeeOther || cookie == nil || cookie.SameSite != http.SameSiteStrictMode || !cookie.HttpOnly {
		t.Fatalf("bon jeton : %d %v", recorder.Code, recorder.Result().Cookies())
	}

	// Essais limités : au-delà, même le bon jeton attend.
	for range tokenMaxTries {
		tokenForm(srv, "faux-jeton-0123456789", same)
	}
	if recorder := tokenForm(srv, modeTok, same); recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("après %d échecs : %d", tokenMaxTries, recorder.Code)
	}
	// Une autre adresse n'est pas bloquée par la première.
	if recorder := tokenFormFrom(srv, modeTok, same, "198.51.100.7:4321"); recorder.Code != http.StatusSeeOther {
		t.Fatalf("autre adresse bloquée : %d", recorder.Code)
	}
	// Plafond global : beaucoup d'adresses ensemble finissent par bloquer le formulaire.
	resetTokenTries()
	for index := range tokenMaxGlobal {
		tokenFormFrom(srv, "faux-jeton-0123456789", same, fmt.Sprintf("203.0.113.%d:1", index%250))
	}
	if recorder := tokenFormFrom(srv, modeTok, same, "198.51.100.8:1"); recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("plafond global : %d", recorder.Code)
	}
}

// Des essais simultanés ne dépassent pas le plafond : la réservation et le contrôle se font sous le même verrou.
func TestTokenTryConcurrent(t *testing.T) {
	resetTokenTries()
	t.Cleanup(resetTokenTries)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if tokenTry("192.0.2.9") {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != tokenMaxTries {
		t.Fatalf("%d essais admis, plafond %d", ok.Load(), tokenMaxTries)
	}
	tokenRefund("192.0.2.9")
	if !tokenTry("192.0.2.9") {
		t.Fatal("un essai rendu n'est pas réutilisable")
	}
}
