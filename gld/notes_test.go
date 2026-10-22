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
	post(`{"key":"p/Beta","text":"  "}`)
	if strings.Contains(get(), "p/Beta") {
		t.Fatal("note vide non supprimée")
	}
}
