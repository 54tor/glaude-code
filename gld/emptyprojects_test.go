package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepEmptyProjectDirsKeepsDirsWithFilesAndRecentOnes(t *testing.T) {
	previousRoot := root
	root = filepath.Join(t.TempDir(), "projects")
	t.Cleanup(func() { root = previousRoot })
	emptyDead := filepath.Join(root, "-tmp-dead", "memory")
	withTranscript := filepath.Join(root, "-tmp-keep")
	recentEmpty := filepath.Join(root, "-tmp-recent", "memory")
	realProject := filepath.Join(root, "-home-bob-proj", "memory")
	for _, dir := range []string{emptyDead, withTranscript, recentEmpty, realProject} {
		os.MkdirAll(dir, 0o755)
	}
	os.WriteFile(filepath.Join(withTranscript, "s.jsonl"), []byte("{}"), 0o600)
	old := time.Now().Add(-2 * time.Hour)
	for _, dir := range []string{filepath.Join(root, "-tmp-dead"), withTranscript, realProject, filepath.Dir(realProject)} {
		os.Chtimes(dir, old, old)
	}

	if removedCount := sweepEmptyProjectDirs(time.Now()); removedCount != 1 {
		t.Fatalf("valeurAttendue 1 dossier retiré, valeurObtenue %d", removedCount)
	}
	for path, shouldExist := range map[string]bool{filepath.Join(root, "-tmp-dead"): false, withTranscript: true, filepath.Dir(recentEmpty): true, filepath.Dir(realProject): true} {
		if _, err := os.Stat(path); (err == nil) != shouldExist {
			t.Errorf("%s : existe=%v, valeurAttendue %v", filepath.Base(path), err == nil, shouldExist)
		}
	}
}
