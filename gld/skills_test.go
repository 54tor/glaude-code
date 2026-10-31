package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkills(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "skills", "revue", "SKILL.md"), "---\nname: revue\ndescription: Relit un diff\ndisable-model-invocation: true\nallowed-tools: Read Grep\n---\n\nÉtapes…\n", old)
	put(t, filepath.Join(cfg, "skills", "sans-desc", "SKILL.md"), "# Titre du skill\n\ncorps\n", old)
	put(t, filepath.Join(cfg, "skills", "avec-fichiers", "SKILL.md"), "---\ndescription: x\n---\nx\n", old)
	put(t, filepath.Join(cfg, "skills", "avec-fichiers", "scripts", "go.sh"), "echo\n", old)
	os.Chmod(filepath.Join(cfg, "skills", "avec-fichiers", "scripts", "go.sh"), 0o755)
	put(t, filepath.Join(cfg, "skills", "dossier-sans-skill", "notes.txt"), "rien\n", old)
	put(t, filepath.Join(proj, ".claude", "skills", "local", "SKILL.md"), "---\ndescription: Skill du projet\n---\nx\n", old)
	plug := filepath.Join(cfg, "plugins", "cache", "p", "1")
	put(t, filepath.Join(plug, "skills", "du-plugin", "SKILL.md"), "---\nname: du-plugin\ndescription: Fourni par un plugin\n---\nx\n", old)
	put(t, filepath.Join(cfg, "plugins", "installed_plugins.json"), `{"version":2,"plugins":{"p@m":[{"installPath":"`+plug+`"}]}}`, old)

	by := map[string]Skill{}
	for _, skill := range listSkills() {
		by[skill.Key] = skill
	}
	if len(by) != 5 {
		t.Fatalf("%d skills : %v", len(by), by)
	}
	if skill := by["u/revue"]; skill.Description != "Relit un diff" || !skill.Manual || skill.Tools != "Read Grep" || skill.ReadOnly || skill.Scope != "user" {
		t.Fatalf("revue : %+v", skill)
	}
	if skill := by["u/sans-desc"]; skill.Description != "Titre du skill" || !strings.Contains(skill.Warn, "description") {
		t.Fatalf("sans description : %+v", skill)
	}
	if skill := by["u/avec-fichiers"]; skill.Files != 2 {
		t.Fatalf("fichiers : %+v", skill)
	}
	if skill := by["p/-p/local"]; skill.Scope != "project" || skill.Project != proj {
		t.Fatalf("projet : %+v", skill)
	}
	if skill := by["x/p@m/du-plugin"]; !skill.ReadOnly || skill.Plugin != "p@m" {
		t.Fatalf("plugin : %+v", skill)
	}

	// lecture, écriture, corbeille
	text, files, err := readSkill("u/avec-fichiers")
	if err != nil || len(files) != 2 || !strings.Contains(text, "description: x") {
		t.Fatal(text, files, err)
	}
	if err := saveSkill("u/revue", "---\nname: revue\ndescription: Nouveau\n---\n\nx\n", false); err != nil {
		t.Fatal(err)
	}
	if ts := trashList(); len(ts) != 1 || ts[0].Key != "k/u/revue" {
		t.Fatalf("corbeille : %+v", ts)
	}
	// restauration depuis la corbeille
	if _, err := trashRestore(trashList()[0].ID); err != nil {
		t.Fatal(err)
	}
	if text, _, _ := readSkill("u/revue"); !strings.Contains(text, "Relit un diff") {
		t.Fatalf("restauration : %s", text)
	}
	// création : nom strict, pas d'écrasement
	if err := saveSkill("u/Mon Skill", "---\ndescription: x\n---\nx\n", true); err == nil {
		t.Fatal("nom invalide accepté")
	}
	if err := saveSkill("u/NouveauMaj", "x\n", true); err == nil {
		t.Fatal("majuscules acceptées pour un nouveau skill")
	}
	if err := saveSkill("u/revue", "x\n", true); err == nil {
		t.Fatal("écrasement accepté")
	}
	if err := saveSkill("u/nouveau", "---\ndescription: neuf\n---\nx\n", true); err != nil {
		t.Fatal(err)
	}
	// refus : plugin, traversée, texte vide
	for _, key := range []string{"x/p@m/du-plugin"} {
		if saveSkill(key, "x\n", false) == nil || removeSkill(key) == nil {
			t.Errorf("écriture dans un plugin acceptée : %s", key)
		}
	}
	for _, key := range []string{"u/..", "u/../x", "u/a/b", "p/../x/y", "x/pas-un-id/y", "z/x", ""} {
		if _, _, err := readSkill(key); err == nil {
			t.Errorf("clé acceptée : %q", key)
		}
	}
	if saveSkill("u/revue", "  \n", false) == nil {
		t.Fatal("texte vide accepté")
	}
	// suppression : un dossier qui contient autre chose que SKILL.md est refusé
	if err := removeSkill("u/avec-fichiers"); err == nil || !strings.Contains(err.Error(), "d'autres fichiers") {
		t.Fatalf("suppression d'un dossier avec fichiers : %v", err)
	}
	if err := removeSkill("u/nouveau"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "skills", "nouveau")); !os.IsNotExist(err) {
		t.Fatal("dossier resté")
	}
	// copie : dossier entier, depuis un plugin vers soi, de soi vers un projet ; pas d'écrasement
	if dst, err := copySkill("x/p@m/du-plugin", "u"); err != nil || dst != "u/du-plugin" {
		t.Fatal(dst, err)
	}
	if _, err := copySkill("x/p@m/du-plugin", "u"); err == nil {
		t.Fatal("copie sur un existant acceptée")
	}
	if dst, err := copySkill("u/avec-fichiers", "p/-p"); err != nil || dst != "p/-p/avec-fichiers" {
		t.Fatal(dst, err)
	}
	if data, err := os.ReadFile(filepath.Join(proj, ".claude", "skills", "avec-fichiers", "scripts", "go.sh")); err != nil || string(data) != "echo\n" {
		t.Fatalf("fichier d'appui non copié : %q %v", data, err)
	}
	if st, err := os.Stat(filepath.Join(proj, ".claude", "skills", "avec-fichiers", "scripts", "go.sh")); err != nil || st.Mode()&0o100 == 0 {
		t.Fatalf("script copié sans bit exécutable : %v %v", st, err)
	}
	for _, to := range []string{"x", "p/..", "u/y", ""} {
		if _, err := copySkill("u/revue", to); err == nil {
			t.Errorf("destination acceptée : %q", to)
		}
	}
}

func TestBrouillonSkill(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "skills", "a", "SKILL.md"), "---\nname: a\ndescription: d\n---\n\ncorps\n", old)
	var system, prompt string
	old := memDraft
	defer func() { memDraft = old }()
	memDraft = func(model, text, path string) (string, float64, error) {
		system, prompt = text, path
		return "texte condensé sans en-tête\n", 0.001, nil
	}
	out, _, err := skillDraft("haiku", "condense", "u/a", nil)
	if err != nil || !strings.HasPrefix(out, "---\nname: a\ndescription: d\n---\n") {
		t.Fatalf("en-tête non conservé : %q %v", out, err)
	}
	if system != skillSystem() || !strings.Contains(prompt, "corps") || !strings.Contains(prompt, "condense") {
		t.Fatal("prompt :", prompt)
	}
	if _, _, err := skillDraft("modele-inconnu", "x", "", nil); err == nil {
		t.Fatal("modèle inconnu accepté")
	}
	if _, _, err := skillDraft("haiku", " ", "", nil); err == nil {
		t.Fatal("consigne vide acceptée")
	}
}

func TestOrigineDesSkills(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := t.TempDir()
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "skills", "maison", "SKILL.md"), "---\ndescription: x\n---\nx\n", old)
	put(t, filepath.Join(cfg, "skills", "avec-page", "SKILL.md"), "---\ndescription: x\nhomepage: https://github.com/a/b\n---\nx\n", old)
	put(t, filepath.Join(cfg, "skills", "npx", "SKILL.md"), "---\ndescription: x\n---\nx\n", old)
	put(t, filepath.Join(home, ".agents", ".skill-lock.json"), `{"skills":{"npx":{"source":"o/r","sourceType":"github"}}}`, old)
	put(t, filepath.Join(proj, ".claude", "skills", "verrou", "SKILL.md"), "---\ndescription: x\n---\nx\n", old)
	put(t, filepath.Join(proj, "skills-lock.json"), `{"skills":{"verrou":{"source":"x/y","sourceType":"github"}}}`, old)
	bucket := "aaaaaaaa-0000-0000-0000-000000000000_bbbbbbbb-0000-0000-0000-000000000000"
	put(t, filepath.Join(cfg, "skills", "synced", bucket, "docs", "SKILL.md"), "---\ndescription: x\n---\nx\n", old)
	put(t, filepath.Join(cfg, "skills", "synced", bucket, "manifest.json"), "{}", old)

	by := map[string]Skill{}
	for _, skill := range listSkills() {
		by[skill.Key] = skill
	}
	for key, want := range map[string]string{"u/maison": "custom", "u/avec-page": "install", "u/npx": "install", "p/-p/verrou": "install", "s/" + bucket + "/docs": "store"} {
		if by[key].Origin != want {
			t.Errorf("%s : origine %q, attendu %q", key, by[key].Origin, want)
		}
	}
	if by["u/npx"].Source != "github : o/r" || by["u/avec-page"].Source != "https://github.com/a/b" {
		t.Errorf("sources : %+v %+v", by["u/npx"], by["u/avec-page"])
	}
	if skill := by["s/"+bucket+"/docs"]; !skill.ReadOnly || skill.Scope != "store" {
		t.Errorf("store : %+v", skill)
	}
	if len(by) != 5 || saveSkill("s/"+bucket+"/docs", "x\n", false) == nil {
		t.Errorf("le store doit être en lecture seule (%d skills)", len(by))
	}
}

func TestSkillsSymlink(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	ext := t.TempDir()
	put(t, filepath.Join(ext, "lie", "SKILL.md"), "---\ndescription: via lien\n---\nx\n", old)
	put(t, filepath.Join(ext, "lie", "refs", "a.md"), "a\n", old)
	if err := os.MkdirAll(filepath.Join(cfg, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ext, "lie"), filepath.Join(cfg, "skills", "lie")); err != nil {
		t.Fatal(err)
	}
	for _, skill := range listSkills() {
		if skill.Key == "u/lie" {
			if skill.Files != 2 {
				t.Fatalf("fichiers : %d", skill.Files)
			}
			return
		}
	}
	t.Fatal("skill lié absent de la liste")
}

func TestSkillsSymlinkProtected(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	ext := t.TempDir()
	put(t, filepath.Join(ext, "lie", "SKILL.md"), "---\ndescription: via lien\n---\nx\n", old)
	os.MkdirAll(filepath.Join(cfg, "skills"), 0o755)
	if err := os.Symlink(filepath.Join(ext, "lie"), filepath.Join(cfg, "skills", "lie")); err != nil {
		t.Fatal(err)
	}
	if removeSkill("u/lie") == nil || saveSkill("u/lie", "y", false) == nil {
		t.Fatal("un skill lié doit être refusé en suppression et en écriture")
	}
	if _, err := os.Stat(filepath.Join(ext, "lie", "SKILL.md")); err != nil {
		t.Fatal("le SKILL.md cible a été touché")
	}
}

func TestReadLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "l.json")
	put(t, path, `{"version":3,"skills":{"find-skills":{"source":"vercel-labs/skills","sourceType":"github","sourceUrl":"https://github.com/vercel-labs/skills.git","skillPath":"skills/find-skills/SKILL.md","skillFolderHash":"76a9","installedAt":"a","updatedAt":"b"}}}`, old)
	lockEntry := readLock(path)["find-skills"]
	want := LockEntry{"vercel-labs/skills", "github", "https://github.com/vercel-labs/skills.git", "skills/find-skills/SKILL.md", "76a9", "a", "b"}
	if lockEntry != want {
		t.Errorf("entrée : %+v", lockEntry)
	}
	for _, content := range []string{"{}", "not json", ""} {
		put(t, path, content, old)
		if len(readLock(path)) != 0 {
			t.Errorf("%q : attendu vide", content)
		}
	}
	if readLock(filepath.Join(dir, "absent.json")) != nil {
		t.Error("fichier absent : attendu nil")
	}
}

func TestSkillLockEnLien(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	home := t.TempDir()
	t.Setenv("HOME", home)
	body := "---\ndescription: x\n---\nx\n"
	put(t, filepath.Join(home, ".agents", "skills", "lie", "SKILL.md"), body, old)
	put(t, filepath.Join(cfg, "skills", "copie", "SKILL.md"), body, old)
	put(t, filepath.Join(cfg, "skills", "maison", "SKILL.md"), body, old)
	if err := os.Symlink(filepath.Join(home, ".agents", "skills", "lie"), filepath.Join(cfg, "skills", "lie")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(home, ".agents", ".skill-lock.json"), `{"skills":{"lie":{"source":"o/r","sourceType":"github","skillFolderHash":"h"},"copie":{"source":"o/r","sourceType":"github"}}}`, old)
	by := map[string]Skill{}
	for _, skill := range listSkills() {
		by[skill.Key] = skill
	}
	if skillLock := by["u/lie"].Lock; skillLock == nil || !skillLock.Link || !skillLock.Managed || !skillLock.Global || skillLock.Hash != "h" || by["u/lie"].Origin != "install" {
		t.Errorf("lien : %+v", by["u/lie"])
	}
	if skillLock := by["u/copie"].Lock; skillLock == nil || skillLock.Link || skillLock.Managed {
		t.Errorf("copie : %+v", by["u/copie"])
	}
	if by["u/maison"].Lock != nil || by["u/maison"].Origin != "custom" {
		t.Errorf("maison : %+v", by["u/maison"])
	}
}

func TestRemoveSkillDossierAvecLien(t *testing.T) {
	cfg := t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	dir := filepath.Join(cfg, "skills", "lie")
	os.MkdirAll(filepath.Join(dir, "vide"), 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: lie\ndescription: d\n---\n"), 0o644)
	if err := removeSkill("u/lie"); err == nil {
		t.Fatal("dossier avec un sous-dossier supprimé")
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal("SKILL.md supprimé alors que le dossier reste")
	}
}
