package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCorbeilleSessions(t *testing.T) {
	handler, cfg, _ := web(t)
	path := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	sub := filepath.Join(cfg, "projects", "-p", id1)
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "agent.jsonl"), []byte("{}\n"), 0o600)
	body := `{"items":[{"dir":"-p","id":"` + id1 + `"}]}`

	v1ok(t, handler, "POST", "/v1/trash/sessions", body, "")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("transcript encore en place")
	}
	_, lookup, _ := call(t, handler, "GET", "/v1/trash/sessions", "")
	if values, _ := lookup["sessions"].([]any); len(values) != 1 {
		t.Fatalf("corbeille : %v", lookup)
	}
	// restauration : transcript et sous-agents reviennent
	v1ok(t, handler, "PATCH", "/v1/trash/sessions?item="+id1, `{"trashed":false}`, "")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("transcript non restauré")
	}
	if _, err := os.Stat(filepath.Join(sub, "agent.jsonl")); err != nil {
		t.Fatal("sous-agents non restaurés")
	}
	// suppression définitive depuis la corbeille : tout part
	v1ok(t, handler, "POST", "/v1/trash/sessions", body, "")
	v1ok(t, handler, "DELETE", "/v1/trash/sessions?item="+id1, "", "")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("transcript encore là après la purge")
	}
	_, lookup, _ = call(t, handler, "GET", "/v1/trash/sessions", "")
	if values, _ := lookup["sessions"].([]any); len(values) != 0 {
		t.Fatalf("corbeille non vidée : %v", lookup)
	}
	// un id invalide ne touche à rien
	v1err(t, handler, "PATCH", "/v1/trash/sessions?item=..%2Fx", `{"trashed":false}`, 409, "")
}

func TestPurgeCorbeilleSansEtatDesSessions(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	path := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("{}\n"), 0o600)
	if count, _, err := trashSessions([]item{{"-p", id1}}); count != 1 || err != nil {
		t.Fatal(count, err)
	}
	liveSessions = func() map[string]bool { return nil }
	defer use(cfg)
	if _, err := purgeTrashed([]string{id1}); err == nil {
		t.Fatal("la purge aurait dû échouer")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("la session est revenue dans les projets")
	}
	if _, ok := readTrashMeta(id1); !ok {
		t.Fatal("la session a quitté la corbeille")
	}
}

func TestCorbeilleEmporteSousSessions(t *testing.T) {
	handler, cfg, _ := web(t)
	dir := filepath.Join(cfg, "projects", "-p")
	child := "33333333-2222-3333-4444-555555555555"
	line := `{"type":"user","teamName":"session-` + id1[:8] + `","agentName":"w","message":{"role":"user","content":"x"}}` + "\n"
	os.WriteFile(filepath.Join(dir, child+".jsonl"), []byte(line), 0o600)
	v1ok(t, handler, "POST", "/v1/trash/sessions", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, "")
	if _, err := os.Stat(filepath.Join(dir, child+".jsonl")); err == nil {
		t.Fatal("sous-session non supprimée avec sa parente")
	}
}
