package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEcritureConfig(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	p := filepath.Join(cfg, "settings.json")
	orig := "{\n  \"model\": \"opus\",\n  \"hooks\": {\n    \"Stop\": []\n  },\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(ls:*)\"\n    ]\n  },\n  \"theme\": \"dark\"\n}\n"
	put(t, p, orig, old)

	do := func(e CfgEdit) (string, error) {
		e.File = "u"
		_, after, err := configEdit(e)
		return after, err
	}
	// aperçu : rien n'est écrit
	after, err := do(CfgEdit{Op: "set", Path: []string{"model"}, Value: "sonnet", Dry: true})
	if err != nil || !strings.Contains(after, `"model": "sonnet"`) {
		t.Fatal(after, err)
	}
	if b, _ := os.ReadFile(p); string(b) != orig {
		t.Fatal("l'aperçu a écrit")
	}
	// écriture : seule la ligne concernée change, le reste (ordre, hooks) est intact
	after, err = do(CfgEdit{Op: "set", Path: []string{"model"}, Value: "sonnet"})
	if err != nil || after != strings.Replace(orig, `"opus"`, `"sonnet"`, 1) {
		t.Fatalf("%q %v", after, err)
	}
	if b, _ := os.ReadFile(p); string(b) != after {
		t.Fatal("fichier non écrit")
	}
	if bs := listBackups(); len(bs) != 1 || bs[0].File != "u" {
		t.Fatalf("sauvegardes : %+v", bs)
	}
	// permissions : ajout, doublon refusé, retrait ; une liste vidée disparaît avec son conteneur
	if after, err = do(CfgEdit{Op: "add", Path: []string{"permissions", "allow"}, Value: "Bash(git status:*)"}); err != nil || !strings.Contains(after, "Bash(git status:*)") {
		t.Fatal(after, err)
	}
	if _, err = do(CfgEdit{Op: "add", Path: []string{"permissions", "allow"}, Value: "Bash(ls:*)"}); err == nil {
		t.Fatal("doublon accepté")
	}
	do(CfgEdit{Op: "remove", Path: []string{"permissions", "allow"}, Value: "Bash(git status:*)"})
	if after, err = do(CfgEdit{Op: "remove", Path: []string{"permissions", "allow"}, Value: "Bash(ls:*)"}); err != nil || strings.Contains(after, "permissions") {
		t.Fatal(after, err)
	}
	// variables d'environnement et plugins : créés puis retirés, conteneur compris
	do(CfgEdit{Op: "set", Path: []string{"env", "MA_VAR"}, Value: "1"})
	do(CfgEdit{Op: "set", Path: []string{"enabledPlugins", "a@b"}, Value: true})
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"MA_VAR": "1"`) || !strings.Contains(string(b), `"a@b": true`) {
		t.Fatal(string(b))
	}
	do(CfgEdit{Op: "delete", Path: []string{"env", "MA_VAR"}})
	if after, _ = do(CfgEdit{Op: "delete", Path: []string{"enabledPlugins", "a@b"}}); strings.Contains(after, "env") || strings.Contains(after, "enabledPlugins") {
		t.Fatal(after)
	}
	// nombres et booléens typés
	if _, err = do(CfgEdit{Op: "set", Path: []string{"cleanupPeriodDays"}, Value: float64(30)}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"cleanupPeriodDays": 30`) {
		t.Fatal(string(b))
	}
	// refus : types faux, chemins hors liste, projet inconnu, nom de variable invalide
	for _, e := range []CfgEdit{
		{Op: "set", Path: []string{"autoCompactEnabled"}, Value: "oui"},
		{Op: "set", Path: []string{"cleanupPeriodDays"}, Value: float64(-1)},
		{Op: "set", Path: []string{"hooks", "Stop"}, Value: "x"},
		{Op: "set", Path: []string{"mcpServers", "x"}, Value: "x"},
		{Op: "set", Path: []string{"apiKeyHelper"}, Value: "x"},
		{Op: "set", Path: []string{"env", "A B"}, Value: "x"},
		{Op: "set", Path: []string{"env", "A"}, Value: "ligne1\nligne2"},
		{Op: "add", Path: []string{"permissions", "allow"}, Value: ""},
		{Op: "set", Path: []string{"enabledPlugins", "sans-arobase"}, Value: true},
	} {
		if _, err := do(e); err == nil {
			t.Errorf("modification acceptée : %+v", e)
		}
	}
	if _, _, err := configEdit(CfgEdit{File: "p/-inconnu", Op: "set", Path: []string{"model"}, Value: "x"}); err == nil {
		t.Fatal("projet inconnu accepté")
	}
	// restauration : l'état d'avant revient, et l'état remplacé est sauvegardé
	first := listBackups()[len(listBackups())-1]
	if err := restoreBackup(first.Name); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != orig {
		t.Fatalf("restauration : %q", b)
	}
	for _, bad := range []string{"../x.json", "20200101-000000.000000__u.json.sh", "n'importe quoi"} {
		if restoreBackup(bad) == nil {
			t.Errorf("sauvegarde invalide acceptée : %q", bad)
		}
	}
}

// Un fichier dont la mise en forme n'est pas celle de Claude Code n'est pas réécrit.
func TestEcritureRefuseFormatParticulier(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "settings.json"), `{"model":"opus"}`+"\n", old)
	if _, _, err := configEdit(CfgEdit{File: "u", Op: "set", Path: []string{"model"}, Value: "x"}); err == nil {
		t.Fatal("fichier compact réécrit")
	}
}

// Un fichier absent est créé, avec ses dossiers.
func TestEcritureCreeLeFichier(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	if _, after, err := configEdit(CfgEdit{File: "l/-p", Op: "set", Path: []string{"theme"}, Value: "dark"}); err != nil || after != "{\n  \"theme\": \"dark\"\n}\n" {
		t.Fatalf("%q %v", after, err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "settings.local.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSuggestionsPermissions(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	line := func(cmd string) string {
		return `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"` + cmd + `"}}]}}` + "\n"
	}
	var b strings.Builder
	for i := 0; i < 4; i++ {
		b.WriteString(line("git status") + line("ls -la /tmp") + line("rm -rf x") + line("go test ./...") + line("cat a | grep b"))
	}
	b.WriteString(line("git status") + line("make build"))
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), b.String(), old)
	got := map[string]PermSuggest{}
	for _, s := range permSuggestions([]string{"Bash(go test:*)"}) {
		got[s.Rule] = s
	}
	if s := got["Bash(git status:*)"]; s.Count != 5 || !s.ReadOnly {
		t.Fatalf("git status : %+v", got)
	}
	if s := got["Bash(ls:*)"]; s.Count != 4 || !s.ReadOnly {
		t.Fatalf("ls : %+v", got)
	}
	for _, no := range []string{"Bash(rm:*)", "Bash(go test:*)", "Bash(cat:*)", "Bash(make build:*)"} {
		if _, ok := got[no]; ok {
			t.Errorf("règle proposée à tort : %s", no)
		}
	}
}
