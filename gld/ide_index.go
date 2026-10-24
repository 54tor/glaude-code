package main

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ideFeedMax   = 100 << 10
	ideFeedPaths = 300
)

type ideIndexEntry struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

type ideLimits struct {
	Entries, Depth int
	Time           time.Duration
}

var ideIndexLimits = ideLimits{20000, 12, 5 * time.Second}

// ideBuildHook permet aux tests de compter les constructions.
var ideBuildHook = func() {}

func ideIgnored(name string) bool {
	return strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules"
}

func ideBuildIndex(root *os.Root, lim ideLimits) ([]ideIndexEntry, bool) {
	es, _, partial := ideWalk(root, lim)
	return es, partial
}

// ideWalk parcourt l'arbre sans lire le contenu des fichiers ni suivre de lien ; une borne
// atteinte rend ce qui est déjà collecté avec partial=true. dirs sert à la veille du disque.
func ideWalk(root *os.Root, lim ideLimits) (entries []ideIndexEntry, dirs []string, partial bool) {
	type item struct {
		dir   string
		depth int
	}
	deadline := time.Now().Add(lim.Time)
	stack := []item{{".", 0}}
	seen := 0
walk:
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if time.Now().After(deadline) {
			partial = true
			break
		}
		d, err := root.Open(it.dir)
		if err != nil {
			continue
		}
		des, err := d.ReadDir(-1)
		d.Close()
		if err != nil {
			continue
		}
		dirs = append(dirs, it.dir)
		for _, de := range des {
			// Un seul dossier énorme dépasserait sinon la borne de temps.
			if seen++; seen%256 == 0 && time.Now().After(deadline) {
				partial = true
				break walk
			}
			name := de.Name()
			if ideIgnored(name) {
				continue
			}
			rel := name
			if it.dir != "." {
				rel = it.dir + "/" + name
			}
			switch {
			case de.Type()&fs.ModeSymlink != 0:
			case de.IsDir():
				if it.depth+1 > lim.Depth {
					partial = true
					continue
				}
				stack = append(stack, item{rel, it.depth + 1})
			case de.Type().IsRegular():
				if len(entries) >= lim.Entries {
					partial = true
					break walk
				}
				e := ideIndexEntry{Path: rel}
				if info, err := de.Info(); err == nil {
					e.Size, e.Mtime = info.Size(), info.ModTime().Unix()
				}
				entries = append(entries, e)
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, dirs, partial
}

// ideIdx est immuable une fois publié : la veille en publie une copie à chaque changement.
type ideIdx struct {
	state   string
	entries []ideIndexEntry
	set     map[string]bool
}

func ideIndexGet(dir string) ideIdx {
	ws, err := ideWS(dir)
	if err != nil {
		return ideIdx{state: "partial"}
	}
	return ws.idx()
}

func routesIDEIndex(mux *http.ServeMux) {
	mux.HandleFunc("GET /fs-index", func(w http.ResponseWriter, r *http.Request) {
		dir, ok := ideKnownDir(r.URL.Query().Get("dir"))
		if !ok {
			ideFail(w, errIdeDir)
			return
		}
		ix := ideIndexGet(dir)
		files := ix.entries
		if files == nil {
			files = []ideIndexEntry{}
		}
		writeJSON(w, 200, map[string]any{"state": ix.state, "files": files})
	})

	mux.HandleFunc("POST /fs-files", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Dir   string   `json:"dir"`
			Paths []string `json:"paths"`
		}
		if !ideBody(w, r, 64<<10, &in) {
			return
		}
		root, err := ideOpenRoot(in.Dir)
		if err != nil {
			ideFail(w, err)
			return
		}
		defer root.Close()
		type file struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		out := []file{}
		ix := ideIndexGet(in.Dir)
		if len(in.Paths) > ideFeedPaths {
			in.Paths = in.Paths[:ideFeedPaths]
		}
		for _, p := range in.Paths {
			if !ix.set[p] {
				continue
			}
			if c, ok := ideFeedRead(root, p); ok {
				out = append(out, file{p, c})
			}
		}
		writeJSON(w, 200, map[string]any{"files": out})
	})
}

func ideFeedRead(root *os.Root, rel string) (string, bool) {
	fi, err := ideLstat(root, rel)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > ideFeedMax {
		return "", false
	}
	f, err := root.Open(rel)
	if err != nil {
		return "", false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || !os.SameFile(fi, st) {
		return "", false
	}
	b, err := io.ReadAll(io.LimitReader(f, ideFeedMax+1))
	if err != nil || len(b) > ideFeedMax || strings.IndexByte(string(b), 0) >= 0 || !utf8.Valid(b) {
		return "", false
	}
	return string(b), true
}
