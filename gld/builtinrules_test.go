package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	path := filepath.Join(builtinDir(), "gld-askuser.md")
	if err := os.WriteFile(path, []byte("retouché"), 0o644); err != nil {
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
	t.Cleanup(func() { root = old; promptFlagOnce = sync.Once{}; promptFlagName = "" })
	oldHelp := claudeHelp
	t.Cleanup(func() { claudeHelp = oldHelp })
	claudeHelp = func() string { return "  --append-system-prompt-file <f>\n  --append-system-prompt <p>" }
	if builtinPromptArgs() != nil {
		t.Fatal("rien tant que les règles ne sont pas écrites")
	}
	if _, err := installBuiltinRules(); err != nil {
		t.Fatal(err)
	}
	values := builtinPromptArgs()
	if len(values) != 2 || values[0] != "--append-system-prompt-file" || values[1] != builtinPromptFile() {
		t.Fatalf("args = %v", values)
	}
	data, _ := os.ReadFile(builtinPromptFile())
	if strings.Contains(string(data), "source: gld") || strings.Contains(string(data), builtinMark) || !strings.Contains(string(data), "# Output format") {
		t.Fatalf("prompt mal assemblé : %.200s", data)
	}
	b2, _ := os.ReadFile(builtinPromptFile())
	for _, record := range []struct{ help, flag string }{{"--append-system-prompt <p>", "--append-system-prompt"}, {"--model", ""}} {
		promptFlagOnce, promptFlagName = sync.Once{}, ""
		claudeHelp = func() string { return record.help }
		got := builtinPromptArgs()
		switch {
		case record.flag == "" && got != nil:
			t.Fatalf("option inconnue : rien attendu, reçu %v", got)
		case record.flag != "" && (len(got) != 2 || got[0] != record.flag || got[1] != string(b2)):
			t.Fatalf("repli : %v", got)
		}
	}
	promptFlagOnce, promptFlagName = sync.Once{}, ""
	claudeHelp = func() string { return "--append-system-prompt-file" }
	if err := setBuiltinOn(false); err != nil || builtinPromptArgs() != nil || builtinOn() {
		t.Fatal("coupées : aucune option attendue")
	}
	if err := setBuiltinOn(true); err != nil || builtinPromptArgs() == nil {
		t.Fatal("réactivées : option attendue")
	}
}

func TestBuiltinRulesMarkedGld(t *testing.T) {
	ents, _ := builtinRules.ReadDir("rules")
	for _, dirEntry := range ents {
		data, _ := builtinRules.ReadFile("rules/" + dirEntry.Name())
		lookup, err := parseMeta(string(data))
		if err != nil || lookup["source"] != "gld" {
			t.Fatalf("%s : source: gld absent du premier niveau (%v)", dirEntry.Name(), err)
		}
	}
}
