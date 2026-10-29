package main

// Aides communes aux routes : réponse JSON, lecture des sessions visées, journal.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// writeErr : seul format d'erreur de l'API /v1, avec le vrai statut HTTP.
func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

// forbid refuse une requête avant routage : erreur JSON sur /v1, texte brut pour les pages.
func forbid(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		writeErr(w, http.StatusForbidden, "forbidden", L("srv.http_util.refuse"))
		return
	}
	http.Error(w, "forbidden", http.StatusForbidden)
}

// decodeBody lit un corps JSON borné ; false après avoir répondu 400.
func decodeBody(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	if json.NewDecoder(io.LimitReader(r.Body, limit)).Decode(v) != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
		return false
	}
	return true
}

// pathID lit un segment {name} qui doit être un identifiant de session ; false après avoir répondu 400.
func pathID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := r.PathValue(name)
	if !idRe.MatchString(id) {
		writeErr(w, http.StatusBadRequest, "bad_id", L("srv.http_util.idInvalide"))
		return "", false
	}
	return id, true
}

type item struct{ Dir, ID string }

// itemMax : plafond d'éléments d'une requête multiple (?item=… répété, ou items du corps).
const itemMax = 400

// validItems n'accepte que des sessions qui existent vraiment ; false après avoir répondu.
func validItems(w http.ResponseWriter, items []item) bool {
	if len(items) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
		return false
	}
	if len(items) > itemMax {
		writeErr(w, http.StatusBadRequest, "too_many", L("srv.http_util.tropDElements"))
		return false
	}
	for _, it := range items {
		if _, ok := transcript(it.Dir, it.ID); !ok {
			writeErr(w, http.StatusBadRequest, "bad_id", L("srv.http_util.idInvalide"))
			return false
		}
	}
	return true
}

// itemsQuery lit ?item=<dossier>:<id> répété : l'identifiant ne contient jamais « : », le nom de dossier peut en porter.
func itemsQuery(w http.ResponseWriter, r *http.Request) ([]item, bool) {
	var items []item
	for _, v := range r.URL.Query()["item"] {
		i := strings.LastIndex(v, ":")
		if i < 0 {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return nil, false
		}
		items = append(items, item{v[:i], v[i+1:]})
	}
	return items, validItems(w, items)
}

// readItems décode {items:[{dir,id}], to} (PATCH /v1/sessions, POST /v1/trash/sessions).
func readItems(w http.ResponseWriter, r *http.Request) ([]item, string, bool) {
	var req struct {
		Items []item
		To    string
	}
	if !decodeBody(w, r, &req, 1<<22) || !validItems(w, req.Items) {
		return nil, "", false
	}
	return req.Items, req.To, true
}

// ids : les identifiants courts de sessions, pour le journal.
func ids(items []item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID[:8]
	}
	return out
}

func errOr(err error) string {
	if err != nil {
		return L("srv.common.failed", "A1", err.Error())
	}
	return "ok"
}
