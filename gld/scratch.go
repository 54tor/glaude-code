package main

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

type scratchOrphan struct {
	base, path string
	kb         int64
}

func dirSizeKB(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total / 1024
}

// scratchBases : racines temporaires réelles (ni lien symbolique, ni dossier d'un autre compte).
func scratchBases() []string {
	candidates, _ := scratchDirs("", "")
	var bases []string
	for _, base := range candidates {
		info, err := os.Lstat(base)
		if err == nil && info.IsDir() && ownedByMe(info) {
			bases = append(bases, base)
		}
	}
	return bases
}

// scratchOrphans : dossiers temporaires <base>/<projet>/<session> dont le transcript n'existe plus et dont la session n'est pas vivante.
func scratchOrphans() ([]scratchOrphan, error) {
	live, err := liveCheck()
	if err != nil {
		return nil, err
	}
	var orphans []scratchOrphan
	for _, base := range scratchBases() {
		projectEntries, _ := os.ReadDir(base)
		for _, projectEntry := range projectEntries {
			sessionEntries, _ := os.ReadDir(filepath.Join(base, projectEntry.Name()))
			for _, sessionEntry := range sessionEntries {
				sessionID := sessionEntry.Name()
				sessionInfo, infoErr := sessionEntry.Info()
				if infoErr != nil || !sessionEntry.IsDir() || !ownedByMe(sessionInfo) || !idRe.MatchString(sessionID) || live[sessionID] {
					continue
				}
				if _, statErr := os.Stat(filepath.Join(root, projectEntry.Name(), sessionID+".jsonl")); statErr == nil {
					continue
				}
				path := filepath.Join(base, projectEntry.Name(), sessionID)
				orphans = append(orphans, scratchOrphan{base: base, path: path, kb: dirSizeKB(path)})
			}
		}
	}
	return orphans, nil
}

func routesScratch(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/scratch", func(responseWriter http.ResponseWriter, request *http.Request) {
		orphans, err := scratchOrphans()
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		var totalKB int64
		for _, orphan := range orphans {
			totalKB += orphan.kb
		}
		writeJSON(responseWriter, 200, map[string]any{"count": len(orphans), "kb": totalKB})
	})
	mux.HandleFunc("DELETE /v1/scratch", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionsMu.Lock()
		defer sessionsMu.Unlock()
		orphans, err := scratchOrphans()
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		var removedCount int
		var freedKB int64
		for _, orphan := range orphans {
			if rRemoveAll(orphan.base, orphan.path) == nil {
				removedCount++
				freedKB += orphan.kb
			}
		}
		journal("scratch-purge : %d dossier(s), %d Ko", removedCount, freedKB)
		writeJSON(responseWriter, 200, map[string]any{"count": removedCount, "kb": freedKB})
	})
}
