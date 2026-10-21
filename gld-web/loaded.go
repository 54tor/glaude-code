package main

// Ce que Claude a réellement chargé : le hook InstructionsLoaded de Claude Code appelle `gld-web _loaded` à chaque
// chargement d'un CLAUDE.md, d'une règle ou d'un fichier importé, et on en garde la trace (une ligne JSON par
// chargement). On sait alors dans combien de sessions une règle est entrée, et quels fichiers une session a reçus.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
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

const loadedMax = 4 << 20 // au-delà, on ne garde que les derniers 2 Mo

func loadedFile() string { return filepath.Join(cfgDir(), "gld-web-loaded.jsonl") }

// runLoaded lit l'entrée du hook (JSON sur stdin) et l'ajoute au journal. Jamais d'échec visible : un hook ne doit
// pas gêner une session.
func runLoaded(in io.Reader) int {
	var h struct {
		Session string   `json:"session_id"`
		File    string   `json:"file_path"`
		Type    string   `json:"memory_type"`
		Reason  string   `json:"load_reason"`
		Globs   []string `json:"globs"`
		Trigger string   `json:"trigger_file_path"`
	}
	if json.NewDecoder(io.LimitReader(in, 1<<16)).Decode(&h) != nil || h.File == "" {
		return 0
	}
	line, _ := json.Marshal(loadEvent{time.Now().Unix(), h.Session, h.File, h.Type, h.Reason, h.Globs, h.Trigger})
	appendLoaded(append(line, '\n'))
	return 0
}

func appendLoaded(line []byte) {
	p := loadedFile()
	if st, err := os.Stat(p); err == nil && st.Size() > loadedMax {
		if b, err := os.ReadFile(p); err == nil {
			b = b[len(b)-loadedMax/2:]
			for i, c := range b { // on repart au début d'une ligne
				if c == '\n' {
					b = b[i+1:]
					break
				}
			}
			os.WriteFile(p, b, 0o600)
		}
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(line)
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
	st, err := os.Stat(loadedFile())
	if err != nil {
		return nil
	}
	if loadedCache.size == st.Size() && loadedCache.mtime.Equal(st.ModTime()) {
		return loadedCache.ev
	}
	b, err := os.ReadFile(loadedFile())
	if err != nil {
		return nil
	}
	var ev []loadEvent
	for _, l := range splitLines(b) {
		var e loadEvent
		if json.Unmarshal(l, &e) == nil && e.File != "" {
			ev = append(ev, e)
		}
	}
	loadedCache.size, loadedCache.mtime, loadedCache.ev = st.Size(), st.ModTime(), ev
	return ev
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// FileLoads : combien de sessions ont chargé ce fichier, et quand pour la dernière fois.
type FileLoads struct {
	Sessions int   `json:"sessions"`
	Last     int64 `json:"last"`
}

func loadsByFile(ev []loadEvent) map[string]FileLoads {
	seen := map[string]map[string]bool{}
	out := map[string]FileLoads{}
	for _, e := range ev {
		if seen[e.File] == nil {
			seen[e.File] = map[string]bool{}
		}
		seen[e.File][e.Session] = true
		o := out[e.File]
		o.Last = max(o.Last, e.At)
		o.Sessions = len(seen[e.File])
		out[e.File] = o
	}
	return out
}

// loadsOfSession : les fichiers chargés par une session, du plus ancien au plus récent, avec leurs raisons.
func loadsOfSession(ev []loadEvent, id string) []loadEvent {
	var out []loadEvent
	for _, e := range ev {
		if e.Session == id {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}
