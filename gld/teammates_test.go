package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTeammateLivesSkipsLeader(t *testing.T) {
	cfg := t.TempDir()
	oldRoot := root
	root = filepath.Join(cfg, "projects")
	t.Cleanup(func() { root = oldRoot })
	teamDir := filepath.Join(cfg, "teams", "session-abcdef12")
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"members":[{"name":"team-lead"},{"name":"tests-ui","model":"sonnet"}]}`
	if err := os.WriteFile(filepath.Join(teamDir, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := teammateLives("abcdef12-0000-0000-0000-000000000000")
	if len(got) != 1 || got[0].Desc != "tests-ui" || got[0].Type != "sonnet" {
		t.Fatalf("got %+v", got)
	}
	if teammateLives("short") != nil {
		t.Fatal("short id must yield nothing")
	}
}
