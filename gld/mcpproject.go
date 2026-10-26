package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type McpState struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	OK     bool   `json:"ok"`
}

var mcpCache = struct {
	sync.Mutex
	m map[string]struct {
		at  time.Time
		out []McpState
	}
}{m: map[string]struct {
	at  time.Time
	out []McpState
}{}}

// projectMcpStates : `claude mcp list` lancé dans le dossier du projet (serveurs du projet et personnels), gardé une minute par dossier.
var projectMcpStates = func(cwd string) ([]McpState, error) {
	mcpCache.Lock()
	defer mcpCache.Unlock()
	if c, ok := mcpCache.m[cwd]; ok && time.Since(c.at) < time.Minute {
		return c.out, nil
	}
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
	mcpCache.m[cwd] = struct {
		at  time.Time
		out []McpState
	}{time.Now(), out}
	return out, nil
}

func projectMcp(dir string) ([]McpState, error) {
	cwd := ""
	if safeDir(dir) {
		cwd = projectCwd(dir)
	}
	if fi, e := os.Stat(cwd); cwd == "" || e != nil || !fi.IsDir() {
		return nil, terr("srv.memory.dossierDuProjetIntrouvable")
	}
	return projectMcpStates(cwd)
}
