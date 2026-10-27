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
	for _, s := range listSkills() {
		by[s.Key] = s
	}
	if len(by) != 5 {
		t.Fatalf("%d skills : %v", len(by), by)
	}
	if s := by["u/revue"]; s.Description != "Relit un diff" || !s.Manual || s.Tools != "Read Grep" || s.ReadOnly || s.Scope != "user" {
		t.Fatalf("revue : %+v", s)
	}
	if s := by["u/sans-desc"]; s.Description != "Titre du skill" || !strings.Contains(s.Warn, "description") {
		t.Fatalf("sans description : %+v", s)
	}
	if s := by["u/avec-fichiers"]; s.Files != 2 {
		t.Fatalf("fichiers : %+v", s)
	}
	if s := by["p/-p/local"]; s.Scope != "project" || s.Project != proj {
		t.Fatalf("projet : %+v", s)
	}
	if s := by["x/p@m/du-plugin"]; !s.ReadOnly || s.Plugin != "p@m" {
		t.Fatalf("plugin : %+v", s)
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
	if s, _, _ := readSkill("u/revue"); !strings.Contains(s, "Relit un diff") {
		t.Fatalf("restauration : %s", s)
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
	for _, k := range []string{"x/p@m/du-plugin"} {
		if saveSkill(k, "x\n", false) == nil || removeSkill(k) == nil {
			t.Errorf("écriture dans un plugin acceptée : %s", k)
		}
	}
	for _, k := range []string{"u/..", "u/../x", "u/a/b", "p/../x/y", "x/pas-un-id/y", "z/x", ""} {
		if _, _, err := readSkill(k); err == nil {
			t.Errorf("clé acceptée : %q", k)
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
	if b, err := os.ReadFile(filepath.Join(proj, ".claude", "skills", "avec-fichiers", "scripts", "go.sh")); err != nil || string(b) != "echo\n" {
		t.Fatalf("fichier d'appui non copié : %q %v", b, err)
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
	memDraft = func(model, s, p string) (string, float64, error) {
		system, prompt = s, p
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
	for _, s := range listSkills() {
		by[s.Key] = s
	}
	for k, want := range map[string]string{"u/maison": "custom", "u/avec-page": "install", "u/npx": "install", "p/-p/verrou": "install", "s/" + bucket + "/docs": "store"} {
		if by[k].Origin != want {
			t.Errorf("%s : origine %q, attendu %q", k, by[k].Origin, want)
		}
	}
	if by["u/npx"].Source != "github : o/r" || by["u/avec-page"].Source != "https://github.com/a/b" {
		t.Errorf("sources : %+v %+v", by["u/npx"], by["u/avec-page"])
	}
	if s := by["s/"+bucket+"/docs"]; !s.ReadOnly || s.Scope != "store" {
		t.Errorf("store : %+v", s)
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
	for _, s := range listSkills() {
		if s.Key == "u/lie" {
			if s.Files != 2 {
				t.Fatalf("fichiers : %d", s.Files)
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
