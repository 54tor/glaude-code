package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest("PUT", "/v1/notes", strings.NewReader(body)))
		return recorder.Code, recorder.Body.String()
	}
	get := func() string {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest("GET", "/v1/notes", nil))
		return recorder.Body.String()
	}
	if counter, _ := post(`{"key":"x/abc","text":"t"}`); counter != 400 {
		t.Fatal("clé sans préfixe acceptée")
	}
	post(`{"key":"p/Alpha","text":"à faire"}`)
	if !strings.Contains(get(), "à faire") {
		t.Fatal(get())
	}
	post(`{"key":"p/Alpha","to":"p/Beta"}`)
	if group := get(); strings.Contains(group, "p/Alpha") || !strings.Contains(group, "p/Beta") {
		t.Fatal(group)
	}
	post(`{"key":"p/Gamma","text":"[\"garder\"]"}`)
	post(`{"key":"p/Beta","to":"p/Gamma"}`)
	if group := get(); !strings.Contains(group, "garder") || !strings.Contains(group, "à faire") {
		t.Fatal("déplacement sur une note existante :", group)
	}
	big := `{"key":"p/Big","text":"` + strings.Repeat("x", noteMax-10) + `"}`
	post(big)
	post(`{"key":"p/Big2","text":"` + strings.Repeat("y", noteMax-10) + `"}`)
	if counter, out := post(`{"key":"p/Big2","to":"p/Big"}`); counter != 413 || !strings.Contains(out, `"too_large"`) || !strings.Contains(get(), "p/Big2") {
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
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(method, url, strings.NewReader(body)))
		return recorder.Code, recorder.Body.String()
	}
	if counter, _ := do("POST", "/v1/notes/sent", `{"key":"x","text":"t"}`); counter != 400 {
		t.Fatal("clé invalide acceptée")
	}
	if counter, _ := do("POST", "/v1/notes/sent", `{"key":"p/A","text":" "}`); counter != 400 {
		t.Fatal("texte vide accepté")
	}
	for index := 0; index < sentMax+3; index++ {
		if counter, out := do("POST", "/v1/notes/sent", `{"key":"p/A","text":"- item `+strconv.Itoa(index)+`","to":"s1"}`); counter != 200 {
			t.Fatal(counter, out)
		}
	}
	_, out := do("GET", "/v1/notes/sent?key=p/A", "")
	var got struct{ Sent []sentEntry }
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Sent) != sentMax || got.Sent[0].Text != "- item 3" {
		t.Fatal("plafond non appliqué :", len(got.Sent), out[:80])
	}
	if counter, _ := do("DELETE", "/v1/notes/sent?key=p/A&id="+got.Sent[0].ID, ""); counter != 200 {
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

// Un fichier de notes existant mais illisible (droits 0000) ne doit jamais être remplacé par une seule note :
// l'erreur de lecture n'est pas une absence.
func TestNotesRefuseDEcraserUnFichierIllisible(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root lit un fichier 0000")
	}
	previousRoot := root
	t.Cleanup(func() { root = previousRoot })
	root = t.TempDir() + "/projects"
	contenuAttendu := []byte(`{"p/Alpha":"à garder"}`)
	if writeErr := os.WriteFile(notesFile(), contenuAttendu, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if chmodErr := os.Chmod(notesFile(), 0); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	t.Cleanup(func() { os.Chmod(notesFile(), 0o600) })
	mux := http.NewServeMux()
	routesNotes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("PUT", "/v1/notes", strings.NewReader(`{"key":"p/Beta","text":"nouvelle"}`)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("écriture acceptée sur un fichier illisible : %d %s", recorder.Code, recorder.Body.String())
	}
	os.Chmod(notesFile(), 0o600)
	contenuObtenu, _ := os.ReadFile(notesFile())
	if string(contenuObtenu) != string(contenuAttendu) {
		t.Fatalf("fichier de notes réécrit : %s", contenuObtenu)
	}
}
