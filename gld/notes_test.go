package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotes(t *testing.T) {
	old := root
	t.Cleanup(func() { root = old })
	root = t.TempDir() + "/projects"
	mux := http.NewServeMux()
	routesNotes(mux)
	post := func(body string) (int, string) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/notes", strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	get := func() string {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/notes", nil))
		return w.Body.String()
	}
	if c, _ := post(`{"key":"x/abc","text":"t"}`); c != 400 {
		t.Fatal("clé sans préfixe acceptée")
	}
	post(`{"key":"p/Alpha","text":"à faire"}`)
	if !strings.Contains(get(), "à faire") {
		t.Fatal(get())
	}
	post(`{"key":"p/Alpha","to":"p/Beta"}`)
	if g := get(); strings.Contains(g, "p/Alpha") || !strings.Contains(g, "p/Beta") {
		t.Fatal(g)
	}
	post(`{"key":"p/Gamma","text":"[\"garder\"]"}`)
	post(`{"key":"p/Beta","to":"p/Gamma"}`)
	if g := get(); !strings.Contains(g, "garder") || !strings.Contains(g, "à faire") {
		t.Fatal("déplacement sur une note existante :", g)
	}
	big := `{"key":"p/Big","text":"` + strings.Repeat("x", noteMax-10) + `"}`
	post(big)
	post(`{"key":"p/Big2","text":"` + strings.Repeat("y", noteMax-10) + `"}`)
	if _, out := post(`{"key":"p/Big2","to":"p/Big"}`); !strings.Contains(out, `"ok":false`) || !strings.Contains(get(), "p/Big2") {
		t.Fatal("fusion au-delà de noteMax acceptée :", out)
	}
	post(`{"key":"p/Gamma","to":"p/Gamma"}`)
	if !strings.Contains(get(), "p/Gamma") {
		t.Fatal("déplacement sur soi-même : note supprimée")
	}
	post(`{"key":"p/Beta","text":"  "}`)
	if strings.Contains(get(), "p/Beta") {
		t.Fatal("note vide non supprimée")
	}
}
