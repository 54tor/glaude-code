package main

// Documents produits : un fichier que Claude a écrit (outil Write) dans une conversation s'affiche dans celle-ci. Le serveur ne relit
// que les chemins que le transcript montre comme écrits, jamais un chemin choisi par le navigateur.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
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

// wroteDoc : le transcript contient-il un Write de ce chemin ?
func wroteDoc(tr, path string) bool {
	needle, _ := json.Marshal(path)
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
	mux.HandleFunc("GET /conv-doc", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		tr, ok := transcript(q.Get("dir"), q.Get("id"))
		path := q.Get("path")
		typ := docType(path)
		if !ok || typ == "" || !filepath.IsAbs(path) || !wroteDoc(tr, path) {
			http.NotFound(w, r)
			return
		}
		f, err := openTrusted(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || st.Size() > docMax {
			http.NotFound(w, r)
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
