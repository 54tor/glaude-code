package main

import (
	"os"
	"path/filepath"
	"testing"
)

const idOrphelin = "99999999-9999-9999-9999-999999999999"

func TestScratchOrphansLaisseLesSessionsAvecTranscript(t *testing.T) {
	cfg := fixture(t)
	use(cfg)
	liveSessions = func() map[string]bool { return map[string]bool{} }
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CODE_TMPDIR", tmp)
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"/p"}`+"\n", old)
	put(t, filepath.Join(baseTemp(), "-p", id1, "a.txt"), "x", old)
	put(t, filepath.Join(baseTemp(), "-p", idOrphelin, "b.txt"), "x", old)
	orphans, err := scratchOrphans()
	if err != nil || len(orphans) != 1 || filepath.Base(orphans[0].path) != idOrphelin {
		t.Fatalf("valeurObtenue=%v err=%v, valeurAttendue=[%s]", orphans, err, idOrphelin)
	}
}

func TestScratchOrphansEpargneLaSessionVivante(t *testing.T) {
	use(fixture(t))
	liveSessions = func() map[string]bool { return map[string]bool{idOrphelin: true} }
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CODE_TMPDIR", tmp)
	put(t, filepath.Join(baseTemp(), "-p", idOrphelin, "b.txt"), "x", old)
	if orphans, _ := scratchOrphans(); len(orphans) != 0 {
		t.Fatalf("valeurObtenue=%v, valeurAttendue=aucun", orphans)
	}
}

func baseTemp() string {
	bases, _ := scratchDirs("", "")
	return bases[0]
}

func TestScratchOrphansIgnoreUneBaseLienSymbolique(t *testing.T) {
	use(fixture(t))
	liveSessions = func() map[string]bool { return map[string]bool{} }
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CODE_TMPDIR", tmp)
	cible := t.TempDir()
	put(t, filepath.Join(cible, "-p", idOrphelin, "b.txt"), "x", old)
	if err := os.Symlink(cible, baseTemp()); err != nil {
		t.Skip("liens symboliques indisponibles")
	}
	if orphans, _ := scratchOrphans(); len(orphans) != 0 {
		t.Fatalf("valeurObtenue=%v, valeurAttendue=aucun", orphans)
	}
}
