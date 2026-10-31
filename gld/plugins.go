package main

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
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

const pluginCommandTimeout = 3 * time.Minute

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
	rawList, err := runPlugin("", 60*time.Second, "list", "--json", "--available")
	if err != nil {
		return nil, err
	}
	plugCache.raw, plugCache.at = rawList, time.Now()
	return rawList, nil
}

func runPlugin(dir string, timeout time.Duration, args ...string) ([]byte, error) {
	commandContext, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := withPath(exec.CommandContext(commandContext, "claude", append([]string{"plugin"}, args...)...))
	cmd.Dir = dir
	if cmd.Dir == "" {
		cmd.Dir = workDir()
	}
	return cmd.CombinedOutput()
}

// listPlugins : les plugins installés (tous portées) et les plus installés parmi ceux des marketplaces qui correspondent à query.
func listPlugins(query string, limit int) ([]PluginInst, []PluginAvail, error) {
	rawList, err := pluginRaw()
	if err != nil {
		return nil, nil, err
	}
	var listing struct {
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
	if decodeErr := json.Unmarshal(rawList, &listing); decodeErr != nil {
		return nil, nil, decodeErr
	}
	installed := map[string]bool{}
	installedList := []PluginInst{}
	for _, plugin := range listing.Installed {
		installed[plugin.ID] = true
		installedList = append(installedList, PluginInst{plugin.ID, plugin.Version, plugin.Scope, plugin.Enabled, plugin.ProjectPath})
	}
	sort.Slice(installedList, func(leftIndex, rightIndex int) bool {
		return installedList[leftIndex].ID < installedList[rightIndex].ID
	})
	query = strings.ToLower(strings.TrimSpace(query))
	var available []PluginAvail
	for _, plugin := range listing.Available {
		if query != "" && !strings.Contains(strings.ToLower(plugin.ID+" "+plugin.Description), query) {
			continue
		}
		available = append(available, PluginAvail{plugin.ID, plugin.Name, plugin.Description, plugin.Marketplace, plugin.Installs, installed[plugin.ID]})
	}
	sort.SliceStable(available, func(leftIndex, rightIndex int) bool {
		return available[leftIndex].Installs > available[rightIndex].Installs
	})
	if len(available) > limit {
		available = available[:limit]
	}
	return installedList, nav(available), nil
}

func nav(available []PluginAvail) []PluginAvail {
	if available == nil {
		return []PluginAvail{}
	}
	return available
}

// pluginAct : install, uninstall, enable, disable ou update d'un plugin pour une portée ; project (dossier de ~/.claude/projects)
// donne le dossier de travail des portées project et local.
func pluginAct(action, pluginRef, scope, projectDir string) (string, error) {
	if !pluginActs[action] || !pluginRefRe.MatchString(pluginRef) || !pluginScope[scope] {
		return "", terr("srv.http_util.requeteInvalide")
	}
	dir := ""
	if scope != "user" {
		if !safeDir(projectDir) {
			return "", terr("srv.plugins.projetRequis")
		}
		dir = projectCwd(projectDir)
		if dir == "" || !isDirPath(dir) {
			return "", terr("srv.memory.dossierDuProjetIntrouvable")
		}
	}
	output, err := runPlugin(dir, pluginCommandTimeout, action, pluginRef, "-s", scope)
	plugCache.Lock()
	plugCache.raw = nil
	plugCache.Unlock()
	return strings.TrimSpace(string(output)), err
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

var marketRaw = func() ([]byte, error) {
	return runPlugin("", 30*time.Second, "marketplace", "list", "--json")
}

func listMarketplaces() ([]Marketplace, error) {
	rawList, err := marketRaw()
	if err != nil {
		return nil, err
	}
	marketplaces := []Marketplace{}
	return marketplaces, json.Unmarshal(rawList, &marketplaces)
}

// marketplaceAct : add (source = dépôt « owner/repo » ou URL https), remove, update (name vide = tous).
// marketInternal : une URL https vers la machine, un réseau privé ou le lien local (métadonnées cloud), écrite en clair.
// ponytail: un nom DNS qui résout vers une adresse interne passe ; résoudre puis épingler l'adresse si cela devient un risque.
func marketInternal(source string) bool {
	host, isHTTPS := strings.CutPrefix(source, "https://")
	if !isHTTPS {
		return false
	}
	host, _, _ = strings.Cut(host, "/")
	if hostOnly, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		host = hostOnly
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address, parseErr := netip.ParseAddr(host)
	return parseErr == nil && (address.IsUnspecified() || !externalIP(address.String()))
}

func marketplaceAct(action, source, name string) (string, error) {
	var args []string
	switch {
	case action == "add" && marketSrcRe.MatchString(source) && !marketInternal(source):
		args = []string{"add", "--scope", "user", source}
	case action == "remove" && marketNameRe.MatchString(name):
		args = []string{"remove", name}
	case action == "update" && (name == "" || marketNameRe.MatchString(name)):
		args = []string{"update"}
		if name != "" {
			args = append(args, name)
		}
	default:
		return "", terr("srv.http_util.requeteInvalide")
	}
	output, err := runPlugin("", pluginCommandTimeout, append([]string{"marketplace"}, args...)...)
	plugCache.Lock()
	plugCache.raw = nil
	plugCache.Unlock()
	return strings.TrimSpace(string(output)), err
}
