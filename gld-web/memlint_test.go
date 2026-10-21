package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMemLint(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	put(t, filepath.Join(cfg, "CLAUDE.md"), "global", old)
	put(t, filepath.Join(cfg, "rules", "a.md"), "Toujours répondre en français.", old)
	put(t, filepath.Join(cfg, "rules", "b.md"), "Répondre en français, toujours.", old)
	put(t, filepath.Join(cfg, "rules", "c.md.off"), "désactivée", old)
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md"), "- [x](x.md)\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "x.md"), "entrée x", old)

	if k, err := memLintFiles("global"); err != nil || !slices.Equal(k, []string{"g/CLAUDE.md", "g/rules/a.md", "g/rules/b.md"}) {
		t.Fatal("portée globale :", k, err) // la règle désactivée n'est pas analysée
	}
	if k, err := memLintFiles("-p"); err != nil || !slices.Equal(k, []string{"m/-p/x.md"}) {
		t.Fatal("portée projet :", k, err) // l'index n'est pas analysé
	}
	if _, err := memLintFiles("../x"); err == nil {
		t.Fatal("projet invalide accepté")
	}

	var prompt string
	old := memDraft
	defer func() { memDraft = old }()
	memDraft = func(model, system, p string) (string, float64, error) {
		prompt = p
		return `[{"kind":"doublon","keys":["g/rules/a.md","g/rules/b.md"],"why":"même consigne"},
			{"kind":"doublon","keys":["g/rules/a.md","m/-p/x.md"],"why":"clé hors portée : écartée"},
			{"kind":"autre","keys":["g/rules/a.md"],"why":"genre inconnu : écarté"},
			{"kind":"périmé","keys":[],"why":"sans clé : écarté"}]`, 0.001, nil
	}
	fs, cost, err := memLint("global")
	if err != nil || cost != 0.001 || len(fs) != 1 || fs[0].Why != "même consigne" {
		t.Fatalf("%+v %v %v", fs, cost, err)
	}
	if !strings.Contains(prompt, "=== g/rules/a.md ===") || strings.Contains(prompt, "désactivée") {
		t.Fatal("prompt :", prompt)
	}
	memDraft = func(string, string, string) (string, float64, error) { return "pas du json", 0, nil }
	if _, _, err := memLint("global"); err == nil {
		t.Fatal("réponse illisible acceptée")
	}
}
