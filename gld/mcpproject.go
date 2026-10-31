package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type McpState struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	OK     bool   `json:"ok"`
}

const mcpCacheTTL = time.Minute

type mcpEntry struct {
	at  time.Time
	out []McpState
	err error
}

var (
	mcpCache = struct {
		sync.Mutex
		m map[string]mcpEntry
	}{m: map[string]mcpEntry{}}
	mcpFlight singleflight.Group // un seul `claude mcp list` par dossier à la fois, sans faire attendre les autres dossiers
)

// projectMcpStates : `claude mcp list` lancé dans le dossier du projet (serveurs du projet et personnels), gardé une minute par
// dossier, échec compris : chaque appel démarre les serveurs MCP du projet.
var projectMcpStates = func(cwd string) ([]McpState, error) {
	mcpCache.Lock()
	cached, found := mcpCache.m[cwd]
	mcpCache.Unlock()
	if found && time.Since(cached.at) < mcpCacheTTL {
		return cached.out, cached.err
	}
	result, _, _ := mcpFlight.Do(cwd, func() (any, error) {
		states, listErr := mcpList(cwd)
		fresh := mcpEntry{at: time.Now(), out: states, err: listErr}
		mcpCache.Lock()
		mcpCache.m[cwd] = fresh
		mcpCache.Unlock()
		return fresh, nil
	})
	entry := result.(mcpEntry)
	return entry.out, entry.err
}

func mcpList(cwd string) ([]McpState, error) {
	commandContext, cancel := context.WithTimeout(context.Background(), mcpListTimeout)
	defer cancel()
	cmd := withPath(exec.CommandContext(commandContext, "claude", "mcp", "list"))
	cmd.Dir = cwd
	output, err := cmd.Output()
	if err != nil && len(output) == 0 {
		return nil, err
	}
	states := []McpState{}
	for _, outputLine := range strings.Split(string(output), "\n") {
		if match := mcpLine.FindStringSubmatch(strings.TrimSpace(outputLine)); match != nil {
			status := strings.TrimSpace(match[3])
			states = append(states, McpState{match[1], status, strings.Contains(status, "Connected")})
		}
	}
	return states, nil
}

// projectTrusted : Claude Code a déjà accepté la confiance de ce dossier (ou d'un parent) dans ~/.claude.json. `claude mcp list` démarre les
// serveurs du .mcp.json du projet sans demander : on ne le lance donc jamais dans un dossier que l'utilisateur n'a pas approuvé.
func projectTrusted(cwd string) bool {
	content, err := readTrusted(claudeJSON())
	if err != nil {
		return false
	}
	var config struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(content, &config) != nil {
		return false
	}
	for candidateDir := filepath.Clean(cwd); ; candidateDir = filepath.Dir(candidateDir) {
		if config.Projects[candidateDir].Trusted {
			return true
		}
		if candidateDir == filepath.Dir(candidateDir) {
			return false
		}
	}
}

func projectMcp(dir string) ([]McpState, error) {
	cwd := ""
	if safeDir(dir) {
		cwd = projectCwd(dir)
	}
	if fileInfo, statErr := os.Stat(cwd); cwd == "" || statErr != nil || !fileInfo.IsDir() {
		return nil, terr("srv.memory.dossierDuProjetIntrouvable")
	}
	if !projectTrusted(cwd) {
		return nil, terr("srv.mcpproject.nonApprouve")
	}
	return projectMcpStates(cwd)
}
