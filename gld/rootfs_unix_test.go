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

// Sous GLD_RUN_AS, un dossier de cfgDir() remplacé par un lien sortant (rules, skills/<nom>) ne doit être ni lu, ni vidé :
// os.OpenRoot suivrait le lien, openRootTrusted ouvre depuis cfgDir() et le refuse. Hors GLD_RUN_AS, un skill lié (npx) reste lisible.
func TestDossiersLiesDeCfgDirRefusesSousRunAs(t *testing.T) {
	cfg, dossierExterieur := t.TempDir(), t.TempDir()
	previousRoot := root
	t.Cleanup(func() { root = previousRoot })
	use(cfg)
	regleExterieure := filepath.Join(dossierExterieur, "a.md")
	os.WriteFile(regleExterieure, []byte("règle de root"), 0o600)
	os.WriteFile(filepath.Join(dossierExterieur, "SKILL.md"), []byte("---\ndescription: de root\n---\n"), 0o600)
	os.Symlink(dossierExterieur, filepath.Join(cfg, "rules"))
	os.MkdirAll(filepath.Join(cfg, "skills"), 0o700)
	os.Symlink(dossierExterieur, filepath.Join(cfg, "skills", "lie"))

	if _, _, readErr := readSkill("u/lie"); readErr != nil {
		t.Fatalf("hors GLD_RUN_AS, un skill lié doit rester lisible : %v", readErr)
	}
	fakeRunAs(t)
	if contenu, readErr := memRead("g/rules/a.md"); readErr == nil {
		t.Errorf("règle lue à travers un lien : %q", contenu)
	}
	if contenu, _, readErr := readSkill("u/lie"); readErr == nil {
		t.Errorf("SKILL.md lu à travers un lien : %q", contenu)
	}
	if deleteErr := memDelete("g/rules/a.md", false); deleteErr == nil {
		t.Error("règle supprimée à travers un lien")
	}
	if _, statErr := os.Stat(regleExterieure); statErr != nil {
		t.Fatalf("le fichier visé par le lien a disparu : %v", statErr)
	}
}
