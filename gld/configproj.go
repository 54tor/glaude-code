package main

// Ce que chaque projet configure en propre : ses fichiers de réglages (projet et local), ses règles de permission, ses hooks,
// ses serveurs MCP, ses règles (.claude/rules), ses skills et sa documentation. Lecture seule : un clic mène à l'édition.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ProjRow struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

type ProjFile struct {
	ID    string    `json:"id"`
	Label string    `json:"label"` // settings.json (versionné avec le projet) ou settings.local.json (à vous seul)
	Rows  []ProjRow `json:"rows"`
}

type ProjRule struct {
	Rule  string `json:"rule"`
	Scope string `json:"scope"` // p : réglages du projet ; l : réglages locaux
}

type ProjDocRule struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths,omitempty"` // règle scopée : motifs du champ paths
	Warn  string   `json:"warn,omitempty"`
}

type ProjectCfg struct {
	Dir      string                `json:"dir"`
	Project  string                `json:"project"`
	Files    []ProjFile            `json:"files"`
	Perms    map[string][]ProjRule `json:"perms"`
	Hooks    []HookInfo            `json:"hooks"`
	MCP      []McpServer           `json:"mcp"`
	Rules    []ProjDocRule         `json:"rules"`
	Skills   []string              `json:"skills"`
	Docs     []string              `json:"docs"`
	Specific int                   `json:"specific"` // nombre d'éléments propres au projet : 0 = rien de spécifique
}

// projectRules : les règles Markdown de .claude/rules d'un projet (lecture seule), avec leur portée.
func projectRules(cwd string) []ProjDocRule {
	var out []ProjDocRule
	ents, _ := os.ReadDir(filepath.Join(cwd, ".claude", "rules"))
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		r := ProjDocRule{Name: e.Name()}
		if b, err := os.ReadFile(filepath.Join(cwd, ".claude", "rules", e.Name())); err == nil && len(b) < 1<<20 {
			if m, err := parseMeta(string(b)); err != nil {
				r.Warn = err.Error()
			} else if _, has := m["paths"]; has {
				g, err := globsOf(m)
				if err != nil {
					r.Warn = err.Error()
				}
				r.Paths = g
			}
		}
		out = append(out, r)
	}
	return out
}

func projectConfigs() []ProjectCfg {
	_, raw := allConfigs()
	hooks := hooksOf(raw)
	mcp := mcpServers()
	skills := listSkills()
	_, mem, _ := memList()
	docs := map[string][]string{}
	for _, p := range mem {
		for _, f := range p.Files {
			if strings.HasPrefix(f.Key, "p/") {
				docs[p.Dir] = append(docs[p.Dir], f.Name)
			}
		}
	}
	out := []ProjectCfg{}
	for _, lp := range configProjects() {
		pc := ProjectCfg{Dir: lp.Dir, Project: lp.Project, Perms: map[string][]ProjRule{}, Files: []ProjFile{}, Hooks: []HookInfo{}, MCP: []McpServer{}, Skills: []string{}, Docs: docs[lp.Dir]}
		for _, scope := range []string{"p", "l"} {
			id := scope + "/" + lp.Dir
			doc, ok := raw[id]
			if !ok {
				continue
			}
			label := "settings.json"
			if scope == "l" {
				label = "settings.local.json"
			}
			flat := map[string][]string{}
			flatten("", maskSecrets(doc, ""), 0, flat)
			pf := ProjFile{ID: id, Label: label, Rows: []ProjRow{}}
			for path, vals := range flat {
				pf.Rows = append(pf.Rows, ProjRow{path, strings.Join(vals, " · ")})
			}
			sort.Slice(pf.Rows, func(i, j int) bool { return pf.Rows[i].Path < pf.Rows[j].Path })
			pc.Files = append(pc.Files, pf)
			for _, kind := range append(append([]string{}, permKinds...), "additionalDirectories") {
				v, _ := lookup(doc, "permissions", kind)
				for _, r := range strList(v) {
					pc.Perms[kind] = append(pc.Perms[kind], ProjRule{r, scope})
				}
			}
		}
		for _, h := range hooks {
			if h.File == "p/"+lp.Dir || h.File == "l/"+lp.Dir {
				pc.Hooks = append(pc.Hooks, h)
			}
		}
		for _, m := range mcp {
			if m.Project == lp.Project && (m.Scope == "project" || m.Scope == "local") {
				pc.MCP = append(pc.MCP, m)
			}
		}
		for _, s := range skills {
			if s.Scope == "project" && s.Project == lp.Project {
				pc.Skills = append(pc.Skills, s.Name)
			}
		}
		pc.Rules = projectRules(lp.Project)
		if pc.Rules == nil {
			pc.Rules = []ProjDocRule{}
		}
		if pc.Docs == nil {
			pc.Docs = []string{}
		}
		n := len(pc.Hooks) + len(pc.MCP) + len(pc.Skills) + len(pc.Rules)
		for _, f := range pc.Files {
			n += len(f.Rows)
		}
		pc.Specific = n
		out = append(out, pc)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Specific > 0) != (out[j].Specific > 0) {
			return out[i].Specific > 0
		}
		return out[i].Project < out[j].Project
	})
	return out
}
