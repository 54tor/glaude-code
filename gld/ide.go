package main

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strings"
	"unicode/utf8"
)

const ideMaxFile = 2 << 20

var ideMaxEntries = 5000

func ideEtag(fi fs.FileInfo) string { return fmt.Sprintf("%d-%d", fi.ModTime().UnixNano(), fi.Size()) }

type ideEntry struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

// ideResolve ouvre la racine puis valide le chemin ; l'appelant ferme root.
func ideResolve(r *http.Request, allowRoot bool) (*os.Root, string, fs.FileInfo, error) {
	root, err := ideOpenRoot(r.URL.Query().Get("dir"))
	if err != nil {
		return nil, "", nil, err
	}
	rel, err := ideRel(r.URL.Query().Get("path"), allowRoot)
	if err != nil {
		root.Close()
		return nil, "", nil, err
	}
	fi, err := ideLstat(root, rel)
	if err != nil {
		root.Close()
		return nil, "", nil, err
	}
	return root, rel, fi, nil
}

func routesIDE(mux *http.ServeMux) {
	routesIDEWrite(mux)
	routesIDEIndex(mux)
	mux.HandleFunc("GET /fs-tree", func(w http.ResponseWriter, r *http.Request) {
		root, rel, fi, err := ideResolve(r, true)
		if err != nil {
			ideFail(w, err)
			return
		}
		defer root.Close()
		if !fi.IsDir() {
			ideFail(w, errIdePath)
			return
		}
		d, err := root.Open(rel)
		if err != nil {
			ideFail(w, err)
			return
		}
		defer d.Close()
		des, err := d.ReadDir(-1)
		if err != nil {
			ideFail(w, err)
			return
		}
		entries := make([]ideEntry, 0, len(des))
		for _, de := range des {
			e := ideEntry{Name: de.Name(), Type: "file"}
			switch {
			case de.Type()&fs.ModeSymlink != 0:
				e.Type = "symlink"
			case de.IsDir():
				e.Type = "dir"
			}
			if info, err := de.Info(); err == nil {
				e.Size, e.Mtime = info.Size(), info.ModTime().Unix()
			}
			entries = append(entries, e)
		}
		sort.Slice(entries, func(i, j int) bool {
			a, b := entries[i], entries[j]
			if (a.Type == "dir") != (b.Type == "dir") {
				return a.Type == "dir"
			}
			la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
			if la != lb {
				return la < lb
			}
			return a.Name < b.Name
		})
		truncated := len(entries) > ideMaxEntries
		if truncated {
			entries = entries[:ideMaxEntries]
		}
		writeJSON(w, 200, map[string]any{"entries": entries, "truncated": truncated})
	})

	mux.HandleFunc("GET /fs-file", func(w http.ResponseWriter, r *http.Request) {
		root, rel, fi, err := ideResolve(r, false)
		if err != nil {
			ideFail(w, err)
			return
		}
		defer root.Close()
		if !fi.Mode().IsRegular() {
			ideFail(w, errIdePath)
			return
		}
		if fi.Size() > ideMaxFile {
			ideFail(w, errIdeTooBig)
			return
		}
		f, err := root.Open(rel)
		if err != nil {
			ideFail(w, err)
			return
		}
		defer f.Close()
		// Lstat puis Open est course-able : le descripteur ouvert doit être le même fichier.
		if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || !os.SameFile(fi, st) {
			ideFail(w, errIdeSymlink)
			return
		}
		b, err := io.ReadAll(io.LimitReader(f, ideMaxFile+1))
		if err != nil {
			ideFail(w, err)
			return
		}
		if len(b) > ideMaxFile {
			ideFail(w, errIdeTooBig)
			return
		}
		head := b
		if len(head) > 8192 {
			head = head[:8192]
		}
		if strings.IndexByte(string(head), 0) >= 0 || !utf8.Valid(b) {
			ideFail(w, errIdeBinary)
			return
		}
		writeJSON(w, 200, map[string]any{"content": string(b), "etag": ideEtag(fi), "size": int64(len(b)), "mtime": fi.ModTime().Unix()})
	})
}
