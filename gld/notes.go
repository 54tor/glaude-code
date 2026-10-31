package main

// Bloc-notes par projet (ensemble nommé de dossiers) ou par dossier : un texte libre que l'on garde à côté des sessions et que l'on
// peut envoyer à une session. Une clé « p/<nom du projet> » ou « d/<dossier> », un seul fichier JSON dans le dossier de configuration.

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	noteMax  = 64 << 10
	notesMax = 500

	noteKeyMaxLength    = 600
	noteKeyPrefixLength = 2
)

var notesMu sync.Mutex

func notesFile() string { return filepath.Join(cfgDir(), "gld-web-notes.json") }

// notesLoad : les notes ; un fichier absent est vide, un fichier illisible une erreur (on ne réécrit jamais par-dessus).
func notesLoad() (map[string]string, error) {
	notes := map[string]string{}
	content, readErr := readTrusted(notesFile())
	if readErr != nil {
		return notes, nil
	}
	if decodeErr := json.Unmarshal(content, &notes); decodeErr != nil {
		return nil, terr("srv.notes.fichierIllisible", "A1", decodeErr.Error())
	}
	return notes, nil
}

func notesSave(notes map[string]string) error {
	content, _ := json.MarshalIndent(notes, "", "  ")
	return rWriteAtomic(cfgDir(), notesFile(), content, 0o600)
}

func noteKeyOK(key string) bool {
	hasValidLength := len(key) > noteKeyPrefixLength && len(key) <= noteKeyMaxLength
	hasKnownPrefix := strings.HasPrefix(key, "p/") || strings.HasPrefix(key, "d/")
	return hasValidLength && hasKnownPrefix && !strings.ContainsRune(key, 0)
}

func routesNotes(mux *http.ServeMux) {
	routesNotesSent(mux)
	mux.HandleFunc("GET /v1/notes", func(responseWriter http.ResponseWriter, request *http.Request) {
		notesMu.Lock()
		defer notesMu.Unlock()
		notes, err := notesLoad()
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"notes": notes})
	})
	// {key, text} : enregistre (texte vide : supprime) ; {key, to} : déplace la note (renommage d'un projet).
	mux.HandleFunc("PUT /v1/notes", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Key, Text, To string }
		if !decodeBody(responseWriter, request, &body, noteMax+1<<10) {
			return
		}
		if !noteKeyOK(body.Key) || len(body.Text) > noteMax || (body.To != "" && !noteKeyOK(body.To)) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		notesMu.Lock()
		defer notesMu.Unlock()
		notes, err := notesLoad()
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		switch {
		case body.To != "":
			if movedText, isPresent := notes[body.Key]; isPresent && body.To != body.Key {
				if destinationText, hasDestination := notes[body.To]; hasDestination {
					if movedText = mergeNotes(destinationText, movedText); len(movedText) > noteMax {
						writeErr(responseWriter, http.StatusRequestEntityTooLarge, "too_large", L("srv.notes.fusionTropLongue"))
						return
					}
				}
				notes[body.To] = movedText
				delete(notes, body.Key)
			}
		case strings.TrimSpace(body.Text) == "":
			delete(notes, body.Key)
		default:
			if _, exists := notes[body.Key]; !exists && len(notes) >= notesMax {
				writeErr(responseWriter, http.StatusConflict, "conflict", L("srv.notes.tropDeNotes"))
				return
			}
			notes[body.Key] = body.Text
		}
		if saveErr := notesSave(notes); saveErr != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", saveErr.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.notes.noteEnregistree")})
	})
}

// noteItems : les items d'une note, comme l'UI les lit (tableau JSON, sinon une ligne non vide par item).
func noteItems(noteText string) []string {
	var jsonItems []string
	if json.Unmarshal([]byte(noteText), &jsonItems) == nil {
		return jsonItems
	}
	var items []string
	for _, textLine := range strings.Split(noteText, "\n") {
		if strings.TrimSpace(textLine) != "" {
			items = append(items, textLine)
		}
	}
	return items
}

// mergeNotes : un déplacement sur une clé déjà notée ajoute les items à la suite au lieu d'écraser la destination.
func mergeNotes(destinationText, sourceText string) string {
	var merged strings.Builder
	encoder := json.NewEncoder(&merged)
	encoder.SetEscapeHTML(false) // sinon < > & triplent de taille à chaque fusion
	encoder.Encode(append(noteItems(destinationText), noteItems(sourceText)...))
	return strings.TrimSuffix(merged.String(), "\n")
}

const sentMax = 50

type sentEntry struct {
	ID   string `json:"id"`
	At   int64  `json:"at"`
	Text string `json:"text"`
	To   string `json:"to"`
}

func sentFile() string { return filepath.Join(cfgDir(), "gld-web-notes-history.json") }

func sentLoad() (map[string][]sentEntry, error) {
	history := map[string][]sentEntry{}
	content, readErr := readTrusted(sentFile())
	if readErr != nil {
		return history, nil
	}
	if decodeErr := json.Unmarshal(content, &history); decodeErr != nil {
		return nil, terr("srv.notes.fichierIllisible", "A1", decodeErr.Error())
	}
	return history, nil
}

func sentSave(history map[string][]sentEntry) error {
	content, _ := json.MarshalIndent(history, "", "  ")
	return rWriteAtomic(cfgDir(), sentFile(), content, 0o600)
}

// routesNotesSent : historique des items envoyés à une session, écrit avant l'envoi pour qu'un envoi perdu ne perde pas le texte.
func routesNotesSent(mux *http.ServeMux) {
	fail := func(responseWriter http.ResponseWriter, err error) {
		writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
	}
	mux.HandleFunc("GET /v1/notes/sent", func(responseWriter http.ResponseWriter, request *http.Request) {
		key := request.URL.Query().Get("key")
		if !noteKeyOK(key) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		notesMu.Lock()
		defer notesMu.Unlock()
		history, err := sentLoad()
		if err != nil {
			fail(responseWriter, err)
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"sent": append([]sentEntry{}, history[key]...)})
	})
	mux.HandleFunc("POST /v1/notes/sent", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Key, Text, To string }
		if !decodeBody(responseWriter, request, &body, noteMax+2<<10) {
			return
		}
		if !noteKeyOK(body.Key) || strings.TrimSpace(body.Text) == "" || len(body.Text) > noteMax || len(body.To) > noteKeyMaxLength {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		notesMu.Lock()
		defer notesMu.Unlock()
		history, err := sentLoad()
		if err != nil {
			fail(responseWriter, err)
			return
		}
		if _, exists := history[body.Key]; !exists && len(history) >= notesMax {
			writeErr(responseWriter, http.StatusConflict, "conflict", L("srv.notes.tropDeNotes"))
			return
		}
		now := time.Now()
		entry := sentEntry{ID: strconv.FormatInt(now.UnixNano(), 36), At: now.UnixMilli(), Text: body.Text, To: body.To}
		entries := append(history[body.Key], entry)
		if len(entries) > sentMax {
			entries = entries[len(entries)-sentMax:]
		}
		history[body.Key] = entries
		if saveErr := sentSave(history); saveErr != nil {
			fail(responseWriter, saveErr)
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"entry": entry})
	})
	mux.HandleFunc("DELETE /v1/notes/sent", func(responseWriter http.ResponseWriter, request *http.Request) {
		key, entryID := request.URL.Query().Get("key"), request.URL.Query().Get("id")
		if !noteKeyOK(key) || entryID == "" {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		notesMu.Lock()
		defer notesMu.Unlock()
		history, err := sentLoad()
		if err != nil {
			fail(responseWriter, err)
			return
		}
		kept := history[key][:0:0]
		for _, entry := range history[key] {
			if entry.ID != entryID {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 {
			delete(history, key)
		} else {
			history[key] = kept
		}
		if saveErr := sentSave(history); saveErr != nil {
			fail(responseWriter, saveErr)
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.notes.noteEnregistree")})
	})
}
