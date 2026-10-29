package main

import (
	"io"
	"net/http"
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
	ents, _ := readDirTrusted(base)
	for _, e := range ents {
		n := strings.TrimSuffix(e.Name(), ".md")
		if e.IsDir() || n == e.Name() || !skillName.MatchString(n) {
			continue
		}
		var s Skill
		if b := readSmall(filepath.Join(base, e.Name())); b != nil {
			s.Dir = n
			describeSkill(&s, string(b))
		}
		out = append(out, slashCmd{"/" + prefix + n, cut(s.Description, 200), origin})
	}
	return out
}

// readSmall : le contenu d'un fichier régulier d'au plus skillMaxBytes (ni lien, ni tube, ni gros fichier), nil sinon.
func readSmall(p string) []byte {
	f, err := openTrusted(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || st.Size() > skillMaxBytes {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(f, skillMaxBytes+1))
	if len(b) > skillMaxBytes {
		return nil
	}
	return b
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
	mux.HandleFunc("GET /v1/slash-commands", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"commands": slashCommands()})
	})
}
