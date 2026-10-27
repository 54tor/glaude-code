package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Un lien posé par la cible, en dernier segment ou au milieu du chemin, ne fait jamais écrire ni effacer hors de la base.
func TestConfineRefuseLesLiensSortants(t *testing.T) {
	base, out := t.TempDir(), t.TempDir()
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, filepath.Join(base, "lien"))
	os.Symlink(out, filepath.Join(base, "dossier"))
	checks := map[string]error{
		"WriteFile lien":    rWriteFile(base, filepath.Join(base, "lien"), []byte("x"), 0o600),
		"WriteFile dossier": rWriteFile(base, filepath.Join(base, "dossier", "victime"), []byte("x"), 0o600),
		"RemoveAll dossier": rRemoveAll(base, filepath.Join(base, "dossier", "victime")),
		"Rename dossier":    rRename(base, filepath.Join(base, "dossier", "victime"), filepath.Join(base, "vole")),
		"MkdirAll dossier":  rMkdirAll(base, filepath.Join(base, "dossier", "sous"), 0o700),
		"hors de la base":   rWriteFile(base, victim, []byte("x"), 0o600),
	}
	if f, err := rOpenFile(base, filepath.Join(base, "lien"), os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		f.Close()
		t.Error("OpenFile a suivi le lien")
	}
	for what, err := range checks {
		if err == nil {
			t.Errorf("%s accepté", what)
		}
	}
	if b, _ := os.ReadFile(victim); string(b) != "intact" {
		t.Fatalf("fichier hors de la base modifié : %q", b)
	}
	if _, err := os.Stat(filepath.Join(out, "sous")); err == nil {
		t.Fatal("dossier créé hors de la base")
	}
	if err := rWriteFile(base, filepath.Join(base, "a", "b"), []byte("ok"), 0o600); err == nil {
		t.Fatal("dossier parent absent : WriteFile aurait dû échouer")
	}
	if err := rMkdirAll(base, filepath.Join(base, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := rWriteFile(base, filepath.Join(base, "a", "b"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTrashNeSuitPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, filepath.Join(cfg, "gld-web.log"))
	journal("essai")
	os.Mkdir(filepath.Join(out, "x"), 0o700)
	os.Symlink(out, filepath.Join(cfg, "gld-web-trash"))
	trashPurge("")
	os.RemoveAll(filepath.Join(cfg, "gld-web-trash")) // le lien seul, jamais sa cible
	rRemoveAll(cfg, filepath.Join(cfg, "gld-web-trash", "x"))
	if b, _ := os.ReadFile(victim); string(b) != "intact" {
		t.Fatalf("journal écrit à travers un lien : %q", b)
	}
	if _, err := os.Stat(filepath.Join(out, "x")); err != nil {
		t.Fatal("corbeille vidée à travers un lien")
	}
}

func TestWriteConfigNeSuitPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	os.Symlink(out, filepath.Join(cfg, "gld-web-config-backups"))
	if err := saveBackup("u", []byte("x")); err == nil {
		t.Error("sauvegarde écrite à travers un lien")
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Fatalf("fichiers écrits hors de la config : %v", ents)
	}
	p := filepath.Join(cfg, "settings.json")
	if err := writeConfig(p, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	os.Chmod(p, 0o644)
	writeConfig(p, []byte("{ }\n"))
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o644 {
		t.Fatalf("mode non conservé : %v", st.Mode())
	}
	if m, _ := filepath.Glob(filepath.Join(cfg, ".gld-web-*.tmp")); len(m) != 0 {
		t.Fatalf("temporaires restés : %v", m)
	}
}

func TestFichiersDeDonneesNeSuiventPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	for _, f := range []string{"gld-web-notes.json.tmp", "gld-web-tabs.json.tmp", "gld-web-groups.json.tmp"} {
		os.Symlink(victim, filepath.Join(cfg, f))
	}
	if err := notesSave(map[string]string{"p/x": "y"}); err != nil {
		t.Fatal(err)
	}
	writeTabs(nil)
	os.MkdirAll(filepath.Join(out, "-p"), 0o700)
	os.WriteFile(filepath.Join(out, "-p", id1+".jsonl"), []byte("{}\n"), 0o600)
	os.MkdirAll(filepath.Join(cfg, "projects"), 0o700)
	os.Symlink(filepath.Join(out, "-p"), filepath.Join(cfg, "projects", "-p"))
	if err := setTitle(filepath.Join(cfg, "projects", "-p", id1+".jsonl"), id1, "t"); err == nil {
		t.Error("titre écrit à travers un lien")
	}
	if b, _ := os.ReadFile(victim); string(b) != "intact" {
		t.Fatalf("fichier hors de la config écrasé : %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "-p", id1+".jsonl")); string(b) != "{}\n" {
		t.Fatalf("transcript hors de la config modifié : %q", b)
	}
}

func TestCorbeilleSessionsNeSuitPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("{}\n"), 0o600)
	os.Symlink(out, filepath.Join(cfg, "gld-web-trash")) // la corbeille pointe hors de la config
	if n, _, _ := trashSessions([]item{{"-p", id1}}); n != 0 {
		t.Fatal("session déplacée à travers un lien")
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Fatalf("écrit hors de la config : %v", ents)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("transcript perdu")
	}
	victim := filepath.Join(out, "history")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, filepath.Join(cfg, "history.jsonl"))
	pruneHistory([]string{id1})
	if b, _ := os.ReadFile(victim); string(b) != "intact" {
		t.Fatalf("history réécrit à travers un lien : %q", b)
	}
}

func TestReassignNeSuitPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(dst, 0o755)
	encSrc := nonAlnum.ReplaceAllString(src, "-")
	jl := filepath.Join(cfg, "projects", encSrc, id1+".jsonl")
	os.MkdirAll(filepath.Dir(jl), 0o755)
	os.WriteFile(jl, []byte(`{"cwd":"`+src+`"}`+"\n"), 0o600)
	victim := filepath.Join(out, "victime")
	os.WriteFile(victim, []byte("intact"), 0o600)
	os.Symlink(victim, jl+".tmp")
	if n, err := reassign([]item{{encSrc, id1}}, dst); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "intact" {
		t.Fatalf("réécriture du cwd à travers un lien : %q", b)
	}
}

func TestImagesEtSkillsNeSuiventPasLesLiens(t *testing.T) {
	cfg, out := t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	os.Symlink(out, filepath.Join(cfg, "gld-web-images"))
	png := []byte("\x89PNG\r\n\x1a\n0000")
	if _, err := saveImage(png); err == nil {
		t.Error("image écrite à travers un lien")
	}
	os.Symlink(out, filepath.Join(cfg, "skills"))
	if err := saveSkill("u/neuf", "---\nname: neuf\ndescription: d\n---\n", true); err == nil {
		t.Error("skill créé à travers un lien")
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Fatalf("écrit hors de la config : %v", ents)
	}
}

func TestWalkRootSuitUnLienHorsRunAs(t *testing.T) {
	real, links := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(real, "revue.md"), []byte("---\ndescription: d\n---\n"), 0o644)
	os.Symlink(real, filepath.Join(links, "commands"))
	got := walkCommands(filepath.Join(links, "commands"), "", "", nil)
	if len(got) != 1 || got[0].Path != "revue.md" {
		t.Fatalf("commandes d'un dossier lié : %+v", got)
	}
}
