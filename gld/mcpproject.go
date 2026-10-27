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
	c, ok := mcpCache.m[cwd]
	mcpCache.Unlock()
	if ok && time.Since(c.at) < time.Minute {
		return c.out, c.err
	}
	v, _, _ := mcpFlight.Do(cwd, func() (any, error) {
		out, err := mcpList(cwd)
		e := mcpEntry{time.Now(), out, err}
		mcpCache.Lock()
		mcpCache.m[cwd] = e
		mcpCache.Unlock()
		return e, nil
	})
	e := v.(mcpEntry)
	return e.out, e.err
}

func mcpList(cwd string) ([]McpState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := withPath(exec.CommandContext(ctx, "claude", "mcp", "list"))
	cmd.Dir = cwd
	b, err := cmd.Output()
	if err != nil && len(b) == 0 {
		return nil, err
	}
	out := []McpState{}
	for _, l := range strings.Split(string(b), "\n") {
		if m := mcpLine.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			st := strings.TrimSpace(m[3])
			out = append(out, McpState{m[1], st, strings.Contains(st, "Connected")})
		}
	}
	return out, nil
}

// projectTrusted : Claude Code a déjà accepté la confiance de ce dossier (ou d'un parent) dans ~/.claude.json. `claude mcp list` démarre les
// serveurs du .mcp.json du projet sans demander : on ne le lance donc jamais dans un dossier que l'utilisateur n'a pas approuvé.
func projectTrusted(cwd string) bool {
	b, err := os.ReadFile(claudeJSON())
	if err != nil {
		return false
	}
	var d struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(b, &d) != nil {
		return false
	}
	for p := filepath.Clean(cwd); ; p = filepath.Dir(p) {
		if d.Projects[p].Trusted {
			return true
		}
		if p == filepath.Dir(p) {
			return false
		}
	}
}

func projectMcp(dir string) ([]McpState, error) {
	cwd := ""
	if safeDir(dir) {
		cwd = projectCwd(dir)
	}
	if fi, e := os.Stat(cwd); cwd == "" || e != nil || !fi.IsDir() {
		return nil, terr("srv.memory.dossierDuProjetIntrouvable")
	}
	if !projectTrusted(cwd) {
		return nil, terr("srv.mcpproject.nonApprouve")
	}
	return projectMcpStates(cwd)
}
