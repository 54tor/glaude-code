package main

import (
	"net/http"
	"net/http/httptest"
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
