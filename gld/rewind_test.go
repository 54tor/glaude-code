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
	path := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
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
	put(t, path, strings.Join(ls, "\n")+"\n", old)

	// Clic sur « je lance » : l'appel d'outil du même message et son résultat restent, le titre aussi.
	count, err := rewind(path, int64(len(ls[0])+1))
	if err != nil || count != 3 {
		t.Fatalf("rewind = %d, %v", count, err)
	}
	data, _ := os.ReadFile(path)
	if want := strings.Join(append(ls[:4:4], ls[6]), "\n") + "\n"; string(data) != want {
		t.Fatalf("transcript =\n%s", data)
	}
	if _, err := rewind(path, 3); err == nil {
		t.Fatal("un décalage hors début de ligne doit être refusé")
	}
}
