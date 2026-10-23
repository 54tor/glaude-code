package main

// Bloc-notes par projet (ensemble nommé de dossiers) ou par dossier : un texte libre que l'on garde à côté des sessions et que l'on
// peut envoyer à une session. Une clé « p/<nom du projet> » ou « d/<dossier> », un seul fichier JSON dans le dossier de configuration.

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	noteMax  = 64 << 10
	notesMax = 500
)

var notesMu sync.Mutex

func notesFile() string { return filepath.Join(cfgDir(), "gld-web-notes.json") }

func notesLoad() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(notesFile()); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func notesSave(m map[string]string) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	tmp := notesFile() + ".tmp"
	if err := writeSynced(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, notesFile())
}

func noteKeyOK(k string) bool {
	return len(k) > 2 && len(k) <= 600 && (strings.HasPrefix(k, "p/") || strings.HasPrefix(k, "d/")) && !strings.ContainsRune(k, 0)
}

func routesNotes(mux *http.ServeMux) {
	mux.HandleFunc("GET /notes", func(w http.ResponseWriter, r *http.Request) {
		notesMu.Lock()
		defer notesMu.Unlock()
		writeJSON(w, 200, map[string]any{"notes": notesLoad()})
	})
	// {key, text} : enregistre (texte vide : supprime) ; {key, to} : déplace la note (renommage d'un projet).
	mux.HandleFunc("POST /notes", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key, Text, To string }
		if json.NewDecoder(io.LimitReader(r.Body, noteMax+1<<10)).Decode(&req) != nil || !noteKeyOK(req.Key) || len(req.Text) > noteMax || (req.To != "" && !noteKeyOK(req.To)) {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		notesMu.Lock()
		defer notesMu.Unlock()
		m := notesLoad()
		switch {
		case req.To != "":
			if t, ok := m[req.Key]; ok {
				m[req.To] = t
				delete(m, req.Key)
			}
		case strings.TrimSpace(req.Text) == "":
			delete(m, req.Key)
		default:
			if _, has := m[req.Key]; !has && len(m) >= notesMax {
				writeJSON(w, 200, map[string]any{"ok": false, "out": L("srv.notes.tropDeNotes")})
				return
			}
			m[req.Key] = req.Text
		}
		if err := notesSave(m); err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.notes.noteEnregistree")})
	})
}
