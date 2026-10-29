package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoHomes : A (source, exportée) puis bascule sur B (cible vide) dans le même processus.
func importArchive(t *testing.T) (archive []byte, b string) {
	a := exportEnv(t)
	put(t, filepath.Join(a, "rules", "r.md"), "r\n", old)
	put(t, filepath.Join(a, "projects", "-home-bob-p", "memory", "n.md"), "note\n", old)
	put(t, filepath.Join(a, "projects", "-home-bob-p", "x.jsonl"), "{}\n", old)
	z, _, err := exportBackup([]string{"skills", "settings", "memory"})
	if err != nil {
		t.Fatal(err)
	}
	b = t.TempDir()
	use(b)
	t.Setenv("CLAUDE_CONFIG_DIR", b)
	return z, b
}

func TestStageBackupPlan(t *testing.T) {
	z, _ := importArchive(t)
	p, err := stageBackup(z)
	if err != nil {
		t.Fatal(err)
	}
	defer dropStage(p.ID)
	if len(p.Items) == 0 {
		t.Fatal("plan vide")
	}
	for _, it := range p.Items {
		if it.Cat == "" || it.Status != "new" {
			t.Errorf("%+v", it)
		}
	}
	for id, cat := range map[string]string{"set:model": "settings", "skill:deploy": "skills", "file:CLAUDE.md": "memory", "file:rules/r.md": "memory"} {
		if it, ok := syncItem(p.Items, id); !ok || it.Cat != cat {
			t.Errorf("%s : %+v", id, it)
		}
	}
	if it, ok := syncItem(p.Items, "projmem:-home-bob-p"); !ok || it.Cat != "memory" {
		t.Errorf("projmem : %+v", it)
	}
}

func TestApplyBackupFiles(t *testing.T) {
	z, b := importArchive(t)
	put(t, filepath.Join(b, "CLAUDE.md"), "ancien\n", old)
	p, err := stageBackup(z)
	if err != nil {
		t.Fatal(err)
	}
	res, err := applyBackup(p.ID, []SyncPick{{"file:CLAUDE.md", "replace"}, {"skill:deploy", "replace"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Status != "ok" {
			t.Errorf("%+v", r)
		}
	}
	if c, _ := os.ReadFile(filepath.Join(b, "CLAUDE.md")); string(c) != "regles\n" {
		t.Errorf("CLAUDE.md : %q", c)
	}
	if _, err := os.Stat(filepath.Join(b, "skills", "deploy", "SKILL.md")); err != nil {
		t.Error(err)
	}
	var found bool
	filepath.WalkDir(filepath.Join(b, "gld-web-sync-backups"), func(p string, d os.DirEntry, _ error) error {
		if !d.IsDir() && strings.HasSuffix(p, "CLAUDE.md") {
			found = true
		}
		return nil
	})
	if !found {
		t.Error("sauvegarde absente")
	}
}

func TestApplyBackupProjectTargets(t *testing.T) {
	z, b := importArchive(t)
	if err := os.MkdirAll(filepath.Join(b, "projects", "-home-bob-q"), 0o755); err != nil {
		t.Fatal(err)
	}
	pick := []SyncPick{{"projmem:-home-bob-p", "replace"}}
	p, _ := stageBackup(z)
	res, err := applyBackup(p.ID, pick, nil)
	if err != nil || len(res) != 1 || res[0].Status != "skipped" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(b, "projects", "-home-bob-p")); err == nil {
		t.Error("dossier créé sans cible")
	}
	p, _ = stageBackup(z)
	res, err = applyBackup(p.ID, pick, map[string]string{"-home-bob-p": "-home-bob-q"})
	if err != nil || res[0].Status != "ok" {
		t.Fatalf("%+v %v", res, err)
	}
	if c, _ := os.ReadFile(filepath.Join(b, "projects", "-home-bob-q", "memory", "n.md")); string(c) != "note\n" {
		t.Errorf("%q", c)
	}
	p, _ = stageBackup(z)
	if _, err := applyBackup(p.ID, pick, map[string]string{"-home-bob-p": "../x"}); err != nil {
		t.Fatal(err)
	}
}

func TestStageBackupIdempotent(t *testing.T) {
	z, _ := importArchive(t)
	p, _ := stageBackup(z)
	var picks []SyncPick
	for _, it := range p.Items {
		picks = append(picks, SyncPick{it.ID, "replace"})
	}
	if _, err := applyBackup(p.ID, picks, nil); err != nil {
		t.Fatal(err)
	}
	p, err := stageBackup(z)
	if err != nil {
		t.Fatal(err)
	}
	defer dropStage(p.ID)
	for _, it := range p.Items {
		if it.Status != "same" && !strings.HasPrefix(it.ID, "projmem:") {
			t.Errorf("%+v", it)
		}
	}
}

func TestApplyBackupUnknownID(t *testing.T) {
	z, _ := importArchive(t)
	if _, err := applyBackup("nope", nil, nil); err == nil {
		t.Error("id inconnu accepté")
	}
	p, _ := stageBackup(z)
	if _, err := applyBackup(p.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := applyBackup(p.ID, nil, nil); err == nil {
		t.Error("étape réutilisée")
	}
}

func TestStageEviction(t *testing.T) {
	z, _ := importArchive(t)
	var first, firstDir string
	for i := 0; i < 5; i++ {
		p, err := stageBackup(z)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = p.ID
			stagesMu.Lock()
			firstDir = stages[first].dir
			stagesMu.Unlock()
		}
		defer dropStage(p.ID)
	}
	if _, err := os.Stat(firstDir); err == nil {
		t.Error("dossier de la plus ancienne étape conservé")
	}
	if _, err := applyBackup(first, nil, nil); err == nil {
		t.Error("étape évincée appliquée")
	}
}

func TestApplyBackupConcurrentClaim(t *testing.T) {
	z, _ := importArchive(t)
	p, err := stageBackup(z)
	if err != nil {
		t.Fatal(err)
	}
	picks := []SyncPick{{"file:CLAUDE.md", "replace"}, {"projmem:-home-bob-p", "replace"}}
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, e := applyBackup(p.ID, picks, nil); errs <- e }()
	}
	e1, e2 := <-errs, <-errs
	if (e1 == nil) == (e2 == nil) {
		t.Fatalf("exactement une application attendue : %v / %v", e1, e2)
	}
}
