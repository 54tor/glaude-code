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
func reapStages(room int) {
	ids := make([]string, 0, len(stages))
	for id, s := range stages {
		if time.Since(s.at) > stageTTL {
			os.RemoveAll(s.dir)
			delete(stages, id)
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return stages[ids[i]].at.Before(stages[ids[j]].at) })
	for len(ids) > stageMax-room {
		os.RemoveAll(stages[ids[0]].dir)
		delete(stages, ids[0])
		ids = ids[1:]
	}
}

func dropStage(id string) {
	stagesMu.Lock()
	defer stagesMu.Unlock()
	if s, ok := stages[id]; ok {
		os.RemoveAll(s.dir)
		delete(stages, id)
	}
}

// takeStage : revendique l'étape (retirée de la carte) pour que ni une autre application ni l'éviction ne touche à son dossier.
func takeStage(id string) (*stage, error) {
	stagesMu.Lock()
	defer stagesMu.Unlock()
	reapStages(0)
	s, ok := stages[id]
	if !ok {
		return nil, terr("srv.backup_import.etapeInconnue")
	}
	delete(stages, id)
	return s, nil
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

func stageBackup(b []byte) (BackupPlan, error) {
	dir, m, err := extractBackup(b)
	if err != nil {
		return BackupPlan{}, err
	}
	var raw [8]byte
	rand.Read(raw[:])
	id := hex.EncodeToString(raw[:])
	s := &stage{dir: dir, at: time.Now(), m: m}
	p := BackupPlan{ID: id, Manifest: m, Items: []SyncItem{}, Targets: liveProjects(), Skipped: []string{}}
	for _, pl := range backupPlanners {
		items, e := pl(s)
		if e != nil {
			os.RemoveAll(dir)
			return BackupPlan{}, e
		}
		p.Items = append(p.Items, items...)
	}
	p.Skipped = append(p.Skipped, skippedPlugins(s)...)
	stagesMu.Lock()
	reapStages(1)
	stages[id] = s
	stagesMu.Unlock()
	return p, nil
}

func applyBackup(id string, picks []SyncPick, targets map[string]string) ([]BackupResult, error) {
	s, err := takeStage(id)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(s.dir)
	res := []BackupResult{}
	owned := map[string]bool{}
	for _, a := range backupAppliers {
		var mine []SyncPick
		for _, p := range picks {
			if a.owns(p.ID) {
				mine = append(mine, p)
				owned[p.ID] = true
			}
		}
		if len(mine) > 0 {
			res = append(res, a.apply(s, mine, targets)...)
		}
	}
	for _, p := range picks {
		if !owned[p.ID] {
			res = append(res, BackupResult{ID: p.ID, Status: "skipped"})
		}
	}
	return res, nil
}

// --- réglages, fichiers, skills : le moteur de synchronisation, source = home/ de l'archive ---

func isHomeItem(id string) bool {
	for _, p := range []string{"set:", "perm:", "file:", "skill:"} {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

func homeCat(id string) string {
	switch {
	case strings.HasPrefix(id, "set:"), strings.HasPrefix(id, "perm:"):
		return "settings"
	case strings.HasPrefix(id, "skill:"), strings.HasPrefix(id, "file:commands/"):
		return "skills"
	}
	return "memory"
}

func planHomeItems(s *stage) ([]SyncItem, error) {
	src := filepath.Join(s.dir, "home")
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return nil, nil
	}
	items, err := syncPlanDirs(src, cfgDir(), true)
	for i := range items {
		items[i].Cat = homeCat(items[i].ID)
	}
	return items, err
}

func applyHomeItems(s *stage, picks []SyncPick, _ map[string]string) []BackupResult {
	_, err := syncApplyDirs(filepath.Join(s.dir, "home"), cfgDir(), picks, true)
	res := make([]BackupResult, len(picks))
	for i, p := range picks {
		res[i] = BackupResult{ID: p.ID, Status: "ok"}
		if err != nil {
			res[i] = BackupResult{ID: p.ID, Status: "failed", Message: err.Error()}
		}
	}
	return res
}

// --- mémoire et commandes de projet ---

func isProjectItem(id string) bool {
	return strings.HasPrefix(id, "projmem:") || strings.HasPrefix(id, "projcmd:")
}

// stagedProjectFiles : fichiers de l'archive d'un projet pour un sous-dossier (memory ou commands), chemin relatif en « / ».
func stagedProjectFiles(s *stage, key, sub string) map[string][]byte {
	out := map[string][]byte{}
	base := filepath.Join(s.dir, "projects", key, sub)
	filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if c, e := os.ReadFile(p); e == nil {
			rel, _ := filepath.Rel(base, p)
			out[filepath.ToSlash(rel)] = c
		}
		return nil
	})
	return out
}

func projectKeys(s *stage) []string {
	ents, _ := os.ReadDir(filepath.Join(s.dir, "projects"))
	var ks []string
	for _, e := range ents {
		if e.IsDir() && safeDir(e.Name()) {
			ks = append(ks, e.Name())
		}
	}
	return ks
}

// defaultTarget : le projet de même clé sur cette machine, s'il existe.
func defaultTarget(key string) string {
	if safeDir(key) && isDirPath(filepath.Join(root, key)) {
		return key
	}
	return ""
}

func projectItem(kind, key string, files map[string][]byte, cur func(target, rel string) (string, bool)) SyncItem {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	t := defaultTarget(key)
	st := "same"
	for _, n := range names {
		c, ok := "", false
		if t != "" {
			c, ok = cur(t, n)
		}
		if !ok {
			st = "new"
			break
		}
		if c != string(files[n]) {
			st = "diff"
		}
	}
	cat := "memory"
	if kind == "projcmd" {
		cat = "skills"
	}
	return SyncItem{ID: kind + ":" + key, Kind: kind, Label: key, Status: st, Cat: cat, From: strings.Join(names, ", "), To: t}
}

func planProjectItems(s *stage) ([]SyncItem, error) {
	var items []SyncItem
	for _, k := range projectKeys(s) {
		if f := stagedProjectFiles(s, k, "memory"); len(f) > 0 {
			items = append(items, projectItem("projmem", k, f, func(t, rel string) (string, bool) {
				c, err := memRead("m/" + t + "/" + rel)
				return c, err == nil
			}))
		}
		if f := stagedProjectFiles(s, k, "commands"); len(f) > 0 {
			items = append(items, projectItem("projcmd", k, f, func(t, rel string) (string, bool) {
				c, err := readSnippet("p/" + t + "/" + rel)
				return c, err == nil
			}))
		}
	}
	return items, nil
}

func applyProjectItems(s *stage, picks []SyncPick, targets map[string]string) []BackupResult {
	res := make([]BackupResult, 0, len(picks))
	for _, p := range picks {
		kind, key, _ := strings.Cut(p.ID, ":")
		r := BackupResult{ID: p.ID, Status: "ok"}
		t, ok := targets[key]
		if !ok {
			t = defaultTarget(key)
		}
		if !safeDir(key) || !safeDir(t) || !isDirPath(filepath.Join(root, t)) {
			res = append(res, BackupResult{ID: p.ID, Status: "skipped"})
			continue
		}
		var err error
		if kind == "projmem" {
			for n, c := range stagedProjectFiles(s, key, "memory") {
				if err = memSave("m/"+t+"/"+n, string(c)); err != nil {
					break
				}
			}
		} else {
			var fl []ImportFile
			for n, c := range stagedProjectFiles(s, key, "commands") {
				fl = append(fl, ImportFile{n, c})
			}
			_, _, err = importSnippets(t, fl, true)
		}
		if err != nil {
			r = BackupResult{ID: p.ID, Status: "failed", Message: err.Error()}
		}
		res = append(res, r)
	}
	return res
}
