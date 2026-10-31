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
	leadID := "abcdef12-0000-0000-0000-000000000000"
	mateID := "11111111-2222-3333-4444-555555555555"
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	leadPath := filepath.Join(projectDir, leadID+".jsonl")
	header := `{"type":"mode"}` + "\n" + `{"teamName":"session-abcdef12","agentName":"tests-ui"}` + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, mateID+".jsonl"), []byte(header), 0o600); err != nil {
		t.Fatal(err)
	}
	got := teammateLives(leadPath, leadID)
	if len(got) != 1 || got[0].Desc != "tests-ui" || got[0].Type != "sonnet" || got[0].ID != mateID {
		t.Fatalf("got %+v", got)
	}
	if !isTeamTranscript(leadPath, leadID, mateID) || isTeamTranscript(leadPath, leadID, leadID) {
		t.Fatal("teammate membership wrong")
	}
	if teammateLives(leadPath, "short") != nil {
		t.Fatal("short id must yield nothing")
	}
}
