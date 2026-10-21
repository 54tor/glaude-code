package main

// Lecture de la configuration : précédence des réglages, santé, hooks, serveurs MCP. Rien n'est modifié ici.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// --- précédence --------------------------------------------------------------------------------------

// EffRow : un réglage et ses valeurs par niveau. Les niveaux, du moins au plus prioritaire : u, p, l.
type EffRow struct {
	Path   string            `json:"path"`
	Values map[string]string `json:"values"` // niveau -> valeur (texte abrégé)
	Winner string            `json:"winner"` // niveau dont la valeur s'applique (pour une liste : tous s'ajoutent)
	Merged bool              `json:"merged,omitempty"`
}

var levelRank = map[string]int{"u": 0, "p": 1, "l": 2}

func level(id string) string { return id[:1] }

// flatten met un document à plat : « permissions.allow[] » pour les éléments d'une liste, un chemin par feuille.
func flatten(prefix string, v any, depth int, out map[string][]string) {
	switch x := v.(type) {
	case *omap:
		if depth >= 2 || prefix == "hooks" {
			out[prefix] = append(out[prefix], summarize(v))
			return
		}
		for _, k := range x.keys {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			flatten(p, x.m[k], depth+1, out)
		}
	case []any:
		for _, e := range x {
			out[prefix+"[]"] = append(out[prefix+"[]"], summarize(e))
		}
	default:
		out[prefix] = append(out[prefix], summarize(v))
	}
}

func summarize(v any) string {
	switch x := v.(type) {
	case string:
		return cut(x, 120)
	case *omap:
		return fmt.Sprintf("{%d clé(s)}", len(x.keys))
	case []any:
		return fmt.Sprintf("[%d]", len(x))
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// effective : les réglages vus d'un projet (dir vide : utilisateur seul). Une liste s'additionne d'un niveau à l'autre ;
// pour le reste, le niveau le plus prioritaire l'emporte.
func effective(raw map[string]any, dir string) []EffRow {
	ids := []string{"u"}
	if dir != "" {
		ids = append(ids, "p/"+dir, "l/"+dir)
	}
	type cell struct{ vals map[string][]string }
	cells := map[string]*cell{}
	for _, id := range ids {
		doc, ok := raw[id]
		if !ok {
			continue
		}
		flat := map[string][]string{}
		flatten("", masked(doc), 0, flat)
		for path, vals := range flat {
			if cells[path] == nil {
				cells[path] = &cell{map[string][]string{}}
			}
			cells[path].vals[level(id)] = vals
		}
	}
	var rows []EffRow
	for path, c := range cells {
		r := EffRow{Path: path, Values: map[string]string{}, Merged: strings.HasSuffix(path, "[]")}
		best := -1
		for lv, vals := range c.vals {
			r.Values[lv] = strings.Join(vals, " · ")
			if levelRank[lv] > best {
				best, r.Winner = levelRank[lv], lv
			}
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	return rows
}

func masked(v any) any { return maskSecrets(v, "") }

// --- hooks -------------------------------------------------------------------------------------------

type HookInfo struct {
	Scope   string `json:"scope"` // niveau : u, p, l
	File    string `json:"file"`  // identifiant du fichier
	Event   string `json:"event"`
	Matcher string `json:"matcher,omitempty"`
	Type    string `json:"type"`
	Command string `json:"command"`
	Missing string `json:"missing,omitempty"` // ce qui manque : fichier ou programme introuvable
}

var shellBuiltins = map[string]bool{"[": true, "test": true, "echo": true, "exit": true, "cd": true, "true": true, "false": true, ":": true, "export": true, "set": true, "if": true, "then": true, "fi": true, "printf": true, "exec": true}

// missingPart : la première chose que la commande d'un hook référence et qui n'existe pas (un fichier, ou un programme
// en position de commande). Les variables autres que $HOME ne sont pas résolues : on ne devine pas.
func missingPart(cmd string) string {
	home, _ := os.UserHomeDir()
	exp := func(s string) string {
		s = strings.NewReplacer("${HOME}", home, "$HOME", home).Replace(s)
		if strings.HasPrefix(s, "~/") {
			s = filepath.Join(home, s[2:])
		}
		return s
	}
	tokens := regexp.MustCompile(`"([^"]*)"|'([^']*)'|(\S+)`).FindAllStringSubmatch(cmd, -1)
	atCommand := true // le mot suivant est un nom de programme (début, ou après && || ; |)
	for _, m := range tokens {
		w := m[1] + m[2] + m[3]
		quoted := m[3] == ""
		op := !quoted && (w == "&&" || w == "||" || w == ";" || w == "|")
		w = strings.TrimRight(w, ";")
		if op {
			atCommand = true
			continue
		}
		endsCmd := !quoted && strings.HasSuffix(m[3], ";")
		skip := w == "" || strings.HasPrefix(w, "-") || strings.Contains(w, "://") || strings.ContainsAny(w, "=<>*?`") || w == "]" || w == "["
		w = exp(w)
		if !skip && !strings.Contains(w, "$") {
			switch {
			case strings.HasPrefix(w, "/") || strings.HasPrefix(w, "./"):
				if _, err := os.Stat(w); err != nil {
					return w
				}
			case atCommand && !shellBuiltins[w] && !strings.Contains(w, "/"):
				if _, err := exec.LookPath(w); err != nil {
					return w
				}
			}
		}
		atCommand = endsCmd
	}
	return ""
}

func hooksOf(raw map[string]any) []HookInfo {
	var out []HookInfo
	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if li, lj := levelRank[level(ids[i])], levelRank[level(ids[j])]; li != lj {
			return li < lj
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		hv, ok := lookup(raw[id], "hooks")
		ho := asMap(hv)
		if !ok || ho == nil {
			continue
		}
		for _, ev := range ho.keys {
			entries, _ := ho.m[ev].([]any)
			for _, e := range entries {
				eo := asMap(e)
				if eo == nil {
					continue
				}
				matcher, _ := eo.m["matcher"].(string)
				list, _ := eo.m["hooks"].([]any)
				for _, h := range list {
					if o := asMap(h); o != nil {
						typ, _ := o.m["type"].(string)
						cmd, _ := o.m["command"].(string)
						hi := HookInfo{Scope: level(id), File: id, Event: ev, Matcher: matcher, Type: typ, Command: cmd}
						if typ == "command" {
							hi.Missing = missingPart(cmd)
						}
						out = append(out, hi)
					}
				}
			}
		}
	}
	return out
}

// --- santé -------------------------------------------------------------------------------------------

type CfgIssue struct {
	File string `json:"file"`
	Kind string `json:"kind"` // json, hook, permission, plugin, secret
	Msg  string `json:"msg"`
}

var broadRule = regexp.MustCompile(`^(\*|Bash|Bash\(\*\)|Bash\(:\*\)|Edit|Write|Read|WebFetch)$`)

func installedPlugins() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(filepath.Join(cfgDir(), "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var d struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if json.Unmarshal(b, &d) != nil {
		return nil
	}
	for k := range d.Plugins {
		out[k] = true
	}
	return out
}

func configHealth(files []ConfigFile, raw map[string]any) []CfgIssue {
	var issues []CfgIssue
	for _, f := range files {
		if f.Error != "" {
			issues = append(issues, CfgIssue{f.ID, "json", f.Label + " : " + f.Error})
		}
	}
	for _, h := range hooksOf(raw) {
		if h.Missing != "" {
			issues = append(issues, CfgIssue{h.File, "hook", "hook " + h.Event + " : « " + h.Missing + " » introuvable (" + cut(h.Command, 70) + ")"})
		}
	}
	plugins := installedPlugins()
	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		doc := raw[id]
		for _, kind := range []string{"allow", "deny", "ask"} {
			v, _ := lookup(doc, "permissions", kind)
			seen := map[string]bool{}
			for _, r := range strList(v) {
				if seen[r] {
					issues = append(issues, CfgIssue{id, "permission", "règle en double dans " + kind + " : " + r})
				}
				seen[r] = true
				if kind == "allow" && broadRule.MatchString(r) {
					issues = append(issues, CfgIssue{id, "permission", "règle trop large dans allow : « " + r + " » autorise tout l'outil"})
				}
			}
		}
		a, _ := lookup(doc, "permissions", "allow")
		d, _ := lookup(doc, "permissions", "deny")
		deny := map[string]bool{}
		for _, r := range strList(d) {
			deny[r] = true
		}
		for _, r := range strList(a) {
			if deny[r] {
				issues = append(issues, CfgIssue{id, "permission", "« " + r + " » est à la fois autorisée et refusée (le refus l'emporte)"})
			}
		}
		if ep, ok := lookup(doc, "enabledPlugins"); ok && plugins != nil {
			if o := asMap(ep); o != nil {
				for _, k := range o.keys {
					if on, _ := o.m[k].(bool); on && !plugins[k] {
						issues = append(issues, CfgIssue{id, "plugin", "plugin activé mais pas installé : " + k})
					}
				}
			}
		}
		if id[0] == 'p' { // versionné avec le projet : pas de secret ici
			if e, ok := lookup(doc, "env"); ok {
				if o := asMap(e); o != nil {
					for _, k := range o.keys {
						if s, _ := o.m[k].(string); secretKey.MatchString(k) && s != "" {
							issues = append(issues, CfgIssue{id, "secret", "la variable " + k + " ressemble à un secret dans un fichier de projet (versionné) : préférez settings.local.json"})
						}
					}
				}
			}
		}
	}
	return issues
}

// --- serveurs MCP (lecture seule) ----------------------------------------------------------------------

type McpServer struct {
	Name    string   `json:"name"`
	Scope   string   `json:"scope"` // user, project (.mcp.json), local (~/.claude.json, par projet)
	Project string   `json:"project,omitempty"`
	Type    string   `json:"type"`
	Command string   `json:"command,omitempty"`
	URL     string   `json:"url,omitempty"`
	EnvKeys []string `json:"envKeys,omitempty"`
}

func mcpFrom(servers any, scope, project string) []McpServer {
	o := asMap(servers)
	if o == nil {
		return nil
	}
	var out []McpServer
	for _, name := range o.keys {
		s := asMap(o.m[name])
		if s == nil {
			continue
		}
		m := McpServer{Name: name, Scope: scope, Project: project}
		m.Type, _ = s.m["type"].(string)
		m.Command, _ = s.m["command"].(string)
		if args := strList(s.m["args"]); len(args) > 0 {
			m.Command = strings.TrimSpace(m.Command + " " + cut(strings.Join(args, " "), 80))
		}
		m.URL, _ = s.m["url"].(string)
		if m.Type == "" {
			if m.URL != "" {
				m.Type = "http"
			} else {
				m.Type = "stdio"
			}
		}
		if e := asMap(s.m["env"]); e != nil {
			m.EnvKeys = append(m.EnvKeys, e.keys...)
		}
		if h := asMap(s.m["headers"]); h != nil {
			m.EnvKeys = append(m.EnvKeys, h.keys...)
		}
		out = append(out, m)
	}
	return out
}

// mcpServers : les serveurs déclarés dans ~/.claude.json (utilisateur, puis par projet) et dans les .mcp.json des projets.
// Seules les clés des serveurs sont lues : le reste de ~/.claude.json (compte, identifiants) n'est jamais exposé.
func mcpServers() []McpServer {
	var out []McpServer
	home, _ := os.UserHomeDir()
	if b, err := os.ReadFile(filepath.Join(home, ".claude.json")); err == nil {
		if v, err := parseOrdered(b); err == nil {
			if s, ok := lookup(v, "mcpServers"); ok {
				out = append(out, mcpFrom(s, "user", "")...)
			}
			if p, ok := lookup(v, "projects"); ok {
				if po := asMap(p); po != nil {
					for _, path := range po.keys {
						if s, ok := lookup(po.m[path], "mcpServers"); ok {
							out = append(out, mcpFrom(s, "local", path)...)
						}
					}
				}
			}
		}
	}
	_, projects, _ := memList()
	for _, p := range projects {
		if p.Missing {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(p.Project, ".mcp.json")); err == nil {
			if v, err := parseOrdered(b); err == nil {
				if s, ok := lookup(v, "mcpServers"); ok {
					out = append(out, mcpFrom(s, "project", p.Project)...)
				}
			}
		}
	}
	return out
}

var mcpLine = regexp.MustCompile(`^(.+?): (.*) - (.+)$`)

// mcpStatus lance `claude mcp list` (il contrôle chaque serveur : quelques secondes) et renvoie l'état par nom.
var mcpStatus = func() (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := withPath(exec.CommandContext(ctx, "claude", "mcp", "list"))
	cmd.Dir = workDir()
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, err
	}
	res := map[string]string{}
	for _, l := range strings.Split(string(out), "\n") {
		if m := mcpLine.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			res[m[1]] = strings.TrimSpace(m[3])
		}
	}
	return res, nil
}
