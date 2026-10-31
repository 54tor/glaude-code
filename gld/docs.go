package main

// Documents produits : un fichier que Claude a écrit (outil Write) dans une conversation s'affiche dans celle-ci. Le serveur ne relit
// que les chemins que le transcript montre comme écrits, jamais un chemin choisi par le navigateur.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	docMax           = 20 << 20
	maxMentionedDocs = 5
)

// docTypes : extensions affichables et type servi. Pas de SVG (script possible).
var docTypes = map[string]string{
	".md": "text/plain; charset=utf-8", ".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".pdf": "application/pdf", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
}

func docType(path string) string { return docTypes[strings.ToLower(filepath.Ext(path))] }

// writtenPath : le file_path d'un appel Write de ce bloc, s'il est affichable.
func writtenPath(name string, input json.RawMessage) string {
	if name != "Write" {
		return ""
	}
	var writeInput struct {
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(input, &writeInput) != nil {
		return ""
	}
	isDisplayable := filepath.IsAbs(writeInput.FilePath) && docType(writeInput.FilePath) != ""
	if !isDisplayable {
		return ""
	}
	return writeInput.FilePath
}

// mentionedDocs : chemins absolus de documents cités dans un texte de Claude (le rapport écrit par un script ou une concaténation
// n'est pas un Write, mais Claude en donne le chemin). Seuls les fichiers qui existent, 5 au plus.
var docMention = regexp.MustCompile(`(?:^|[\s"'(\x60\[])((?:/[\w.@+~-]+)+\.(?i:md|html?|pdf|png|jpe?g|gif|webp))`)

func mentionedDocs(text string) []string {
	var documents []string
	seen := map[string]bool{}
	for _, match := range docMention.FindAllStringSubmatch(text, -1) {
		path := match[1]
		if seen[path] || len(documents) == maxMentionedDocs {
			continue
		}
		seen[path] = true
		if fileInfo, err := os.Stat(path); err == nil && fileInfo.Mode().IsRegular() {
			documents = append(documents, path)
		}
	}
	return documents
}

// wroteDoc : le transcript montre-t-il ce chemin comme écrit ou cité ?
func wroteDoc(transcriptPath, path string) bool {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false) // Claude n'échappe pas & < > dans ses transcripts
	_ = encoder.Encode(path)
	needle := bytes.TrimSpace(encoded.Bytes())
	needle = needle[1 : len(needle)-1] // le chemin cité dans un texte n'est pas une chaîne JSON à lui seul
	found := false
	_ = scan(transcriptPath, func(rawLine []byte) {
		if found || !bytes.Contains(rawLine, needle) {
			return
		}
		for _, message := range lineMsgsMode(rawLine, false, false) {
			if message.R == "doc" && message.T == path {
				found = true
			}
		}
	})
	return found
}

func routesDocs(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sessions/{id}/conversation/document", func(responseWriter http.ResponseWriter, request *http.Request) {
		id, isValid := pathID(responseWriter, request, "id")
		if !isValid {
			return
		}
		query := request.URL.Query()
		transcriptPath, isValid := transcript(query.Get("dir"), id)
		path := query.Get("path")
		contentType := docType(path)
		isAllowed := isValid && contentType != "" && filepath.IsAbs(path) && wroteDoc(transcriptPath, path)
		if !isAllowed {
			notFound(responseWriter)
			return
		}
		documentFile, err := openTrusted(path)
		if err != nil {
			notFound(responseWriter)
			return
		}
		defer documentFile.Close()
		fileInfo, statErr := documentFile.Stat()
		isServable := statErr == nil && fileInfo.Mode().IsRegular() && fileInfo.Size() <= docMax
		if !isServable {
			notFound(responseWriter)
			return
		}
		header := responseWriter.Header()
		header.Set("Content-Type", contentType)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Content-Security-Policy", "default-src 'none'; sandbox; img-src data:; style-src 'unsafe-inline'; font-src data:")
		header.Set("Cache-Control", "private, no-cache")
		io.Copy(responseWriter, documentFile)
	})
}
