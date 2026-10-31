package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// La lecture incrémentale d'un transcript qui grandit donne le même résultat qu'une lecture complète.
func TestInfoIncremental(t *testing.T) {
	path := filepath.Join(t.TempDir(), id1+".jsonl")
	write := func(text string, app bool) {
		flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		if app {
			flag = os.O_WRONLY | os.O_APPEND
		}
		file, err := os.OpenFile(path, flag, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		file.WriteString(text)
		file.Close()
	}
	get := func() Session {
		st, _ := os.Stat(path)
		return info(path, st)
	}
	write(`{"type":"user","cwd":"/x","message":{"content":"premier prompt"}}`+"\n", false)
	if session := get(); session.Title != "premier prompt" || session.Cwd != "/x" {
		t.Fatalf("%+v", session)
	}
	// ajout : un titre, puis une ligne inachevée (sans « \n »), complétée ensuite
	time.Sleep(10 * time.Millisecond)
	write(`{"type":"custom-title","customTitle":"Mon titre"}`+"\n"+`{"type":"user","mes`, true)
	if session := get(); session.Title != "Mon titre" || !session.Named {
		t.Fatalf("titre : %+v", session)
	}
	write(`sage":{"content":"suite"}}`+"\n"+`{"type":"custom-title","customTitle":"Titre final"}`+"\n", true)
	got := get()
	forgetInfo(path)
	if want := get(); got != want || got.Title != "Titre final" {
		t.Fatalf("incrémental %+v, complet %+v", got, want)
	}
	// tronqué : relu en entier
	write(`{"type":"user","cwd":"/y","message":{"content":"autre"}}`+"\n", false)
	if session := get(); session.Title != "autre" || session.Cwd != "/y" {
		t.Fatalf("après troncature : %+v", session)
	}
}

// Les lignes d'un même message répètent son usage : il n'est compté qu'une fois, même relu après une ligne inachevée.
func TestInfoTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), id1+".jsonl")
	callback := func(id string, in, out int) string {
		return `{"type":"assistant","message":{"id":"` + id + `","content":[],"usage":{"input_tokens":` + itoa(in) +
			`,"output_tokens":` + itoa(out) + `,"cache_read_input_tokens":100,"cache_creation_input_tokens":10}}}`
	}
	// m1 sur deux lignes (usage final 5/20), m2 une ligne sans « \n » final
	if err := os.WriteFile(path, []byte(callback("m1", 5, 3)+"\n"+callback("m1", 5, 20)+"\n"+callback("m2", 1, 4)), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	got := info(path, st).Tok
	st, _ = os.Stat(path)
	again := info(path, st).Tok // même contenu : pas de double compte
	if want := (Tokens{In: 6, Out: 24, CR: 200, CW: 20}); got != want || again != want {
		t.Fatalf("tokens = %+v puis %+v, attendu %+v", got, again, want)
	}
}

func itoa(count int) string { return strconv.Itoa(count) }

// Une session lancée par une équipe prend le nom de son rôle comme titre et pointe vers sa parente.
func TestSessionsTeamParent(t *testing.T) {
	old := root
	t.Cleanup(func() { root = old })
	root = t.TempDir()
	dir := filepath.Join(root, "-p")
	os.MkdirAll(dir, 0o700)
	par, kid := "dbaae837-e50f-4018-9940-09a76cd704db", "8f3f05e4-e61f-4ff3-844a-c62c939c6805"
	os.WriteFile(filepath.Join(dir, par+".jsonl"), []byte(`{"type":"user","cwd":"/x","message":{"content":"parent"}}`+"\n"), 0o600)
	os.WriteFile(filepath.Join(dir, kid+".jsonl"), []byte(`{"teamName":"session-dbaae837","agentName":"impl-task-4","type":"user","cwd":"/x","message":{"content":"tache"}}`+"\n"), 0o600)
	got := map[string]Session{}
	for _, group := range sessions() {
		for _, session := range group.Sessions {
			got[session.ID] = session
		}
	}
	if session := got[kid]; session.Parent != par || session.Title != "impl-task-4" {
		t.Fatalf("enfant : %+v", session)
	}
	if got[par].Parent != "" || got[par].Title != "parent" {
		t.Fatalf("parent : %+v", got[par])
	}
}
