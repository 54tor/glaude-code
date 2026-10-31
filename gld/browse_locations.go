package main

import (
	"os"
	"path/filepath"
	"strings"
)

var browseRoots []string

func setBrowseLocations(list string) {
	browseRoots = nil
	if strings.TrimSpace(list) == "" {
		return
	}
	home, _ := os.UserHomeDir()
	candidates := strings.Split(list, ",")
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "~" || strings.HasPrefix(candidate, "~/") {
			candidate = filepath.Join(home, strings.TrimPrefix(candidate, "~"))
		}
		if !filepath.IsAbs(candidate) {
			continue
		}
		candidate = filepath.Clean(candidate)
		if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
			candidate = resolved
		}
		browseRoots = append(browseRoots, candidate)
	}
}

// browseAllowed : sans -locations tout est permis ; sinon le chemin (liens résolus) doit être sous une racine.
func browseAllowed(path string) bool {
	if len(browseRoots) == 0 {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	for _, browseRoot := range browseRoots {
		if pathUnder(path, browseRoot) {
			return true
		}
	}
	return false
}

func browseIsRoot(path string) bool {
	if len(browseRoots) == 0 {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	for _, browseRoot := range browseRoots {
		if path == browseRoot {
			return true
		}
	}
	return false
}
