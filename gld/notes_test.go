package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		mux.ServeHTTP(w, httptest.NewRequest("PUT", "/v1/notes", strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	get := func() string {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/v1/notes", nil))
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
	if c, out := post(`{"key":"p/Big2","to":"p/Big"}`); c != 413 || !strings.Contains(out, `"too_large"`) || !strings.Contains(get(), "p/Big2") {
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

func TestNotesSent(t *testing.T) {
	old := root
	t.Cleanup(func() { root = old })
	root = t.TempDir() + "/projects"
	mux := http.NewServeMux()
	routesNotes(mux)
	do := func(method, url, body string) (int, string) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, url, strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	if c, _ := do("POST", "/v1/notes/sent", `{"key":"x","text":"t"}`); c != 400 {
		t.Fatal("clé invalide acceptée")
	}
	if c, _ := do("POST", "/v1/notes/sent", `{"key":"p/A","text":" "}`); c != 400 {
		t.Fatal("texte vide accepté")
	}
	for i := 0; i < sentMax+3; i++ {
		if c, out := do("POST", "/v1/notes/sent", `{"key":"p/A","text":"- item `+strconv.Itoa(i)+`","to":"s1"}`); c != 200 {
			t.Fatal(c, out)
		}
	}
	_, out := do("GET", "/v1/notes/sent?key=p/A", "")
	var got struct{ Sent []sentEntry }
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Sent) != sentMax || got.Sent[0].Text != "- item 3" {
		t.Fatal("plafond non appliqué :", len(got.Sent), out[:80])
	}
	if c, _ := do("DELETE", "/v1/notes/sent?key=p/A&id="+got.Sent[0].ID, ""); c != 200 {
		t.Fatal("suppression refusée")
	}
	_, out = do("GET", "/v1/notes/sent?key=p/A", "")
	if strings.Contains(out, "- item 3\"") || !strings.Contains(out, "- item 4\"") {
		t.Fatal("mauvaise ligne supprimée")
	}
	if _, out = do("GET", "/v1/notes/sent?key=p/B", ""); !strings.Contains(out, `"sent":[]`) {
		t.Fatal("historique vide attendu :", out)
	}
}
