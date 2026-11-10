package main

// Liste des documents d'une session : tout fichier affichable que le transcript montre comme écrit (Write) ou cité, du plus
// récent au plus ancien. La lecture d'un document reste soumise à wroteDoc (docs.go) : cette liste n'ouvre aucun chemin de plus.

import (
	"bytes"
	"net/http"
	"path/filepath"
	"sort"
)

const maxListedDocs = 500

var docExtensionNeedles = [][]byte{
	[]byte(".md"), []byte(".html"), []byte(".htm"), []byte(".pdf"),
	[]byte(".png"), []byte(".jpg"), []byte(".jpeg"), []byte(".gif"), []byte(".webp"),
}

type docEntry struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Ts      string `json:"ts,omitempty"`
	Size    int64  `json:"size"`
	Missing bool   `json:"missing,omitempty"`
	TooBig  bool   `json:"tooBig,omitempty"`
}

// mayHoldDoc évite d'analyser les lignes d'un transcript de plusieurs Go qui ne peuvent contenir aucun chemin de document.
func mayHoldDoc(rawLine []byte) bool {
	for _, needle := range docExtensionNeedles {
		if bytes.Contains(rawLine, needle) {
			return true
		}
	}
	return false
}

func listDocs(transcriptPath string) ([]docEntry, error) {
	lastTimestampByPath := map[string]string{}
	lastRankByPath := map[string]int{}
	nextRank := 0
	var orderedPaths []string
	scanErr := scan(transcriptPath, func(rawLine []byte) {
		if !mayHoldDoc(rawLine) {
			return
		}
		for _, message := range lineMsgsMode(rawLine, false, false) {
			if message.R != "doc" {
				continue
			}
			if _, alreadySeen := lastTimestampByPath[message.T]; !alreadySeen {
				orderedPaths = append(orderedPaths, message.T)
			}
			lastTimestampByPath[message.T] = message.Ts
			lastRankByPath[message.T] = nextRank
			nextRank++
		}
	})
	if scanErr != nil {
		return nil, scanErr
	}
	sort.SliceStable(orderedPaths, func(left, right int) bool {
		return lastRankByPath[orderedPaths[left]] > lastRankByPath[orderedPaths[right]]
	})
	if len(orderedPaths) > maxListedDocs {
		orderedPaths = orderedPaths[:maxListedDocs]
	}
	entries := make([]docEntry, 0, len(orderedPaths))
	for _, path := range orderedPaths {
		entries = append(entries, describeDoc(path, lastTimestampByPath[path]))
	}
	return entries, nil
}

func describeDoc(path, timestamp string) docEntry {
	entry := docEntry{Path: path, Name: filepath.Base(path), Ts: timestamp}
	documentFile, openErr := openTrusted(path)
	if openErr != nil {
		entry.Missing = true
		return entry
	}
	defer documentFile.Close()
	fileInfo, statErr := documentFile.Stat()
	if statErr != nil || !fileInfo.Mode().IsRegular() {
		entry.Missing = true
		return entry
	}
	entry.Size = fileInfo.Size()
	entry.TooBig = entry.Size > docMax
	return entry
}

func routesDocsList(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sessions/{id}/conversation/documents", func(responseWriter http.ResponseWriter, request *http.Request) {
		id, isValid := pathID(responseWriter, request, "id")
		if !isValid {
			return
		}
		transcriptPath, isFound := transcript(request.URL.Query().Get("dir"), id)
		if !isFound {
			notFound(responseWriter)
			return
		}
		entries, listErr := listDocs(transcriptPath)
		if listErr != nil {
			notFound(responseWriter)
			return
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"documents": entries})
	})
}
