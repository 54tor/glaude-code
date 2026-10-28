package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewind(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	ls := []string{
		`{"type":"user","message":{"content":"un"}}`,
		`{"type":"assistant","message":{"id":"a1","content":[{"type":"text","text":"je lance"}]}}`,
		`{"type":"assistant","message":{"id":"a1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		`{"type":"assistant","message":{"id":"a2","content":[{"type":"text","text":"fini"}]}}`,
		`{"type":"user","message":{"content":"deux"}}`,
		`{"type":"custom-title","customTitle":"T"}`,
		`{"type":"assistant","message":{"id":"a3","content":[{"type":"text","text":"réponse"}]}}`,
	}
	put(t, p, strings.Join(ls, "\n")+"\n", old)

	// Clic sur « je lance » : l'appel d'outil du même message et son résultat restent, le titre aussi.
	n, err := rewind(p, int64(len(ls[0])+1))
	if err != nil || n != 3 {
		t.Fatalf("rewind = %d, %v", n, err)
	}
	b, _ := os.ReadFile(p)
	if want := strings.Join(append(ls[:4:4], ls[6]), "\n") + "\n"; string(b) != want {
		t.Fatalf("transcript =\n%s", b)
	}
	if _, err := rewind(p, 3); err == nil {
		t.Fatal("un décalage hors début de ligne doit être refusé")
	}
}
