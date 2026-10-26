package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Plugins de Claude Code (skills, commandes, agents…) : lecture et gestion par `claude plugin`, qui reste seul à écrire dans ~/.claude.

var (
	pluginRefRe = regexp.MustCompile(`^[\w.-]{1,100}(@[\w.-]{1,100})?$`)
	pluginActs  = map[string]bool{"install": true, "uninstall": true, "enable": true, "disable": true, "update": true}
	pluginScope = map[string]bool{"user": true, "project": true, "local": true}
)

type PluginInst struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Scope   string `json:"scope"`
	Enabled bool   `json:"enabled"`
	Project string `json:"project,omitempty"`
}

type PluginAvail struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Marketplace string `json:"marketplace"`
	Installs    int    `json:"installs"`
	Installed   bool   `json:"installed"`
}

var plugCache struct {
	sync.Mutex
	at  time.Time
	raw []byte
}

// pluginRaw : la sortie de `claude plugin list --json --available` (2 Mo, plusieurs secondes) gardée une minute.
var pluginRaw = func() ([]byte, error) {
	plugCache.Lock()
	defer plugCache.Unlock()
	if plugCache.raw != nil && time.Since(plugCache.at) < time.Minute {
		return plugCache.raw, nil
	}
	b, err := runPlugin("", 60*time.Second, "list", "--json", "--available")
	if err != nil {
		return nil, err
	}
	plugCache.raw, plugCache.at = b, time.Now()
	return b, nil
}

func runPlugin(dir string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := withPath(exec.CommandContext(ctx, "claude", append([]string{"plugin"}, args...)...))
	cmd.Dir = dir
	if cmd.Dir == "" {
		cmd.Dir = workDir()
	}
	return cmd.CombinedOutput()
}

// listPlugins : les plugins installés (tous portées) et les plus installés parmi ceux des marketplaces qui correspondent à q.
func listPlugins(q string, limit int) ([]PluginInst, []PluginAvail, error) {
	b, err := pluginRaw()
	if err != nil {
		return nil, nil, err
	}
	var d struct {
		Installed []struct {
			ID          string `json:"id"`
			Version     string `json:"version"`
			Scope       string `json:"scope"`
			Enabled     bool   `json:"enabled"`
			ProjectPath string `json:"projectPath"`
		} `json:"installed"`
		Available []struct {
			ID          string `json:"pluginId"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Marketplace string `json:"marketplaceName"`
			Installs    int    `json:"installCount"`
		} `json:"available"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, nil, err
	}
	have := map[string]bool{}
	inst := []PluginInst{}
	for _, p := range d.Installed {
		have[p.ID] = true
		inst = append(inst, PluginInst{p.ID, p.Version, p.Scope, p.Enabled, p.ProjectPath})
	}
	sort.Slice(inst, func(i, j int) bool { return inst[i].ID < inst[j].ID })
	q = strings.ToLower(strings.TrimSpace(q))
	var av []PluginAvail
	for _, p := range d.Available {
		if q != "" && !strings.Contains(strings.ToLower(p.ID+" "+p.Description), q) {
			continue
		}
		av = append(av, PluginAvail{p.ID, p.Name, p.Description, p.Marketplace, p.Installs, have[p.ID]})
	}
	sort.SliceStable(av, func(i, j int) bool { return av[i].Installs > av[j].Installs })
	if len(av) > limit {
		av = av[:limit]
	}
	return inst, nav(av), nil
}

func nav(a []PluginAvail) []PluginAvail {
	if a == nil {
		return []PluginAvail{}
	}
	return a
}

// pluginAct : install, uninstall, enable, disable ou update d'un plugin pour une portée ; project (dossier de ~/.claude/projects)
// donne le dossier de travail des portées project et local.
func pluginAct(act, id, scope, project string) (string, error) {
	if !pluginActs[act] || !pluginRefRe.MatchString(id) || !pluginScope[scope] {
		return "", terr("srv.http_util.requeteInvalide")
	}
	dir := ""
	if scope != "user" {
		if !safeDir(project) {
			return "", terr("srv.plugins.projetRequis")
		}
		dir = projectCwd(project)
		if fi, e := os.Stat(dir); dir == "" || e != nil || !fi.IsDir() {
			return "", terr("srv.memory.dossierDuProjetIntrouvable")
		}
	}
	out, err := runPlugin(dir, 3*time.Minute, act, id, "-s", scope)
	plugCache.Lock()
	plugCache.raw = nil
	plugCache.Unlock()
	return strings.TrimSpace(string(out)), err
}
