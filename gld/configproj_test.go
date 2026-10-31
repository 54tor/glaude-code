package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Le dossier de configuration lui-même (cwd d'une fusion de mémoire globale) n'est pas un projet à configurer.
func TestConfigSansDossierDeConfiguration(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "projects", "-c", id1+".jsonl"), `{"cwd":"`+cfg+`"}`+"\n", old)
	if len(memTargets()) != 1 {
		t.Fatal("le projet de test n'existe pas")
	}
	if len(liveProjects()) != 0 || len(projectConfigs()) != 0 {
		t.Fatal("~/.claude listé comme projet de configuration")
	}
}

func TestConfigParProjet(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	vide := t.TempDir() // créé d'abord : premier par ordre alphabétique, il doit pourtant passer après le projet spécifique
	proj := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	put(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"perso":{"command":"u"}},"projects":{"`+proj+`":{"mcpServers":{"local-srv":{"command":"l"}}}}}`, old)
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-v", "22222222-2222-2222-2222-222222222222.jsonl"), `{"cwd":"`+vide+`"}`+"\n", old)
	put(t, filepath.Join(proj, ".claude", "settings.json"), "{\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(make:*)\"\n    ]\n  },\n  \"env\": {\n    \"API_KEY\": \"secret\"\n  }\n}\n", old)
	put(t, filepath.Join(proj, ".claude", "settings.local.json"), "{\n  \"permissions\": {\n    \"deny\": [\n      \"Bash(mv:*)\"\n    ]\n  },\n  \"hooks\": {\n    \"Stop\": [{\"hooks\": [{\"type\": \"command\", \"command\": \"echo fin\"}]}]\n  }\n}\n", old)
	put(t, filepath.Join(proj, ".claude", "rules", "api.md"), "---\npaths:\n  - \"src/**/*.go\"\n---\nr\n", old)
	put(t, filepath.Join(proj, ".claude", "rules", "tous.md"), "toujours\n", old)
	put(t, filepath.Join(proj, ".claude", "skills", "deploy", "SKILL.md"), "---\ndescription: d\n---\nx\n", old)
	put(t, filepath.Join(proj, ".mcp.json"), `{"mcpServers":{"mon-serveur":{"command":"x","env":{"TOKEN":"s"}}}}`, old)
	put(t, filepath.Join(proj, "README.md"), "doc\n", old)

	var projectCfg, localProjectCfg ProjectCfg
	for _, pc := range projectConfigs() {
		switch pc.Dir {
		case "-p":
			projectCfg = pc
		case "-v":
			localProjectCfg = pc
		}
	}
	if len(projectCfg.Files) != 2 || projectCfg.Perms["allow"][0].Rule != "Bash(make:*)" || projectCfg.Perms["allow"][0].Scope != "p" || projectCfg.Perms["deny"][0].Scope != "l" {
		t.Fatalf("réglages et règles : %+v", projectCfg)
	}
	mcp := map[string]string{}
	for _, mcpServer := range projectCfg.MCP {
		mcp[mcpServer.Name] = mcpServer.Scope
	}
	if len(projectCfg.Hooks) != 1 || projectCfg.Hooks[0].Event != "Stop" || len(mcp) != 2 || mcp["mon-serveur"] != "project" || mcp["local-srv"] != "local" || len(projectCfg.Skills) != 1 || projectCfg.Skills[0] != "deploy" {
		t.Fatalf("hooks, MCP, skills : %+v", projectCfg)
	}
	if len(projectCfg.Rules) != 2 || projectCfg.Rules[0].Name != "api.md" || len(projectCfg.Rules[0].Paths) != 1 || len(projectCfg.Rules[1].Paths) != 0 {
		t.Fatalf("règles du projet : %+v", projectCfg.Rules)
	}
	if !strings.Contains(strings.Join(projectCfg.Docs, " "), "README.md") || projectCfg.Specific == 0 {
		t.Fatalf("docs : %+v", projectCfg)
	}
	if projectCfg.Files[0].Label != "settings.json" || projectCfg.Files[1].Label != "settings.local.json" {
		t.Fatalf("libellés : %+v", projectCfg.Files)
	}
	rows := 0
	for _, projFile := range projectCfg.Files {
		rows += len(projFile.Rows)
		if !sort.SliceIsSorted(projFile.Rows, func(index, innerIndex int) bool { return projFile.Rows[index].Path < projFile.Rows[innerIndex].Path }) {
			t.Fatalf("lignes non triées : %+v", projFile.Rows)
		}
	}
	// 1 hook + 2 MCP + 1 skill + 2 règles + les lignes des deux fichiers
	if want := 6 + rows; projectCfg.Specific != want || rows < 4 {
		t.Fatalf("éléments spécifiques : %d au lieu de %d (%d lignes)", projectCfg.Specific, want, rows)
	}
	for _, projFile := range projectCfg.Files {
		for _, projRow := range projFile.Rows {
			if projRow.Value == "secret" {
				t.Fatal("secret non masqué dans la vue par projet")
			}
		}
	}
	if localProjectCfg.Specific != 0 || localProjectCfg.Dir != "-v" {
		t.Fatalf("projet sans réglage propre : %+v", localProjectCfg)
	}
	if projectConfigs()[0].Dir != "-p" { // ceux qui ont du spécifique d'abord
		t.Fatal("ordre")
	}
}

func TestReglesDeProjetDansLaMemoire(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	argument, body := t.TempDir(), t.TempDir()
	put(t, filepath.Join(cfg, "projects", "-a", id1+".jsonl"), `{"cwd":"`+argument+`"}`+"\n", old)
	put(t, filepath.Join(cfg, "projects", "-b", "22222222-2222-2222-2222-222222222222.jsonl"), `{"cwd":"`+body+`"}`+"\n", old)
	put(t, filepath.Join(argument, ".claude", "rules", "go.md"), "---\npaths:\n  - \"**/*.go\"\n---\nRègle Go\n", old)

	_, projects, _ := memList()
	var memFile MemFile
	for _, memProject := range projects {
		for _, localMemFile := range memProject.Files {
			if localMemFile.Key == "r/-a/go.md" {
				memFile = localMemFile
			}
		}
	}
	if memFile.Name != "rules/go.md" || len(memFile.Paths) != 1 || memFile.Always {
		t.Fatalf("règle de projet non listée ou mal annotée : %+v", memFile)
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
	if _, err := os.Stat(filepath.Join(body, ".claude", "rules", "go.md")); err != nil {
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
	if _, err := os.Stat(filepath.Join(body, ".claude", "rules", "go.md")); !os.IsNotExist(err) {
		t.Fatal("fichier resté")
	}
	// clés invalides
	for _, key := range []string{"r/../go.md", "r/-a/../x.md", "r/-a/.cache.md", "r/inconnu/x.md"} {
		if _, err := memRead(key); err == nil {
			t.Errorf("clé acceptée : %s", key)
		}
	}
}

// Une règle de 1 Mio ou plus n'est pas analysée (listée sans portée) ; juste en dessous, elle l'est.
func TestReglesDeProjetTaille(t *testing.T) {
	proj := t.TempDir()
	fm := "---\npaths:\n  - \"src/**\"\n---\n"
	put(t, filepath.Join(proj, ".claude", "rules", "a.md"), fm+strings.Repeat("x", 1<<20-len(fm)), old)
	put(t, filepath.Join(proj, ".claude", "rules", "b.md"), fm+strings.Repeat("x", 1<<20-len(fm)-1), old)
	rs := projectRules(proj)
	if len(rs) != 2 || len(rs[0].Paths) != 0 || len(rs[1].Paths) != 1 {
		t.Fatalf("%+v", rs)
	}
}

// Un dossier jetable (/tmp, $TMPDIR) reste un projet configurable mais n'est pas proposé dans les sélecteurs.
func TestSelecteurSansDossierJetable(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "projects", "-t", id1+".jsonl"), `{"cwd":"`+t.TempDir()+`"}`+"\n", old)
	if len(liveProjects()) != 1 {
		t.Fatal("le projet jetable doit rester un projet vivant")
	}
	if len(pickerProjects()) != 0 {
		t.Fatal("dossier jetable proposé dans le sélecteur")
	}
}
