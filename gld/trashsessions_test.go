package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCorbeilleSessions(t *testing.T) {
	h, cfg, _ := web(t)
	p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	sub := filepath.Join(cfg, "projects", "-p", id1)
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "agent.jsonl"), []byte("{}\n"), 0o600)
	body := `{"items":[{"dir":"-p","id":"` + id1 + `"}]}`

	ok(t, h, "/session-trash-add", body, true, "")
	if _, err := os.Stat(p); err == nil {
		t.Fatal("transcript encore en place")
	}
	_, m, _ := call(t, h, "GET", "/session-trash", "")
	if l, _ := m["sessions"].([]any); len(l) != 1 {
		t.Fatalf("corbeille : %v", m)
	}
	// restauration : transcript et sous-agents reviennent
	ok(t, h, "/session-trash-restore", `{"ids":["`+id1+`"]}`, true, "")
	if _, err := os.Stat(p); err != nil {
		t.Fatal("transcript non restauré")
	}
	if _, err := os.Stat(filepath.Join(sub, "agent.jsonl")); err != nil {
		t.Fatal("sous-agents non restaurés")
	}
	// suppression définitive depuis la corbeille : tout part
	ok(t, h, "/session-trash-add", body, true, "")
	ok(t, h, "/session-trash-purge", `{"ids":["`+id1+`"]}`, true, "")
	if _, err := os.Stat(p); err == nil {
		t.Fatal("transcript encore là après la purge")
	}
	_, m, _ = call(t, h, "GET", "/session-trash", "")
	if l, _ := m["sessions"].([]any); len(l) != 0 {
		t.Fatalf("corbeille non vidée : %v", m)
	}
	// un id invalide ne touche à rien
	status(t, h, "POST", "/session-trash-restore", `{"ids":["../x"]}`, 200)
}
