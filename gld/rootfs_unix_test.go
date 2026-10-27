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
	if b, err := readTrusted(filepath.Join(cfg, "CLAUDE.md")); err != nil || string(b) != "de root" {
		t.Fatalf("lecture ordinaire d'un lien : %q %v", b, err)
	}
	fakeRunAs(t)
	for _, p := range []string{filepath.Join(cfg, "CLAUDE.md"), filepath.Join(cfg, "rules", "secret"), filepath.Join(proj, "CLAUDE.md")} {
		if b, err := readTrusted(p); err == nil {
			t.Errorf("%s lu à travers un lien : %q", p, b)
		}
		if f, err := openTrusted(p); err == nil {
			f.Close()
			t.Errorf("%s ouvert à travers un lien", p)
		}
	}
	if _, err := readDirTrusted(filepath.Join(cfg, "rules")); err == nil {
		t.Error("dossier lié listé")
	}
	os.WriteFile(filepath.Join(cfg, "ok.md"), []byte("ok"), 0o600)
	if b, err := readTrusted(filepath.Join(cfg, "ok.md")); err != nil || string(b) != "ok" {
		t.Fatalf("fichier ordinaire : %q %v", b, err)
	}
	if ents, err := readDirTrusted(cfg); err != nil || len(ents) == 0 {
		t.Fatalf("liste de la config : %v %v", ents, err)
	}
}
