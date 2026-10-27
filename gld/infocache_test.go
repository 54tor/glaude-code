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
	p := filepath.Join(t.TempDir(), id1+".jsonl")
	write := func(s string, app bool) {
		flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		if app {
			flag = os.O_WRONLY | os.O_APPEND
		}
		f, err := os.OpenFile(p, flag, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(s)
		f.Close()
	}
	get := func() Session {
		st, _ := os.Stat(p)
		return info(p, st)
	}
	write(`{"type":"user","cwd":"/x","message":{"content":"premier prompt"}}`+"\n", false)
	if s := get(); s.Title != "premier prompt" || s.Cwd != "/x" {
		t.Fatalf("%+v", s)
	}
	// ajout : un titre, puis une ligne inachevée (sans « \n »), complétée ensuite
	time.Sleep(10 * time.Millisecond)
	write(`{"type":"custom-title","customTitle":"Mon titre"}`+"\n"+`{"type":"user","mes`, true)
	if s := get(); s.Title != "Mon titre" || !s.Named {
		t.Fatalf("titre : %+v", s)
	}
	write(`sage":{"content":"suite"}}`+"\n"+`{"type":"custom-title","customTitle":"Titre final"}`+"\n", true)
	got := get()
	forgetInfo(p)
	if want := get(); got != want || got.Title != "Titre final" {
		t.Fatalf("incrémental %+v, complet %+v", got, want)
	}
	// tronqué : relu en entier
	write(`{"type":"user","cwd":"/y","message":{"content":"autre"}}`+"\n", false)
	if s := get(); s.Title != "autre" || s.Cwd != "/y" {
		t.Fatalf("après troncature : %+v", s)
	}
}

// Les lignes d'un même message répètent son usage : il n'est compté qu'une fois, même relu après une ligne inachevée.
func TestInfoTokens(t *testing.T) {
	p := filepath.Join(t.TempDir(), id1+".jsonl")
	a := func(id string, in, out int) string {
		return `{"type":"assistant","message":{"id":"` + id + `","content":[],"usage":{"input_tokens":` + itoa(in) +
			`,"output_tokens":` + itoa(out) + `,"cache_read_input_tokens":100,"cache_creation_input_tokens":10}}}`
	}
	// m1 sur deux lignes (usage final 5/20), m2 une ligne sans « \n » final
	if err := os.WriteFile(p, []byte(a("m1", 5, 3)+"\n"+a("m1", 5, 20)+"\n"+a("m2", 1, 4)), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	got := info(p, st).Tok
	st, _ = os.Stat(p)
	again := info(p, st).Tok // même contenu : pas de double compte
	if want := (Tokens{In: 6, Out: 24, CR: 200, CW: 20}); got != want || again != want {
		t.Fatalf("tokens = %+v puis %+v, attendu %+v", got, again, want)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// Une session lancée par une équipe prend le nom de son rôle comme titre et pointe vers sa parente.
func TestSessionsTeamParent(t *testing.T) {
	old := root
	t.Cleanup(func() { root = old })
	root = t.TempDir()
	d := filepath.Join(root, "-p")
	os.MkdirAll(d, 0o700)
	par, kid := "dbaae837-e50f-4018-9940-09a76cd704db", "8f3f05e4-e61f-4ff3-844a-c62c939c6805"
	os.WriteFile(filepath.Join(d, par+".jsonl"), []byte(`{"type":"user","cwd":"/x","message":{"content":"parent"}}`+"\n"), 0o600)
	os.WriteFile(filepath.Join(d, kid+".jsonl"), []byte(`{"teamName":"session-dbaae837","agentName":"impl-task-4","type":"user","cwd":"/x","message":{"content":"tache"}}`+"\n"), 0o600)
	got := map[string]Session{}
	for _, g := range sessions() {
		for _, s := range g.Sessions {
			got[s.ID] = s
		}
	}
	if k := got[kid]; k.Parent != par || k.Title != "impl-task-4" {
		t.Fatalf("enfant : %+v", k)
	}
	if got[par].Parent != "" || got[par].Title != "parent" {
		t.Fatalf("parent : %+v", got[par])
	}
}
