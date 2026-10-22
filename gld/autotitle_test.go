package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutoTitle(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(`{"type":"user","message":{"content":"corrige le bug de login"}}`+"\n"), 0o600)
	old := titleRun
	defer func() { titleRun = old }()
	titleRun = func(string) (string, error) { return "  « Bug de login. »\n", nil }
	got, err := autoTitle(p)
	if err != nil || got != "Bug de login" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLineMsgsSkipsMeta(t *testing.T) {
	meta := []byte(`{"type":"user","isMeta":true,"message":{"content":"Base directory for this skill: /x"}}`)
	if got := lineMsgs(meta, true); len(got) != 0 {
		t.Fatalf("message méta affiché : %v", got)
	}
	real := []byte(`{"type":"user","message":{"content":"bonjour"}}`)
	if got := lineMsgs(real, true); len(got) != 1 || got[0].T != "bonjour" {
		t.Fatalf("message normal perdu : %v", got)
	}
}

func TestLastSaid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	l := `{"type":"assistant","message":{"content":[{"type":"text","text":"Intro.\n\nSi vous préférez, dites-le."}]}}` + "\n"
	os.WriteFile(p, []byte(l), 0o600)
	if got := lastSaid(p); got != "Si vous préférez, dites-le." {
		t.Fatalf("got %q", got)
	}
}
