package main

import (
	"errors"
	"path/filepath"
	"testing"
)

// Sessions ouvertes indéterminées (claude agents a échoué) : tout ce qui écrit ou supprime refuse d'agir.
func TestLiveIndeterminee(t *testing.T) {
	cfg := fixture(t)
	use(cfg)
	defer func() { liveSessions = func() map[string]bool { return map[string]bool{} } }()
	liveSessions = func() map[string]bool { return nil }

	n, _, _, err := purgeSessions([]item{{"-p", id1}})
	if n != 0 || !errors.Is(err, errLive) || !exists(filepath.Join(cfg, "projects", "-p", id1+".jsonl")) {
		t.Fatalf("purge : n=%d err=%v, la session doit rester", n, err)
	}
	if err := checkPrompt(id1, "continue"); !errors.Is(err, errLive) {
		t.Fatalf("checkPrompt : %v", err)
	}
	if _, err := reassign([]item{{"-p", id1}}, cfg); !errors.Is(err, errLive) {
		t.Fatalf("reassign : %v", err)
	}
	if err := checkPrompt(id1, ""); err != nil { // sans message, rien à vérifier
		t.Fatalf("reprise sans message refusée : %v", err)
	}
}
