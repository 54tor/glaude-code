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

	v1ok(t, h, "POST", "/v1/trash/sessions", body, "")
	if _, err := os.Stat(p); err == nil {
		t.Fatal("transcript encore en place")
	}
	_, m, _ := call(t, h, "GET", "/v1/trash/sessions", "")
	if l, _ := m["sessions"].([]any); len(l) != 1 {
		t.Fatalf("corbeille : %v", m)
	}
	// restauration : transcript et sous-agents reviennent
	v1ok(t, h, "PATCH", "/v1/trash/sessions?item="+id1, `{"trashed":false}`, "")
	if _, err := os.Stat(p); err != nil {
		t.Fatal("transcript non restauré")
	}
	if _, err := os.Stat(filepath.Join(sub, "agent.jsonl")); err != nil {
		t.Fatal("sous-agents non restaurés")
	}
	// suppression définitive depuis la corbeille : tout part
	v1ok(t, h, "POST", "/v1/trash/sessions", body, "")
	v1ok(t, h, "DELETE", "/v1/trash/sessions?item="+id1, "", "")
	if _, err := os.Stat(p); err == nil {
		t.Fatal("transcript encore là après la purge")
	}
	_, m, _ = call(t, h, "GET", "/v1/trash/sessions", "")
	if l, _ := m["sessions"].([]any); len(l) != 0 {
		t.Fatalf("corbeille non vidée : %v", m)
	}
	// un id invalide ne touche à rien
	v1err(t, h, "PATCH", "/v1/trash/sessions?item=..%2Fx", `{"trashed":false}`, 409, "")
}

func TestPurgeCorbeilleSansEtatDesSessions(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("{}\n"), 0o600)
	if n, _, err := trashSessions([]item{{"-p", id1}}); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	liveSessions = func() map[string]bool { return nil }
	defer use(cfg)
	if _, err := purgeTrashed([]string{id1}); err == nil {
		t.Fatal("la purge aurait dû échouer")
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("la session est revenue dans les projets")
	}
	if _, ok := readTrashMeta(id1); !ok {
		t.Fatal("la session a quitté la corbeille")
	}
}
