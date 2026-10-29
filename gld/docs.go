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

const docMax = 20 << 20

// docTypes : extensions affichables et type servi. Pas de SVG (script possible).
var docTypes = map[string]string{
	".md": "text/plain; charset=utf-8", ".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".pdf": "application/pdf", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
}

func docType(p string) string { return docTypes[strings.ToLower(filepath.Ext(p))] }

// writtenPath : le file_path d'un appel Write de ce bloc, s'il est affichable.
func writtenPath(name string, in json.RawMessage) string {
	if name != "Write" {
		return ""
	}
	var m struct {
		P string `json:"file_path"`
	}
	if json.Unmarshal(in, &m) != nil || !filepath.IsAbs(m.P) || docType(m.P) == "" {
		return ""
	}
	return m.P
}

// mentionedDocs : chemins absolus de documents cités dans un texte de Claude (le rapport écrit par un script ou une concaténation
// n'est pas un Write, mais Claude en donne le chemin). Seuls les fichiers qui existent, 5 au plus.
var docMention = regexp.MustCompile(`(?:^|[\s"'(\x60\[])((?:/[\w.@+~-]+)+\.(?i:md|html?|pdf|png|jpe?g|gif|webp))`)

func mentionedDocs(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range docMention.FindAllStringSubmatch(text, -1) {
		p := m[1]
		if seen[p] || len(out) == 5 {
			continue
		}
		seen[p] = true
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			out = append(out, p)
		}
	}
	return out
}

// wroteDoc : le transcript montre-t-il ce chemin comme écrit ou cité ?
func wroteDoc(tr, path string) bool {
	needle, _ := json.Marshal(path)
	needle = needle[1 : len(needle)-1] // le chemin cité dans un texte n'est pas une chaîne JSON à lui seul
	found := false
	_ = scan(tr, func(b []byte) {
		if found || !bytes.Contains(b, needle) {
			return
		}
		for _, m := range lineMsgsMode(b, false, false) {
			if m.R == "doc" && m.T == path {
				found = true
			}
		}
	})
	return found
}

func routesDocs(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sessions/{id}/conversation/document", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		q := r.URL.Query()
		tr, ok := transcript(q.Get("dir"), id)
		path := q.Get("path")
		typ := docType(path)
		notFound := func() { writeErr(w, http.StatusNotFound, "not_found", L("srv.http_util.introuvable")) }
		if !ok || typ == "" || !filepath.IsAbs(path) || !wroteDoc(tr, path) {
			notFound()
			return
		}
		f, err := openTrusted(path)
		if err != nil {
			notFound()
			return
		}
		defer f.Close()
		if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || st.Size() > docMax {
			notFound()
			return
		}
		h := w.Header()
		h.Set("Content-Type", typ)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox; img-src data:; style-src 'unsafe-inline'; font-src data:")
		h.Set("Cache-Control", "private, no-cache")
		io.Copy(w, f)
	})
}
