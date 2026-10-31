package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutoTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(path, []byte(`{"type":"user","message":{"content":"corrige le bug de login"}}`+"\n"), 0o600)
	old := titleRun
	defer func() { titleRun = old }()
	titleRun = func(string) (string, error) { return "  « Bug de login. »\n", nil }
	got, err := autoTitle(path)
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
	path := filepath.Join(t.TempDir(), "s.jsonl")
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Intro.\n\nSi vous préférez, dites-le."}]}}` + "\n"
	os.WriteFile(path, []byte(line), 0o600)
	if got := lastSaid(path); got != "Si vous préférez, dites-le." {
		t.Fatalf("got %q", got)
	}
}

func TestFlushTitlesWaitsForSessionEnd(t *testing.T) {
	old, oldLive, oldRoot := titleRun, liveSessions, root
	t.Cleanup(func() { titleRun, liveSessions, root = old, oldLive, oldRoot })
	cfg := t.TempDir()
	root = filepath.Join(cfg, "projects")
	dir := filepath.Join(root, "-p")
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, id1+".jsonl")
	os.WriteFile(path, []byte(`{"type":"user","message":{"content":"corrige le login"}}`+"\n"), 0o600)
	titleRun = func(string) (string, error) { return "Bug de login", nil }
	live := true
	liveSessions = func() map[string]bool { return map[string]bool{id1: live} }
	deferTitle(id1, path)
	flushTitles()
	if hasTitle(path) {
		t.Fatal("titre posé sur une session ouverte")
	}
	live = false
	flushTitles()
	if !hasTitle(path) {
		t.Fatal("titre non posé à la fin de la session")
	}
}
