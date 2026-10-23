package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestLoopback(t *testing.T) {
	for h, want := range map[string]bool{"127.0.0.1": true, "::1": true, "localhost": true, "127.1.2.3": true, "0.0.0.0": false, "192.168.1.5": false, "::": false, "exemple.org": false} {
		if isLoopback(h) != want {
			t.Errorf("isLoopback(%q) = %v", h, !want)
		}
	}
}

func TestWithToken(t *testing.T) {
	const tok = "0123456789abcdef0123"
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	h := withToken(tok, ok)
	do := func(r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := do(httptest.NewRequest("GET", "/api", nil)); w.Code != 401 {
		t.Fatal("sans jeton :", w.Code)
	}
	if w := do(httptest.NewRequest("GET", "/?token=faux", nil)); w.Code != 401 {
		t.Fatal("mauvais jeton :", w.Code)
	}
	if w := do(httptest.NewRequest("GET", "/?token=", nil)); w.Code != 401 {
		t.Fatal("jeton vide :", w.Code)
	}
	w := do(httptest.NewRequest("GET", "/memoire?token="+tok+"&x=1", nil))
	if w.Code != 303 || w.Header().Get("Location") != "/memoire?x=1" {
		t.Fatal("amorçage :", w.Code, w.Header().Get("Location"))
	}
	c := w.Result().Cookies()
	if len(c) != 1 || !c[0].HttpOnly || c[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie : %+v", c)
	}
	r := httptest.NewRequest("GET", "/api", nil)
	r.AddCookie(c[0])
	if w := do(r); w.Code != 200 {
		t.Fatal("avec cookie :", w.Code)
	}
	r = httptest.NewRequest("GET", "/api", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	if w := do(r); w.Code != 200 {
		t.Fatal("avec Bearer :", w.Code)
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
	for _, k := range []string{"GLD_WEB_TOKEN", childTokenEnv} {
		if _, ok := os.LookupEnv(k); ok {
			t.Fatal(k, "reste dans l'environnement : les processus lancés en hériteraient")
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
