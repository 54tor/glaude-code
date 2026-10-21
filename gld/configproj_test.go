package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigParProjet(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	vide := t.TempDir()
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-v", "22222222-2222-2222-2222-222222222222.jsonl"), `{"cwd":"`+vide+`"}`+"\n", old)
	put(t, filepath.Join(proj, ".claude", "settings.json"), "{\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(make:*)\"\n    ]\n  },\n  \"env\": {\n    \"API_KEY\": \"secret\"\n  }\n}\n", old)
	put(t, filepath.Join(proj, ".claude", "settings.local.json"), "{\n  \"permissions\": {\n    \"deny\": [\n      \"Bash(mv:*)\"\n    ]\n  },\n  \"hooks\": {\n    \"Stop\": [{\"hooks\": [{\"type\": \"command\", \"command\": \"echo fin\"}]}]\n  }\n}\n", old)
	put(t, filepath.Join(proj, ".claude", "rules", "api.md"), "---\npaths:\n  - \"src/**/*.go\"\n---\nr\n", old)
	put(t, filepath.Join(proj, ".claude", "rules", "tous.md"), "toujours\n", old)
	put(t, filepath.Join(proj, ".claude", "skills", "deploy", "SKILL.md"), "---\ndescription: d\n---\nx\n", old)
	put(t, filepath.Join(proj, ".mcp.json"), `{"mcpServers":{"mon-serveur":{"command":"x","env":{"TOKEN":"s"}}}}`, old)
	put(t, filepath.Join(proj, "README.md"), "doc\n", old)

	var p, v ProjectCfg
	for _, pc := range projectConfigs() {
		switch pc.Dir {
		case "-p":
			p = pc
		case "-v":
			v = pc
		}
	}
	if len(p.Files) != 2 || p.Perms["allow"][0].Rule != "Bash(make:*)" || p.Perms["allow"][0].Scope != "p" || p.Perms["deny"][0].Scope != "l" {
		t.Fatalf("réglages et règles : %+v", p)
	}
	if len(p.Hooks) != 1 || p.Hooks[0].Event != "Stop" || len(p.MCP) != 1 || p.MCP[0].Name != "mon-serveur" || len(p.Skills) != 1 || p.Skills[0] != "deploy" {
		t.Fatalf("hooks, MCP, skills : %+v", p)
	}
	if len(p.Rules) != 2 || p.Rules[0].Name != "api.md" || len(p.Rules[0].Paths) != 1 || len(p.Rules[1].Paths) != 0 {
		t.Fatalf("règles du projet : %+v", p.Rules)
	}
	if !strings.Contains(strings.Join(p.Docs, " "), "README.md") || p.Specific == 0 {
		t.Fatalf("docs : %+v", p)
	}
	for _, f := range p.Files {
		for _, r := range f.Rows {
			if r.Value == "secret" {
				t.Fatal("secret non masqué dans la vue par projet")
			}
		}
	}
	if v.Specific != 0 || v.Dir != "-v" {
		t.Fatalf("projet sans réglage propre : %+v", v)
	}
	if projectConfigs()[0].Dir != "-p" { // ceux qui ont du spécifique d'abord
		t.Fatal("ordre")
	}
}

func TestReglesDeProjetDansLaMemoire(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	a, b := t.TempDir(), t.TempDir()
	put(t, filepath.Join(cfg, "projects", "-a", id1+".jsonl"), `{"cwd":"`+a+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-b", "22222222-2222-2222-2222-222222222222.jsonl"), `{"cwd":"`+b+`"}`+"\n", old)
	put(t, filepath.Join(a, ".claude", "rules", "go.md"), "---\npaths:\n  - \"**/*.go\"\n---\nRègle Go\n", old)

	_, projects, _ := memList()
	var f MemFile
	for _, p := range projects {
		for _, x := range p.Files {
			if x.Key == "r/-a/go.md" {
				f = x
			}
		}
	}
	if f.Name != "rules/go.md" || len(f.Paths) != 1 || f.Always {
		t.Fatalf("règle de projet non listée ou mal annotée : %+v", f)
	}
	// vers les règles globales (copie), puis déplacement vers un autre projet
	if dst, err := memTransfer("r/-a/go.md", "global", true); err != nil || dst != "g/rules/go.md" {
		t.Fatal(dst, err)
	}
	if _, err := memTransfer("r/-a/go.md", "global", true); err == nil {
		t.Fatal("écrasement accepté")
	}
	if dst, err := memTransfer("g/rules/go.md", "r:-b", true); err != nil || dst != "r/-b/go.md" {
		t.Fatal(dst, err)
	}
	if _, err := os.Stat(filepath.Join(b, ".claude", "rules", "go.md")); err != nil {
		t.Fatal("règle absente du projet de destination")
	}
	if dst, err := memTransfer("r/-a/go.md", "r:-b", false); err == nil {
		t.Fatalf("déplacement sur un doublon accepté : %s", dst)
	}
	if _, err := memTransfer("r/-a/go.md", "-b", false); err == nil {
		t.Fatal("règle de projet envoyée dans une mémoire")
	}
	// modification et suppression : version remplacée à la corbeille
	if err := memSave("r/-b/go.md", "Règle modifiée\n"); err != nil {
		t.Fatal(err)
	}
	if err := memRemove("r/-b/go.md"); err != nil || len(trashList()) != 2 {
		t.Fatalf("suppression : %v, corbeille %d", err, len(trashList()))
	}
	if _, err := os.Stat(filepath.Join(b, ".claude", "rules", "go.md")); !os.IsNotExist(err) {
		t.Fatal("fichier resté")
	}
	// clés invalides
	for _, k := range []string{"r/../go.md", "r/-a/../x.md", "r/-a/.cache.md", "r/inconnu/x.md"} {
		if _, err := memRead(k); err == nil {
			t.Errorf("clé acceptée : %s", k)
		}
	}
}
