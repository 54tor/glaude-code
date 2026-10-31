package main

// Ce que Claude a réellement chargé : le hook InstructionsLoaded de Claude Code appelle `gld _loaded` à chaque
// chargement d'un CLAUDE.md, d'une règle ou d'un fichier importé, et on en garde la trace (une ligne JSON par
// chargement). On sait alors dans combien de sessions une règle est entrée, et quels fichiers une session a reçus.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type loadEvent struct {
	At      int64    `json:"at"`
	Session string   `json:"session"`
	File    string   `json:"file"`
	Type    string   `json:"type,omitempty"`   // User, Project, Local, Managed
	Reason  string   `json:"reason,omitempty"` // session_start, nested_traversal, path_glob_match, include, compact
	Globs   []string `json:"globs,omitempty"`
	Trigger string   `json:"trigger,omitempty"` // fichier dont l'accès a déclenché le chargement
}

const (
	loadedMax          = 4 << 20 // au-delà, on ne garde que les derniers 2 Mo
	loadedKeep         = loadedMax / 2
	loadedHookInputMax = 1 << 16
)

// loadedReadable : le chemin réel (liens suivis) d'un fichier chargé se lit seulement dans les .md de la config Claude
// ou sous un dossier connu de gld. Un @import de CLAUDE.md vers ~/.ssh/… figure dans le journal du hook sans en devenir lisible.
func loadedReadable(real string) bool {
	if resolvedConfig, err := filepath.EvalSymlinks(cfgDir()); err == nil && pathUnder(real, resolvedConfig) {
		return strings.EqualFold(filepath.Ext(real), ".md")
	}
	for ancestor := filepath.Dir(real); ancestor != filepath.Dir(ancestor); ancestor = filepath.Dir(ancestor) {
		if _, known := ideKnownDir(ancestor); known {
			return true
		}
	}
	return false
}

func loadedFile() string { return filepath.Join(cfgDir(), "gld-web-loaded.jsonl") }

// runLoaded lit l'entrée du hook (JSON sur stdin) et l'ajoute au journal. Jamais d'échec visible : un hook ne doit
// pas gêner une session.
func runLoaded(input io.Reader) int {
	var hookInput struct {
		Session string   `json:"session_id"`
		File    string   `json:"file_path"`
		Type    string   `json:"memory_type"`
		Reason  string   `json:"load_reason"`
		Globs   []string `json:"globs"`
		Trigger string   `json:"trigger_file_path"`
	}
	if json.NewDecoder(io.LimitReader(input, loadedHookInputMax)).Decode(&hookInput) != nil || hookInput.File == "" {
		return 0
	}
	line, _ := json.Marshal(loadEvent{
		At:      time.Now().Unix(),
		Session: hookInput.Session,
		File:    hookInput.File,
		Type:    hookInput.Type,
		Reason:  hookInput.Reason,
		Globs:   hookInput.Globs,
		Trigger: hookInput.Trigger,
	})
	appendLoaded(append(line, '\n'))
	return 0
}

func appendLoaded(line []byte) {
	logPath := loadedFile()
	if fileInfo, statErr := os.Stat(logPath); statErr == nil && fileInfo.Size() > loadedMax {
		if content, readErr := os.ReadFile(logPath); readErr == nil && len(content) > loadedKeep { // le fichier a pu raccourcir depuis le Stat
			content = content[len(content)-loadedKeep:]
			for byteIndex, byteValue := range content { // on repart au début d'une ligne
				if byteValue == '\n' {
					content = content[byteIndex+1:]
					break
				}
			}
			os.WriteFile(logPath, content, 0o600)
		}
	}
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	file.Write(line)
}

var (
	loadedMu    sync.Mutex
	loadedCache struct {
		size  int64
		mtime time.Time
		ev    []loadEvent
	}
)

// readLoaded : tous les chargements connus (relus seulement si le journal a changé).
func readLoaded() []loadEvent {
	loadedMu.Lock()
	defer loadedMu.Unlock()
	file, err := openTrusted(loadedFile())
	if err != nil {
		return nil
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() {
		return nil
	}
	if loadedCache.size == fileInfo.Size() && loadedCache.mtime.Equal(fileInfo.ModTime()) {
		return loadedCache.ev
	}
	content, err := io.ReadAll(io.LimitReader(file, loadedMax+1))
	if err != nil {
		return nil
	}
	var events []loadEvent
	for _, rawLine := range splitLines(content) {
		var event loadEvent
		if json.Unmarshal(rawLine, &event) == nil && event.File != "" {
			events = append(events, event)
		}
	}
	loadedCache.size, loadedCache.mtime, loadedCache.ev = fileInfo.Size(), fileInfo.ModTime(), events
	return events
}

func splitLines(content []byte) [][]byte {
	var lines [][]byte
	start := 0
	for byteIndex, byteValue := range content {
		if byteValue == '\n' {
			if byteIndex > start {
				lines = append(lines, content[start:byteIndex])
			}
			start = byteIndex + 1
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

// FileLoads : combien de sessions ont chargé ce fichier, et quand pour la dernière fois.
type FileLoads struct {
	Sessions int   `json:"sessions"`
	Last     int64 `json:"last"`
}

func loadsByFile(events []loadEvent) map[string]FileLoads {
	seen := map[string]map[string]bool{}
	byFile := map[string]FileLoads{}
	for _, event := range events {
		if seen[event.File] == nil {
			seen[event.File] = map[string]bool{}
		}
		seen[event.File][event.Session] = true
		loads := byFile[event.File]
		loads.Last = max(loads.Last, event.At)
		loads.Sessions = len(seen[event.File])
		byFile[event.File] = loads
	}
	return byFile
}

// loadsOfSession : les fichiers chargés par une session, du plus ancien au plus récent, avec leurs raisons.
func loadsOfSession(events []loadEvent, sessionID string) []loadEvent {
	var loads []loadEvent
	for _, event := range events {
		if event.Session == sessionID {
			loads = append(loads, event)
		}
	}
	sort.SliceStable(loads, func(leftIndex, rightIndex int) bool {
		return loads[leftIndex].At < loads[rightIndex].At
	})
	return loads
}
