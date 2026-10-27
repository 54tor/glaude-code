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
