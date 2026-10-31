package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestLoopback(t *testing.T) {
	for header, want := range map[string]bool{"127.0.0.1": true, "::1": true, "localhost": true, "127.1.2.3": true, "0.0.0.0": false, "192.168.1.5": false, "::": false, "exemple.org": false} {
		if isLoopback(header) != want {
			t.Errorf("isLoopback(%q) = %v", header, !want)
		}
	}
}

func TestWithToken(t *testing.T) {
	const tok = "0123456789abcdef0123"
	ok := http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) { responseWriter.Write([]byte("ok")) })
	handler := withToken(tok, ok)
	do := func(request *http.Request) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	if recorder := do(httptest.NewRequest("GET", "/v1/state", nil)); recorder.Code != 401 {
		t.Fatal("sans jeton :", recorder.Code)
	}
	if recorder := do(httptest.NewRequest("GET", "/?token=faux", nil)); recorder.Code != 401 {
		t.Fatal("mauvais jeton :", recorder.Code)
	}
	if recorder := do(httptest.NewRequest("GET", "/?token=", nil)); recorder.Code != 401 {
		t.Fatal("jeton vide :", recorder.Code)
	}
	localRecorder := do(httptest.NewRequest("GET", "/memoire?token="+tok+"&x=1", nil))
	if localRecorder.Code != 303 || localRecorder.Header().Get("Location") != "/memoire?x=1" {
		t.Fatal("amorçage :", localRecorder.Code, localRecorder.Header().Get("Location"))
	}
	cookies := localRecorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie : %+v", cookies)
	}
	localRequest := httptest.NewRequest("GET", "/v1/state", nil)
	localRequest.AddCookie(cookies[0])
	if recorder := do(localRequest); recorder.Code != 200 {
		t.Fatal("avec cookie :", recorder.Code)
	}
	localRequest = httptest.NewRequest("GET", "/v1/state", nil)
	localRequest.Header.Set("Authorization", "Bearer "+tok)
	if recorder := do(localRequest); recorder.Code != 200 {
		t.Fatal("avec Bearer :", recorder.Code)
	}
	if checkToken("court") == nil || checkToken(tok) != nil {
		t.Fatal("longueur du jeton")
	}
}

func TestTakeToken(t *testing.T) {
	t.Setenv("GLD_WEB_TOKEN", "0123456789abcdef0123")
	t.Setenv(childTokenEnv, "enfant")
	if tok, gen, err := takeToken(true); err != nil || tok != "0123456789abcdef0123" || gen {
		t.Fatal("jeton fixé, en local :", tok, err)
	}
	for _, key := range []string{"GLD_WEB_TOKEN", childTokenEnv} {
		if _, ok := os.LookupEnv(key); ok {
			t.Fatal(key, "reste dans l'environnement : les processus lancés en hériteraient")
		}
	}
	if tok, _, _ := takeToken(true); tok != "" {
		t.Fatal("en local sans jeton fixé, aucun jeton n'est exigé :", tok)
	}
	if tok, gen, _ := takeToken(false); len(tok) != 48 || !gen {
		t.Fatal("hors localhost, un jeton est créé :", tok)
	}
	t.Setenv("GLD_WEB_TOKEN", "court")
	if _, _, err := takeToken(true); err == nil {
		t.Fatal("un jeton trop court est refusé")
	}
}

// Un jeton faux en Bearer, cookie ou ?token= use les essais de l'adresse ; le bon jeton n'en consomme aucun.
func TestWithTokenThrottlesWrongTokens(t *testing.T) {
	resetTokenTries()
	t.Cleanup(resetTokenTries)
	tok := "0123456789abcdef0123"
	handler := withToken(tok, http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) { responseWriter.WriteHeader(200) }))
	do := func(set func(*http.Request)) int {
		request := httptest.NewRequest("GET", "/v1/x", nil)
		request.RemoteAddr = "192.0.2.7:1234"
		set(request)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}
	for index := 0; index < tokenMaxTries+5; index++ {
		if counter := do(func(request *http.Request) { request.Header.Set("Authorization", "Bearer bad") }); index < tokenMaxTries && counter != 401 || index >= tokenMaxTries && counter != 429 {
			t.Fatalf("essai %d : %d", index, counter)
		}
	}
	if counter := do(func(request *http.Request) { request.Header.Set("Authorization", "Bearer "+tok) }); counter != 200 {
		t.Errorf("bon jeton après l'épuisement : %d", counter)
	}
	if counter := do(func(request *http.Request) { request.AddCookie(&http.Cookie{Name: tokenCookie, Value: "faux"}) }); counter != 429 {
		t.Errorf("cookie faux : %d", counter)
	}
	if counter := do(func(request *http.Request) {}); counter != 401 {
		t.Errorf("sans jeton : %d", counter)
	}
}

// Un cookie gld_token périmé (jeton changé) est effacé à son premier refus : le polling de l'interface ne doit pas épuiser
// les essais de l'adresse, sans quoi le nouveau jeton ne pourrait plus être saisi.
func TestCookiePerimeEfface(t *testing.T) {
	resetTokenTries()
	t.Cleanup(resetTokenTries)
	handler := withToken("0123456789abcdef0123", http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) { responseWriter.WriteHeader(200) }))
	localRequest := httptest.NewRequest("GET", "/v1/x", nil)
	localRequest.RemoteAddr = "192.0.2.8:1234"
	localRequest.AddCookie(&http.Cookie{Name: tokenCookie, Value: "ancien-jeton"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, localRequest)
	cleared := false
	for _, cookie := range recorder.Result().Cookies() {
		cleared = cleared || cookie.Name == tokenCookie && cookie.MaxAge < 0
	}
	if !cleared {
		t.Fatalf("cookie périmé non effacé : %v", recorder.Result().Cookies())
	}
}
