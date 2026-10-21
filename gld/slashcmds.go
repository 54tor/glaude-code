package main

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type slashCmd struct {
	Cmd    string `json:"cmd"` // « /nom » ou « /plugin:nom »
	Desc   string `json:"desc"`
	Origin string `json:"origin"` // user, project, plugin
}

// scanCommands : les commandes d'un dossier commands/ (un .md par commande, sous-dossiers ignorés).
func scanCommands(base, prefix, origin string) []slashCmd {
	var out []slashCmd
	ents, _ := os.ReadDir(base)
	for _, e := range ents {
		n := strings.TrimSuffix(e.Name(), ".md")
		if e.IsDir() || n == e.Name() || !skillName.MatchString(n) {
			continue
		}
		var s Skill
		if b, err := os.ReadFile(filepath.Join(base, e.Name())); err == nil && len(b) <= skillMaxBytes {
			s.Dir = n
			describeSkill(&s, string(b))
		}
		out = append(out, slashCmd{"/" + prefix + n, cut(s.Description, 200), origin})
	}
	return out
}

// slashCommands : les « / » que l'utilisateur a en plus de ceux de Claude Code, personnels, de projet et de plugins.
func slashCommands() []slashCmd {
	out := scanCommands(filepath.Join(cfgDir(), "commands"), "", "user")
	for _, s := range listSkills() {
		if s.Hidden || s.Scope == "store" {
			continue
		}
		c := "/" + s.Dir
		if s.Scope == "plugin" {
			c = "/" + strings.SplitN(s.Plugin, "@", 2)[0] + ":" + s.Dir
		}
		out = append(out, slashCmd{c, cut(s.Description, 200), s.Scope})
	}
	for _, p := range liveProjects() {
		out = append(out, scanCommands(filepath.Join(p.Project, ".claude", "commands"), "", "project")...)
	}
	for _, pr := range pluginSkillRoots() {
		out = append(out, scanCommands(filepath.Join(filepath.Dir(pr.base), "commands"), strings.SplitN(pr.id, "@", 2)[0]+":", "plugin")...)
	}
	seen := map[string]bool{}
	uniq := out[:0]
	for _, c := range out {
		if !seen[c.Cmd] {
			seen[c.Cmd] = true
			uniq = append(uniq, c)
		}
	}
	sort.Slice(uniq, func(i, j int) bool { return uniq[i].Cmd < uniq[j].Cmd })
	return uniq
}

func routesSlash(mux *http.ServeMux) {
	mux.HandleFunc("GET /slash-commands", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"commands": slashCommands()})
	})
}
