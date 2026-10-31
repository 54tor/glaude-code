package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoHomes : A (source, exportée) puis bascule sur B (cible vide) dans le même processus.
func importArchive(t *testing.T) (archive []byte, body string) {
	argument := exportEnv(t)
	put(t, filepath.Join(argument, "rules", "r.md"), "r\n", old)
	put(t, filepath.Join(argument, "projects", "-home-bob-p", "memory", "n.md"), "note\n", old)
	put(t, filepath.Join(argument, "projects", "-home-bob-p", "x.jsonl"), "{}\n", old)
	data, _, err := exportBackup([]string{"skills", "settings", "memory"})
	if err != nil {
		t.Fatal(err)
	}
	body = t.TempDir()
	use(body)
	t.Setenv("CLAUDE_CONFIG_DIR", body)
	return data, body
}

func TestStageBackupPlan(t *testing.T) {
	data, _ := importArchive(t)
	backupPlan, err := stageBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	defer dropStage(backupPlan.ID)
	if len(backupPlan.Items) == 0 {
		t.Fatal("plan vide")
	}
	for _, it := range backupPlan.Items {
		if it.Cat == "" || it.Status != "new" {
			t.Errorf("%+v", it)
		}
	}
	for id, cat := range map[string]string{"set:model": "settings", "skill:deploy": "skills", "file:CLAUDE.md": "memory", "file:rules/r.md": "memory"} {
		if it, ok := syncItem(backupPlan.Items, id); !ok || it.Cat != cat {
			t.Errorf("%s : %+v", id, it)
		}
	}
	if it, ok := syncItem(backupPlan.Items, "projmem:-home-bob-p"); !ok || it.Cat != "memory" {
		t.Errorf("projmem : %+v", it)
	}
}

func TestApplyBackupFiles(t *testing.T) {
	data, body := importArchive(t)
	put(t, filepath.Join(body, "CLAUDE.md"), "ancien\n", old)
	backupPlan, err := stageBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	res, err := applyBackup(backupPlan.ID, []SyncPick{{"file:CLAUDE.md", "replace"}, {"skill:deploy", "replace"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, backupResult := range res {
		if backupResult.Status != "ok" {
			t.Errorf("%+v", backupResult)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(body, "CLAUDE.md")); string(data) != "regles\n" {
		t.Errorf("CLAUDE.md : %q", data)
	}
	if _, err := os.Stat(filepath.Join(body, "skills", "deploy", "SKILL.md")); err != nil {
		t.Error(err)
	}
	var found bool
	filepath.WalkDir(filepath.Join(body, "gld-web-sync-backups"), func(path string, value os.DirEntry, _ error) error {
		if !value.IsDir() && strings.HasSuffix(path, "CLAUDE.md") {
			found = true
		}
		return nil
	})
	if !found {
		t.Error("sauvegarde absente")
	}
}

func TestApplyBackupProjectTargets(t *testing.T) {
	data, body := importArchive(t)
	if err := os.MkdirAll(filepath.Join(body, "projects", "-home-bob-q"), 0o755); err != nil {
		t.Fatal(err)
	}
	pick := []SyncPick{{"projmem:-home-bob-p", "replace"}}
	backupPlan, _ := stageBackup(data)
	res, err := applyBackup(backupPlan.ID, pick, nil)
	if err != nil || len(res) != 1 || res[0].Status != "skipped" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(body, "projects", "-home-bob-p")); err == nil {
		t.Error("dossier créé sans cible")
	}
	backupPlan, _ = stageBackup(data)
	res, err = applyBackup(backupPlan.ID, pick, map[string]string{"-home-bob-p": "-home-bob-q"})
	if err != nil || res[0].Status != "ok" {
		t.Fatalf("%+v %v", res, err)
	}
	if data, _ := os.ReadFile(filepath.Join(body, "projects", "-home-bob-q", "memory", "n.md")); string(data) != "note\n" {
		t.Errorf("%q", data)
	}
	backupPlan, _ = stageBackup(data)
	if _, err := applyBackup(backupPlan.ID, pick, map[string]string{"-home-bob-p": "../x"}); err != nil {
		t.Fatal(err)
	}
}

func TestStageBackupIdempotent(t *testing.T) {
	data, _ := importArchive(t)
	backupPlan, _ := stageBackup(data)
	var picks []SyncPick
	for _, it := range backupPlan.Items {
		picks = append(picks, SyncPick{it.ID, "replace"})
	}
	if _, err := applyBackup(backupPlan.ID, picks, nil); err != nil {
		t.Fatal(err)
	}
	backupPlan, err := stageBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	defer dropStage(backupPlan.ID)
	for _, it := range backupPlan.Items {
		if it.Status != "same" && !strings.HasPrefix(it.ID, "projmem:") {
			t.Errorf("%+v", it)
		}
	}
}

func TestApplyBackupUnknownID(t *testing.T) {
	data, _ := importArchive(t)
	if _, err := applyBackup("nope", nil, nil); err == nil {
		t.Error("id inconnu accepté")
	}
	backupPlan, _ := stageBackup(data)
	if _, err := applyBackup(backupPlan.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := applyBackup(backupPlan.ID, nil, nil); err == nil {
		t.Error("étape réutilisée")
	}
}

func TestStageEviction(t *testing.T) {
	data, _ := importArchive(t)
	var first, firstDir string
	for index := 0; index < 5; index++ {
		backupPlan, err := stageBackup(data)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = backupPlan.ID
			stagesMu.Lock()
			firstDir = stages[first].dir
			stagesMu.Unlock()
		}
		defer dropStage(backupPlan.ID)
	}
	if _, err := os.Stat(firstDir); err == nil {
		t.Error("dossier de la plus ancienne étape conservé")
	}
	if _, err := applyBackup(first, nil, nil); err == nil {
		t.Error("étape évincée appliquée")
	}
}

func TestApplyBackupConcurrentClaim(t *testing.T) {
	data, _ := importArchive(t)
	backupPlan, err := stageBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	picks := []SyncPick{{"file:CLAUDE.md", "replace"}, {"projmem:-home-bob-p", "replace"}}
	errs := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() { _, err := applyBackup(backupPlan.ID, picks, nil); errs <- err }()
	}
	e1, e2 := <-errs, <-errs
	if (e1 == nil) == (e2 == nil) {
		t.Fatalf("exactement une application attendue : %v / %v", e1, e2)
	}
}

func TestBackupSkillExecBitRoundTrip(t *testing.T) {
	argument := exportEnv(t)
	put(t, filepath.Join(argument, "skills", "deploy", "run.sh"), "#!/bin/sh\n", old)
	if err := os.Chmod(filepath.Join(argument, "skills", "deploy", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _, err := exportBackup([]string{"skills"})
	if err != nil {
		t.Fatal(err)
	}
	body := t.TempDir()
	use(body)
	t.Setenv("CLAUDE_CONFIG_DIR", body)
	backupPlan, err := stageBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyBackup(backupPlan.ID, []SyncPick{{"skill:deploy", "replace"}}, nil); err != nil {
		t.Fatal(err)
	}
	for filePath, want := range map[string]bool{"run.sh": true, "SKILL.md": false} {
		fi, err := os.Stat(filepath.Join(body, "skills", "deploy", filePath))
		if err != nil || (fi.Mode()&0o111 != 0) != want {
			t.Errorf("%s : %v exec=%v", filePath, err, want)
		}
	}
}
