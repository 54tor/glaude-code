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
	// ideDeadlineCheckEvery : pendant le parcours, l'échéance n'est consultée qu'une entrée sur ce nombre.
	ideDeadlineCheckEvery = 256
	ideFilesBodyMax       = 64 << 10
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

var ideIndexLimits = ideLimits{Entries: 20000, Depth: 12, Time: 5 * time.Second}

// ideBuildHook permet aux tests de compter les constructions.
var ideBuildHook = func() {}

func ideIgnored(name string) bool {
	return strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules"
}

func ideBuildIndex(root *os.Root, limits ideLimits) ([]ideIndexEntry, bool) {
	entries, _, partial := ideWalk(root, limits)
	return entries, partial
}

// ideWalk parcourt l'arbre sans lire le contenu des fichiers ni suivre de lien ; une borne
// atteinte rend ce qui est déjà collecté avec partial=true. dirs sert à la veille du disque.
func ideWalk(root *os.Root, limits ideLimits) (entries []ideIndexEntry, dirs []string, partial bool) {
	type item struct {
		dir   string
		depth int
	}
	deadline := time.Now().Add(limits.Time)
	stack := []item{{".", 0}}
	seen := 0
walk:
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if time.Now().After(deadline) {
			partial = true
			break
		}
		directory, err := root.Open(current.dir)
		if err != nil {
			continue
		}
		dirEntries, err := directory.ReadDir(-1)
		directory.Close()
		if err != nil {
			continue
		}
		dirs = append(dirs, current.dir)
		for _, dirEntry := range dirEntries {
			// Un seul dossier énorme dépasserait sinon la borne de temps.
			seen++
			if seen%ideDeadlineCheckEvery == 0 && time.Now().After(deadline) {
				partial = true
				break walk
			}
			name := dirEntry.Name()
			if ideIgnored(name) {
				continue
			}
			rel := name
			if current.dir != "." {
				rel = current.dir + "/" + name
			}
			switch {
			case dirEntry.Type()&fs.ModeSymlink != 0:
			case dirEntry.IsDir():
				if current.depth+1 > limits.Depth {
					partial = true
					continue
				}
				stack = append(stack, item{rel, current.depth + 1})
			case dirEntry.Type().IsRegular():
				if len(entries) >= limits.Entries {
					partial = true
					break walk
				}
				entry := ideIndexEntry{Path: rel}
				if info, infoErr := dirEntry.Info(); infoErr == nil {
					entry.Size, entry.Mtime = info.Size(), info.ModTime().Unix()
				}
				entries = append(entries, entry)
			}
		}
	}
	sort.Slice(entries, func(leftIndex, rightIndex int) bool {
		return entries[leftIndex].Path < entries[rightIndex].Path
	})
	return entries, dirs, partial
}

// ideIdx est immuable une fois publié : la veille en publie une copie à chaque changement.
type ideIdx struct {
	state   string
	entries []ideIndexEntry
	set     map[string]bool
}

func ideIndexGet(dir string) ideIdx {
	workspace, err := ideWS(dir)
	if err != nil {
		return ideIdx{state: "partial"}
	}
	return workspace.idx()
}

func routesIDEIndex(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/fs/index", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir, known := ideKnownDir(request.URL.Query().Get("dir"))
		if !known {
			ideFail(responseWriter, errIdeDir)
			return
		}
		index := ideIndexGet(dir)
		files := index.entries
		if files == nil {
			files = []ideIndexEntry{}
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"state": index.state, "files": files})
	})

	mux.HandleFunc("POST /v1/fs/files", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Paths []string `json:"paths"`
		}
		if !ideBody(responseWriter, request, ideFilesBodyMax, &body) {
			return
		}
		dir := request.URL.Query().Get("dir")
		root, err := ideOpenRoot(dir)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer root.Close()
		type file struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		files := []file{}
		index := ideIndexGet(dir)
		if len(body.Paths) > ideFeedPaths {
			body.Paths = body.Paths[:ideFeedPaths]
		}
		for _, path := range body.Paths {
			if !index.set[path] {
				continue
			}
			if content, readable := ideFeedRead(root, path); readable {
				files = append(files, file{path, content})
			}
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"files": files})
	})
}

func ideFeedRead(root *os.Root, rel string) (string, bool) {
	fileInfo, err := ideLstat(root, rel)
	if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() > ideFeedMax {
		return "", false
	}
	file, err := root.Open(rel)
	if err != nil {
		return "", false
	}
	defer file.Close()
	if openedInfo, statErr := file.Stat(); statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(fileInfo, openedInfo) {
		return "", false
	}
	content, err := io.ReadAll(io.LimitReader(file, ideFeedMax+1))
	isText := strings.IndexByte(string(content), 0) < 0 && utf8.Valid(content)
	if err != nil || len(content) > ideFeedMax || !isText {
		return "", false
	}
	return string(content), true
}
