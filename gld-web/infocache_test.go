package main

import (
	"os"
	"path/filepath"
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
