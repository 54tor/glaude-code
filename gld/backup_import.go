package main

import (
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	stageTTL = 30 * time.Minute
	stageMax = 4

	stageIDBytes = 8
)

type BackupPlan struct {
	ID       string         `json:"id"`
	Manifest BackupManifest `json:"manifest"`
	Items    []SyncItem     `json:"items"`
	Targets  []MemTarget    `json:"targets"`
	Skipped  []string       `json:"skipped"`
}

type BackupResult struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // ok, failed, skipped
	Message string `json:"message,omitempty"`
}

type stage struct {
	dir string
	at  time.Time
	m   BackupManifest
}

var (
	stagesMu sync.Mutex
	stages   = map[string]*stage{}
)

// reapStages : à appeler sous stagesMu. Supprime les étapes expirées puis, au-delà de stageMax, les plus anciennes.
func reapStages(freeSlots int) {
	stageIDs := make([]string, 0, len(stages))
	for stageID, archiveStage := range stages {
		if time.Since(archiveStage.at) > stageTTL {
			os.RemoveAll(archiveStage.dir)
			delete(stages, stageID)
			continue
		}
		stageIDs = append(stageIDs, stageID)
	}
	sort.Slice(stageIDs, func(left, right int) bool {
		return stages[stageIDs[left]].at.Before(stages[stageIDs[right]].at)
	})
	for len(stageIDs) > stageMax-freeSlots {
		os.RemoveAll(stages[stageIDs[0]].dir)
		delete(stages, stageIDs[0])
		stageIDs = stageIDs[1:]
	}
}

func dropStage(stageID string) {
	stagesMu.Lock()
	defer stagesMu.Unlock()
	if archiveStage, found := stages[stageID]; found {
		os.RemoveAll(archiveStage.dir)
		delete(stages, stageID)
	}
}

// takeStage : revendique l'étape (retirée de la carte) pour que ni une autre application ni l'éviction ne touche à son dossier.
func takeStage(stageID string) (*stage, error) {
	stagesMu.Lock()
	defer stagesMu.Unlock()
	reapStages(0)
	archiveStage, found := stages[stageID]
	if !found {
		return nil, terr("srv.backup_import.etapeInconnue")
	}
	delete(stages, stageID)
	return archiveStage, nil
}

// backupPlanner / backupApplier : un couple par famille d'éléments, pour que les tâches suivantes en ajoutent sans toucher au reste.
type backupPlanner func(s *stage) ([]SyncItem, error)

type backupApplier struct {
	owns  func(id string) bool
	apply func(s *stage, picks []SyncPick, targets map[string]string) []BackupResult
}

var (
	// ordre d'application : fichiers, marketplaces, plugins, MCP, projets
	backupPlanners = []backupPlanner{planHomeItems, planMarkets, planPlugins, planMcp, planProjectItems}
	backupAppliers = []backupApplier{
		{isHomeItem, applyHomeItems},
		{func(id string) bool { return strings.HasPrefix(id, "market:") }, applyMarkets},
		{func(id string) bool { return strings.HasPrefix(id, "plugin:") }, applyPlugins},
		{func(id string) bool { return strings.HasPrefix(id, "mcp:") }, applyMcp},
		{isProjectItem, applyProjectItems},
	}
)

func stageBackup(archive []byte) (BackupPlan, error) {
	dir, manifest, err := extractBackup(archive)
	if err != nil {
		return BackupPlan{}, err
	}
	var randomBytes [stageIDBytes]byte
	rand.Read(randomBytes[:])
	stageID := hex.EncodeToString(randomBytes[:])
	archiveStage := &stage{dir: dir, at: time.Now(), m: manifest}
	plan := BackupPlan{ID: stageID, Manifest: manifest, Items: []SyncItem{}, Targets: liveProjects(), Skipped: []string{}}
	for _, planner := range backupPlanners {
		items, planErr := planner(archiveStage)
		if planErr != nil {
			os.RemoveAll(dir)
			return BackupPlan{}, planErr
		}
		plan.Items = append(plan.Items, items...)
	}
	plan.Skipped = append(plan.Skipped, skippedPlugins(archiveStage)...)
	stagesMu.Lock()
	reapStages(1)
	stages[stageID] = archiveStage
	stagesMu.Unlock()
	return plan, nil
}

func applyBackup(stageID string, picks []SyncPick, targets map[string]string) ([]BackupResult, error) {
	archiveStage, err := takeStage(stageID)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(archiveStage.dir)
	results := []BackupResult{}
	owned := map[string]bool{}
	for _, applier := range backupAppliers {
		var applierPicks []SyncPick
		for _, pick := range picks {
			if applier.owns(pick.ID) {
				applierPicks = append(applierPicks, pick)
				owned[pick.ID] = true
			}
		}
		if len(applierPicks) > 0 {
			results = append(results, applier.apply(archiveStage, applierPicks, targets)...)
		}
	}
	for _, pick := range picks {
		if !owned[pick.ID] {
			results = append(results, BackupResult{ID: pick.ID, Status: "skipped"})
		}
	}
	return results, nil
}

// --- réglages, fichiers, skills : le moteur de synchronisation, source = home/ de l'archive ---

func isHomeItem(itemID string) bool {
	for _, prefix := range []string{"set:", "perm:", "file:", "skill:"} {
		if strings.HasPrefix(itemID, prefix) {
			return true
		}
	}
	return false
}

func homeCat(itemID string) string {
	switch {
	case strings.HasPrefix(itemID, "set:"), strings.HasPrefix(itemID, "perm:"):
		return "settings"
	case strings.HasPrefix(itemID, "skill:"), strings.HasPrefix(itemID, "file:commands/"):
		return "skills"
	}
	return "memory"
}

func planHomeItems(archiveStage *stage) ([]SyncItem, error) {
	homeDir := filepath.Join(archiveStage.dir, "home")
	if fileInfo, statErr := os.Stat(homeDir); statErr != nil || !fileInfo.IsDir() {
		return nil, nil
	}
	items, err := syncPlanDirs(homeDir, cfgDir(), true)
	for itemIndex := range items {
		items[itemIndex].Cat = homeCat(items[itemIndex].ID)
	}
	return items, err
}

func applyHomeItems(archiveStage *stage, picks []SyncPick, _ map[string]string) []BackupResult {
	_, err := syncApplyDirs(filepath.Join(archiveStage.dir, "home"), cfgDir(), picks, true)
	results := make([]BackupResult, len(picks))
	for pickIndex, pick := range picks {
		results[pickIndex] = BackupResult{ID: pick.ID, Status: "ok"}
		if err != nil {
			results[pickIndex] = BackupResult{ID: pick.ID, Status: "failed", Message: err.Error()}
		}
	}
	return results
}

// --- mémoire et commandes de projet ---

func isProjectItem(itemID string) bool {
	return strings.HasPrefix(itemID, "projmem:") || strings.HasPrefix(itemID, "projcmd:")
}

// stagedProjectFiles : fichiers de l'archive d'un projet pour un sous-dossier (memory ou commands), chemin relatif en « / ».
func stagedProjectFiles(archiveStage *stage, key, subDir string) map[string][]byte {
	filesByPath := map[string][]byte{}
	baseDir := filepath.Join(archiveStage.dir, "projects", key, subDir)
	filepath.WalkDir(baseDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		if content, readErr := os.ReadFile(path); readErr == nil {
			rel, _ := filepath.Rel(baseDir, path)
			filesByPath[filepath.ToSlash(rel)] = content
		}
		return nil
	})
	return filesByPath
}

func projectKeys(archiveStage *stage) []string {
	entries, _ := os.ReadDir(filepath.Join(archiveStage.dir, "projects"))
	var keys []string
	for _, entry := range entries {
		if entry.IsDir() && safeDir(entry.Name()) {
			keys = append(keys, entry.Name())
		}
	}
	return keys
}

// defaultTarget : le projet de même clé sur cette machine, s'il existe.
func defaultTarget(key string) string {
	if safeDir(key) && isDirPath(filepath.Join(root, key)) {
		return key
	}
	return ""
}

func projectItem(kind, key string, files map[string][]byte, readCurrent func(target, rel string) (string, bool)) SyncItem {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	targetProject := defaultTarget(key)
	status := "same"
	for _, name := range names {
		currentContent, exists := "", false
		if targetProject != "" {
			currentContent, exists = readCurrent(targetProject, name)
		}
		if !exists {
			status = "new"
			break
		}
		if currentContent != string(files[name]) {
			status = "diff"
		}
	}
	category := "memory"
	if kind == "projcmd" {
		category = "skills"
	}
	return SyncItem{ID: kind + ":" + key, Kind: kind, Label: key, Status: status, Cat: category, From: strings.Join(names, ", "), To: targetProject}
}

func planProjectItems(archiveStage *stage) ([]SyncItem, error) {
	var items []SyncItem
	for _, key := range projectKeys(archiveStage) {
		if files := stagedProjectFiles(archiveStage, key, "memory"); len(files) > 0 {
			items = append(items, projectItem("projmem", key, files, func(targetProject, rel string) (string, bool) {
				content, err := memRead("m/" + targetProject + "/" + rel)
				return content, err == nil
			}))
		}
		if files := stagedProjectFiles(archiveStage, key, "commands"); len(files) > 0 {
			items = append(items, projectItem("projcmd", key, files, func(targetProject, rel string) (string, bool) {
				content, err := readSnippet("p/" + targetProject + "/" + rel)
				return content, err == nil
			}))
		}
	}
	return items, nil
}

func applyProjectItems(archiveStage *stage, picks []SyncPick, targets map[string]string) []BackupResult {
	results := make([]BackupResult, 0, len(picks))
	for _, pick := range picks {
		kind, key, _ := strings.Cut(pick.ID, ":")
		result := BackupResult{ID: pick.ID, Status: "ok"}
		targetProject, hasTarget := targets[key]
		if !hasTarget {
			targetProject = defaultTarget(key)
		}
		targetIsValid := safeDir(key) && safeDir(targetProject) && isDirPath(filepath.Join(root, targetProject))
		if !targetIsValid {
			results = append(results, BackupResult{ID: pick.ID, Status: "skipped"})
			continue
		}
		var err error
		if kind == "projmem" {
			for name, content := range stagedProjectFiles(archiveStage, key, "memory") {
				if err = memSave("m/"+targetProject+"/"+name, string(content)); err != nil {
					break
				}
			}
		} else {
			var importFiles []ImportFile
			for name, content := range stagedProjectFiles(archiveStage, key, "commands") {
				importFiles = append(importFiles, ImportFile{name, content})
			}
			_, _, err = importSnippets(targetProject, importFiles, true)
		}
		if err != nil {
			result = BackupResult{ID: pick.ID, Status: "failed", Message: err.Error()}
		}
		results = append(results, result)
	}
	return results
}
