package main

// Bloc-notes par projet (ensemble nommé de dossiers) ou par dossier : un texte libre que l'on garde à côté des sessions et que l'on
// peut envoyer à une session. Une clé « p/<nom du projet> » ou « d/<dossier> », un seul fichier JSON dans le dossier de configuration.

import (
	"encoding/json"
	"net/http"
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

// notesLoad : les notes ; un fichier absent est vide, un fichier illisible une erreur (on ne réécrit jamais par-dessus).
func notesLoad() (map[string]string, error) {
	m := map[string]string{}
	b, err := readTrusted(notesFile())
	if err != nil {
		return m, nil
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, terr("srv.notes.fichierIllisible", "A1", err.Error())
	}
	return m, nil
}

func notesSave(m map[string]string) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	return rWriteAtomic(cfgDir(), notesFile(), b, 0o600)
}

func noteKeyOK(k string) bool {
	return len(k) > 2 && len(k) <= 600 && (strings.HasPrefix(k, "p/") || strings.HasPrefix(k, "d/")) && !strings.ContainsRune(k, 0)
}

func routesNotes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/notes", func(w http.ResponseWriter, r *http.Request) {
		notesMu.Lock()
		defer notesMu.Unlock()
		m, err := notesLoad()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"notes": m})
	})
	// {key, text} : enregistre (texte vide : supprime) ; {key, to} : déplace la note (renommage d'un projet).
	mux.HandleFunc("PUT /v1/notes", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key, Text, To string }
		if !decodeBody(w, r, &req, noteMax+1<<10) {
			return
		}
		if !noteKeyOK(req.Key) || len(req.Text) > noteMax || (req.To != "" && !noteKeyOK(req.To)) {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		notesMu.Lock()
		defer notesMu.Unlock()
		m, err := notesLoad()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		switch {
		case req.To != "":
			if t, ok := m[req.Key]; ok && req.To != req.Key {
				if d, has := m[req.To]; has {
					if t = mergeNotes(d, t); len(t) > noteMax {
						writeErr(w, http.StatusRequestEntityTooLarge, "too_large", L("srv.notes.fusionTropLongue"))
						return
					}
				}
				m[req.To] = t
				delete(m, req.Key)
			}
		case strings.TrimSpace(req.Text) == "":
			delete(m, req.Key)
		default:
			if _, has := m[req.Key]; !has && len(m) >= notesMax {
				writeErr(w, http.StatusConflict, "conflict", L("srv.notes.tropDeNotes"))
				return
			}
			m[req.Key] = req.Text
		}
		if err := notesSave(m); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.notes.noteEnregistree")})
	})
}

// noteItems : les items d'une note, comme l'UI les lit (tableau JSON, sinon une ligne non vide par item).
func noteItems(s string) []string {
	var v []string
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// mergeNotes : un déplacement sur une clé déjà notée ajoute les items à la suite au lieu d'écraser la destination.
func mergeNotes(dst, src string) string {
	var b strings.Builder
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false) // sinon < > & triplent de taille à chaque fusion
	e.Encode(append(noteItems(dst), noteItems(src)...))
	return strings.TrimSuffix(b.String(), "\n")
}
