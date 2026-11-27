package main

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var attachmentRe = regexp.MustCompile(imgPrefix + `/([^"\\\s)/]+)`)

// sessionOfTranscript : l'id de la session d'un transcript, ou de la session parente pour un sous-agent (<id>/subagents/x.jsonl).
func sessionOfTranscript(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if idRe.MatchString(base) {
		return base
	}
	for dir := filepath.Dir(path); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if idRe.MatchString(filepath.Base(dir)) {
			return filepath.Base(dir)
		}
	}
	return ""
}

// attachmentCitations : nom d'un fichier déposé → ids des sessions dont un transcript le cite.
func attachmentCitations() map[string][]string {
	citations := map[string][]string{}
	visit := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		sessionID := sessionOfTranscript(path)
		if sessionID == "" {
			return nil
		}
		scan(path, func(rawLine []byte) {
			for _, match := range attachmentRe.FindAllSubmatch(rawLine, -1) {
				name := string(match[1])
				if !slices.Contains(citations[name], sessionID) {
					citations[name] = append(citations[name], sessionID)
				}
			}
		})
		return nil
	}
	// les sessions en corbeille citent encore leurs fichiers : les restaurer ne doit pas les retrouver absents
	filepath.WalkDir(root, visit)
	filepath.WalkDir(sessionTrashDir(), visit)
	return citations
}
