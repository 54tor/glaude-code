package main

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
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

type Marketplace struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Repo   string `json:"repo,omitempty"`
	URL    string `json:"url,omitempty"`
}

var (
	marketNameRe = regexp.MustCompile(`^[\w.-]{1,100}$`)
	marketSrcRe  = regexp.MustCompile(`^([\w.-]{1,100}/[\w.-]{1,100}|https://[\w.-]+(:\d+)?/[\w./~%+-]{1,300})$`) // dépôt GitHub ou URL https ; jamais un chemin local
)

func listMarketplaces() ([]Marketplace, error) {
	b, err := runPlugin("", 30*time.Second, "marketplace", "list", "--json")
	if err != nil {
		return nil, err
	}
	out := []Marketplace{}
	return out, json.Unmarshal(b, &out)
}

// marketplaceAct : add (source = dépôt « owner/repo » ou URL https), remove, update (name vide = tous).
// marketInternal : une URL https vers la machine, un réseau privé ou le lien local (métadonnées cloud), écrite en clair.
// ponytail: un nom DNS qui résout vers une adresse interne passe ; résoudre puis épingler l'adresse si cela devient un risque.
func marketInternal(source string) bool {
	host, ok := strings.CutPrefix(source, "https://")
	if !ok {
		return false
	}
	host, _, _ = strings.Cut(host, "/")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && (a.IsUnspecified() || !externalIP(a.String()))
}

func marketplaceAct(act, source, name string) (string, error) {
	var args []string
	switch {
	case act == "add" && marketSrcRe.MatchString(source) && !marketInternal(source):
		args = []string{"add", "--scope", "user", source}
	case act == "remove" && marketNameRe.MatchString(name):
		args = []string{"remove", name}
	case act == "update" && (name == "" || marketNameRe.MatchString(name)):
		args = []string{"update"}
		if name != "" {
			args = append(args, name)
		}
	default:
		return "", terr("srv.http_util.requeteInvalide")
	}
	out, err := runPlugin("", 3*time.Minute, append([]string{"marketplace"}, args...)...)
	plugCache.Lock()
	plugCache.raw = nil
	plugCache.Unlock()
	return strings.TrimSpace(string(out)), err
}
