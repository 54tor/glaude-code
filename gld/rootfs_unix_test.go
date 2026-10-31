//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeRunAs active le mode GLD_RUN_AS pour la durée du test, sans compte réel : seules les lectures confinées en dépendent ici.
func fakeRunAs(t *testing.T) {
	old := runAsAcct
	runAsAcct = &runAsAccount{}
	t.Cleanup(func() { runAsAcct = old })
}

func TestLecturesConfineesSousRunAs(t *testing.T) {
	cfg, out, proj := t.TempDir(), t.TempDir(), t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	use(cfg)
	secret := filepath.Join(out, "secret")
	os.WriteFile(secret, []byte("de root"), 0o600)
	os.Symlink(secret, filepath.Join(cfg, "CLAUDE.md"))
	os.Symlink(out, filepath.Join(cfg, "rules"))
	os.Symlink(secret, filepath.Join(proj, "CLAUDE.md"))

	// Hors GLD_RUN_AS : un lien vers des dotfiles reste lisible.
	if data, err := readTrusted(filepath.Join(cfg, "CLAUDE.md")); err != nil || string(data) != "de root" {
		t.Fatalf("lecture ordinaire d'un lien : %q %v", data, err)
	}
	fakeRunAs(t)
	for _, path := range []string{filepath.Join(cfg, "CLAUDE.md"), filepath.Join(cfg, "rules", "secret"), filepath.Join(proj, "CLAUDE.md")} {
		if data, err := readTrusted(path); err == nil {
			t.Errorf("%s lu à travers un lien : %q", path, data)
		}
		if file, err := openTrusted(path); err == nil {
			file.Close()
			t.Errorf("%s ouvert à travers un lien", path)
		}
	}
	if _, err := readDirTrusted(filepath.Join(cfg, "rules")); err == nil {
		t.Error("dossier lié listé")
	}
	os.WriteFile(filepath.Join(cfg, "ok.md"), []byte("ok"), 0o600)
	if data, err := readTrusted(filepath.Join(cfg, "ok.md")); err != nil || string(data) != "ok" {
		t.Fatalf("fichier ordinaire : %q %v", data, err)
	}
	if ents, err := readDirTrusted(cfg); err != nil || len(ents) == 0 {
		t.Fatalf("liste de la config : %v %v", ents, err)
	}
}

func TestGardeTrashRestoreNeSuitPasLesLiens(t *testing.T) {
	home, out := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(out, "x"), 0o700)
	os.WriteFile(filepath.Join(out, "x", "meta.json"), []byte(`{"id":"x"}`), 0o600)
	os.Symlink(out, filepath.Join(home, "gld-web-trash"))
	fakeRunAs(t)
	if _, err := readTrustedIn(home, filepath.Join(home, "gld-web-trash", "x", "meta.json")); err == nil {
		t.Fatal("meta.json lu hors du home")
	}
}
