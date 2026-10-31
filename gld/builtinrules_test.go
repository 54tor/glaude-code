package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallBuiltinRules(t *testing.T) {
	old := root
	root = filepath.Join(t.TempDir(), "projects")
	t.Cleanup(func() { root = old })
	rules := filepath.Join(cfgDir(), "rules")
	os.MkdirAll(rules, 0o755)
	legacy := filepath.Join(rules, "gld-askuser.md")
	mine := filepath.Join(rules, "gld-todo-notes.md")
	os.WriteFile(legacy, []byte(builtinMark+" x -->\n"), 0o644)
	os.WriteFile(mine, []byte("à moi\n"), 0o644)

	done, err := installBuiltinRules()
	if err != nil || len(done) != 5 || done[0] != "-gld-askuser.md" {
		t.Fatalf("première installation : %v %v", done, err)
	}
	if _, err := os.Stat(legacy); err == nil {
		t.Fatal("l'ancienne copie gérée par gld doit disparaître de rules/")
	}
	if _, err := os.Stat(mine); err != nil {
		t.Fatal("un fichier repris par l'utilisateur ne doit pas être supprimé")
	}
	p := filepath.Join(builtinDir(), "gld-askuser.md")
	if err := os.WriteFile(p, []byte("retouché"), 0o644); err != nil {
		t.Fatal(err)
	}
	if done, err = installBuiltinRules(); err != nil || len(done) != 1 || done[0] != "gld-askuser.md" {
		t.Fatalf("réécriture de la règle retouchée : %v %v", done, err)
	}
	if done, _ = installBuiltinRules(); len(done) != 0 {
		t.Fatalf("rien à réécrire : %v", done)
	}
}

func TestBuiltinPromptArgs(t *testing.T) {
	old := root
	root = filepath.Join(t.TempDir(), "projects")
	t.Cleanup(func() { root = old })
	if builtinPromptArgs() != nil {
		t.Fatal("rien tant que les règles ne sont pas écrites")
	}
	if _, err := installBuiltinRules(); err != nil {
		t.Fatal(err)
	}
	a := builtinPromptArgs()
	if len(a) != 2 || a[0] != "--append-system-prompt-file" || a[1] != builtinPromptFile() {
		t.Fatalf("args = %v", a)
	}
	b, _ := os.ReadFile(builtinPromptFile())
	if strings.Contains(string(b), "source: gld") || strings.Contains(string(b), builtinMark) || !strings.Contains(string(b), "# Output format") {
		t.Fatalf("prompt mal assemblé : %.200s", b)
	}
	if err := setBuiltinOn(false); err != nil || builtinPromptArgs() != nil || builtinOn() {
		t.Fatal("coupées : aucune option attendue")
	}
	if err := setBuiltinOn(true); err != nil || builtinPromptArgs() == nil {
		t.Fatal("réactivées : option attendue")
	}
}

func TestBuiltinRulesMarkedGld(t *testing.T) {
	ents, _ := builtinRules.ReadDir("rules")
	for _, e := range ents {
		b, _ := builtinRules.ReadFile("rules/" + e.Name())
		m, err := parseMeta(string(b))
		if err != nil || m["source"] != "gld" {
			t.Fatalf("%s : source: gld absent du premier niveau (%v)", e.Name(), err)
		}
	}
}
