package main

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

// Plugins, marketplaces et serveurs MCP d'une archive : tout passe par la CLI claude, seule à écrire dans ~/.claude et ~/.claude.json.

var mcpNameRe = regexp.MustCompile(`^[\w.-]{1,100}$`)

// mcpAddJSON : variable pour que les tests remplacent l'appel à claude.
var mcpAddJSON = func(name string, doc []byte) error {
	// un nom en « - » serait lu comme une option
	if !mcpNameRe.MatchString(name) || name[0] == '-' {
		return terr("srv.backup_cmds.nomMcpRefuse", "A1", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := withPath(exec.CommandContext(ctx, "claude", "mcp", "add-json", name, string(doc), "-s", "user"))
	cmd.Dir = workDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return cmdErr(string(out), err)
	}
	return nil
}

func cmdErr(out string, err error) error {
	if out = strings.TrimSpace(out); out != "" {
		return fmt.Errorf("%s", out)
	}
	return err
}

func readStaged(s *stage, name string, v any) bool {
	b, err := os.ReadFile(filepath.Join(s.dir, name))
	return err == nil && json.Unmarshal(b, v) == nil
}

func marketSource(m Marketplace) string {
	if m.Repo != "" {
		return m.Repo
	}
	return m.URL
}

// --- marketplaces ---

func planMarkets(s *stage) ([]SyncItem, error) {
	var d pluginsDoc
	if !readStaged(s, "plugins.json", &d) {
		return nil, nil
	}
	have := map[string]bool{}
	if ms, err := listMarketplaces(); err == nil {
		for _, m := range ms {
			have[m.Name] = true
		}
	}
	var items []SyncItem
	for _, m := range d.Marketplaces {
		st := "new"
		if have[m.Name] {
			st = "same"
		}
		items = append(items, SyncItem{ID: "market:" + m.Name, Kind: "market", Label: m.Name, Status: st, Cat: "plugins", From: marketSource(m)})
	}
	return items, nil
}

func applyMarkets(s *stage, picks []SyncPick, _ map[string]string) []BackupResult {
	var d pluginsDoc
	readStaged(s, "plugins.json", &d)
	res := make([]BackupResult, 0, len(picks))
	for _, p := range picks {
		name := strings.TrimPrefix(p.ID, "market:")
		r := BackupResult{ID: p.ID, Status: "skipped"}
		for _, m := range d.Marketplaces {
			if m.Name != name {
				continue
			}
			out, err := marketplaceAct("add", marketSource(m), "")
			r = BackupResult{ID: p.ID, Status: "ok"}
			if err != nil {
				r = BackupResult{ID: p.ID, Status: "failed", Message: cmdErr(out, err).Error()}
			}
			break
		}
		res = append(res, r)
	}
	return res
}

// --- plugins ---

func pluginItemID(p PluginInst) string { return "plugin:" + p.Scope + ":" + p.ID }

// projectKeyFor : le dossier de transcripts de ~/.claude/projects dont le projet est ce chemin.
func projectKeyFor(path string) string {
	for _, t := range memTargets() {
		if t.Project == path {
			return t.Dir
		}
	}
	return ""
}

func planPlugins(s *stage) ([]SyncItem, error) {
	var d pluginsDoc
	if !readStaged(s, "plugins.json", &d) {
		return nil, nil
	}
	cur := map[string]PluginInst{}
	if inst, _, err := listPlugins("", 0); err == nil {
		for _, p := range inst {
			cur[p.Scope+":"+p.ID+":"+p.Project] = p
		}
	}
	var items []SyncItem
	for _, p := range d.Plugins {
		if p.Scope != "user" && !isDirPath(p.Project) {
			continue
		}
		st := "new"
		if c, ok := cur[p.Scope+":"+p.ID+":"+p.Project]; ok {
			st = "same"
			if c.Enabled != p.Enabled {
				st = "diff"
			}
		}
		it := SyncItem{ID: pluginItemID(p), Kind: "plugin", Label: p.ID, Status: st, Cat: "plugins", From: p.Scope, To: p.Project}
		items = append(items, it)
	}
	return items, nil
}

// skippedPlugins : plugins de portée project/local dont le dossier de projet n'existe pas ici.
func skippedPlugins(s *stage) []string {
	var d pluginsDoc
	var out []string
	if readStaged(s, "plugins.json", &d) {
		for _, p := range d.Plugins {
			if p.Scope != "user" && !isDirPath(p.Project) {
				out = append(out, pluginItemID(p))
			}
		}
	}
	return out
}

func applyPlugins(s *stage, picks []SyncPick, _ map[string]string) []BackupResult {
	var d pluginsDoc
	readStaged(s, "plugins.json", &d)
	cur := map[string]PluginInst{}
	if inst, _, err := listPlugins("", 0); err == nil {
		for _, p := range inst {
			cur[p.Scope+":"+p.ID+":"+p.Project] = p
		}
	}
	res := make([]BackupResult, 0, len(picks))
	for _, pk := range picks {
		var p *PluginInst
		for i := range d.Plugins {
			if pluginItemID(d.Plugins[i]) == pk.ID {
				p = &d.Plugins[i]
				break
			}
		}
		if p == nil {
			res = append(res, BackupResult{ID: pk.ID, Status: "skipped"})
			continue
		}
		proj := ""
		if p.Scope != "user" {
			if proj = projectKeyFor(p.Project); proj == "" {
				res = append(res, BackupResult{ID: pk.ID, Status: "skipped"})
				continue
			}
		}
		c, installed := cur[p.Scope+":"+p.ID+":"+p.Project]
		var err error
		var out string
		if !installed {
			out, err = pluginAct("install", p.ID, p.Scope, proj)
		}
		// une installation active le plugin : seul l'état « désactivé » demande un appel de plus
		if err == nil && ((installed && c.Enabled != p.Enabled) || (!installed && !p.Enabled)) {
			act := "enable"
			if !p.Enabled {
				act = "disable"
			}
			out, err = pluginAct(act, p.ID, p.Scope, proj)
		}
		r := BackupResult{ID: pk.ID, Status: "ok"}
		if err != nil {
			r = BackupResult{ID: pk.ID, Status: "failed", Message: cmdErr(out, err).Error()}
		}
		res = append(res, r)
	}
	return res
}

// --- serveurs MCP ---

type mcpArchive struct {
	Servers  map[string]json.RawMessage `json:"servers"`
	Stripped map[string][]string        `json:"stripped"`
}

func planMcp(s *stage) ([]SyncItem, error) {
	var d mcpArchive
	if !readStaged(s, "mcp.json", &d) {
		return nil, nil
	}
	have := map[string]bool{}
	for _, m := range mcpServers() {
		if m.Scope == "user" {
			have[m.Name] = true
		}
	}
	names := make([]string, 0, len(d.Servers))
	for n := range d.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	var items []SyncItem
	for _, n := range names {
		var f map[string]json.RawMessage
		json.Unmarshal(d.Servers[n], &f)
		it := SyncItem{ID: "mcp:" + n, Kind: "mcp", Label: n, Status: "new", Cat: "mcp"}
		if have[n] {
			it.Status = "same"
		}
		if _, ok := f["command"]; ok {
			it.Risk = "exec"
		}
		if st := d.Stripped[n]; len(st) > 0 {
			it.From = L("srv.backup_cmds.aSaisir", "A1", strings.Join(st, ", "))
		}
		items = append(items, it)
	}
	return items, nil
}

func applyMcp(s *stage, picks []SyncPick, _ map[string]string) []BackupResult {
	var d mcpArchive
	readStaged(s, "mcp.json", &d)
	res := make([]BackupResult, 0, len(picks))
	for _, pk := range picks {
		name := strings.TrimPrefix(pk.ID, "mcp:")
		doc, ok := d.Servers[name]
		if !ok {
			res = append(res, BackupResult{ID: pk.ID, Status: "skipped"})
			continue
		}
		r := BackupResult{ID: pk.ID, Status: "ok"}
		if err := mcpAddJSON(name, doc); err != nil {
			r = BackupResult{ID: pk.ID, Status: "failed", Message: err.Error()}
		}
		res = append(res, r)
	}
	return res
}
