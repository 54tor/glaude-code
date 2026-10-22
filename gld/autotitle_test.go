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
