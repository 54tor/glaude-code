package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallBuiltinRules(t *testing.T) {
	old := root
	root = filepath.Join(t.TempDir(), "projects")
	t.Cleanup(func() { root = old })
	p := filepath.Join(cfgDir(), "rules", "gld-askuser.md")

	done, err := installBuiltinRules()
	if err != nil || len(done) != 3 {
		t.Fatalf("première installation : %v %v", done, err)
	}
	if err := os.WriteFile(p, []byte("retouché"), 0o644); err != nil {
		t.Fatal(err)
	}
	if done, err = installBuiltinRules(); err != nil || len(done) != 1 || done[0] != "gld-askuser.md" {
		t.Fatalf("réécriture de la règle retouchée : %v %v", done, err)
	}
	if done, _ = installBuiltinRules(); len(done) != 0 {
		t.Fatalf("rien à réécrire : %v", done)
	}

	own := filepath.Join(cfgDir(), "rules", "gld-vieille.md")
	mine := filepath.Join(cfgDir(), "rules", "gld-reprise.md")
	os.WriteFile(own, []byte(builtinMark+" x -->\n"), 0o644)
	os.WriteFile(mine, []byte("à moi\n"), 0o644)
	builtinRetired = []string{"gld-vieille.md", "gld-reprise.md"}
	t.Cleanup(func() { builtinRetired = nil })
	if done, _ = installBuiltinRules(); len(done) != 1 || done[0] != "-gld-vieille.md" {
		t.Fatalf("retrait : %v", done)
	}
	if _, err := os.Stat(mine); err != nil {
		t.Fatal("un fichier repris par l'utilisateur ne doit pas être supprimé")
	}
}
