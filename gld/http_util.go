package main

// Aides communes aux routes : réponse JSON, lecture des sessions visées, journal.

import (
	"encoding/json"
	"io"
	"net/http"
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

// decodeBody lit un corps JSON borné ; false après avoir répondu 400.
func decodeBody(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	if json.NewDecoder(io.LimitReader(r.Body, limit)).Decode(v) != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
		return false
	}
	return true
}

type item struct{ Dir, ID string }

// readItems décode {items:[{dir,id}], to} et n'accepte que des sessions qui existent vraiment.
func readItems(w http.ResponseWriter, r *http.Request) ([]item, string, bool) {
	var req struct {
		Items []item
		To    string
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<22)).Decode(&req) != nil || len(req.Items) == 0 {
		writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
		return nil, "", false
	}
	for _, it := range req.Items {
		if _, ok := transcript(it.Dir, it.ID); !ok {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.idInvalide")})
			return nil, "", false
		}
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
