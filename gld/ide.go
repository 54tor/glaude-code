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

const (
	ideMaxFile = 2 << 20
	// ideBinarySniffBytes : un octet nul dans ce début de fichier le classe binaire.
	ideBinarySniffBytes = 8192
)

var ideMaxEntries = 5000

func ideEtag(fileInfo fs.FileInfo) string {
	return fmt.Sprintf("%d-%d", fileInfo.ModTime().UnixNano(), fileInfo.Size())
}

type ideEntry struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

// ideResolve ouvre la racine puis valide le chemin ; l'appelant ferme root.
func ideResolve(request *http.Request, allowRoot bool) (*os.Root, string, fs.FileInfo, error) {
	root, err := ideOpenRoot(request.URL.Query().Get("dir"))
	if err != nil {
		return nil, "", nil, err
	}
	rel, err := ideRel(request.URL.Query().Get("path"), allowRoot)
	if err != nil {
		root.Close()
		return nil, "", nil, err
	}
	fileInfo, err := ideLstat(root, rel)
	if err != nil {
		root.Close()
		return nil, "", nil, err
	}
	return root, rel, fileInfo, nil
}

func routesIDE(mux *http.ServeMux) {
	routesIDEWrite(mux)
	routesIDEIndex(mux)
	routesIDEWatch(mux)
	mux.HandleFunc("GET /v1/fs/tree", func(responseWriter http.ResponseWriter, request *http.Request) {
		root, rel, targetInfo, err := ideResolve(request, true)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer root.Close()
		if !targetInfo.IsDir() {
			ideFail(responseWriter, errIdePath)
			return
		}
		directory, err := root.Open(rel)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer directory.Close()
		dirEntries, err := directory.ReadDir(-1)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		entries := make([]ideEntry, 0, len(dirEntries))
		for _, dirEntry := range dirEntries {
			if dirEntry.Name() == ".git" {
				continue
			}
			entry := ideEntry{Name: dirEntry.Name(), Type: "file"}
			switch {
			case dirEntry.Type()&fs.ModeSymlink != 0:
				entry.Type = "symlink"
			case dirEntry.IsDir():
				entry.Type = "dir"
			}
			if info, infoErr := dirEntry.Info(); infoErr == nil {
				entry.Size, entry.Mtime = info.Size(), info.ModTime().Unix()
			}
			entries = append(entries, entry)
		}
		sort.Slice(entries, func(leftIndex, rightIndex int) bool {
			left, right := entries[leftIndex], entries[rightIndex]
			if (left.Type == "dir") != (right.Type == "dir") {
				return left.Type == "dir"
			}
			leftLower, rightLower := strings.ToLower(left.Name), strings.ToLower(right.Name)
			if leftLower != rightLower {
				return leftLower < rightLower
			}
			return left.Name < right.Name
		})
		truncated := len(entries) > ideMaxEntries
		if truncated {
			entries = entries[:ideMaxEntries]
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"entries": entries, "truncated": truncated})
	})

	mux.HandleFunc("GET /v1/fs/entries/content", func(responseWriter http.ResponseWriter, request *http.Request) {
		root, rel, targetInfo, err := ideResolve(request, false)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer root.Close()
		if !targetInfo.Mode().IsRegular() {
			ideFail(responseWriter, errIdePath)
			return
		}
		if targetInfo.Size() > ideMaxFile {
			ideFail(responseWriter, errIdeTooBig)
			return
		}
		file, err := root.Open(rel)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer file.Close()
		// Lstat puis Open est course-able : le descripteur ouvert doit être le même fichier.
		if openedInfo, statErr := file.Stat(); statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(targetInfo, openedInfo) {
			ideFail(responseWriter, errIdeSymlink)
			return
		}
		content, err := io.ReadAll(io.LimitReader(file, ideMaxFile+1))
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		if len(content) > ideMaxFile {
			ideFail(responseWriter, errIdeTooBig)
			return
		}
		head := content
		if len(head) > ideBinarySniffBytes {
			head = head[:ideBinarySniffBytes]
		}
		isBinary := strings.IndexByte(string(head), 0) >= 0 || !utf8.Valid(content)
		if isBinary {
			ideFail(responseWriter, errIdeBinary)
			return
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{
			"content": string(content),
			"etag":    ideEtag(targetInfo),
			"size":    int64(len(content)),
			"mtime":   targetInfo.ModTime().Unix(),
		})
	})
}
