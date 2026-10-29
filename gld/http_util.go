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

// statusCodes : code par défaut d'un statut ; les codes métier ne s'ajoutent que s'ils apportent un sens.
var statusCodes = map[int]string{400: "bad_request", 401: "unauthorized", 403: "forbidden", 404: "not_found", 409: "conflict", 413: "too_large", 415: "unsupported", 429: "rate_limited", 500: "internal", 503: "unavailable"}

func statusCode(status int) string {
	if c, ok := statusCodes[status]; ok {
		return c
	}
	return "error"
}

// notFound : 404 uniforme.
func notFound(w http.ResponseWriter) {
	writeErr(w, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
}

// writeErr : seul format d'erreur de l'API /v1, avec le vrai statut HTTP.
func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

// writeErrExtra : l'enveloppe d'erreur plus des champs propres à un cas (rétrocompatible : code et message restent).
func writeErrExtra(w http.ResponseWriter, status int, code, msg string, extra map[string]any) {
	e := map[string]any{"code": code, "message": msg}
	for k, v := range extra {
		e[k] = v
	}
	writeJSON(w, status, map[string]any{"error": e})
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

// bulk accumule le résultat d'une suppression multiple : chaque élément réussit ou échoue seul.
type bulk struct {
	done   []string
	failed []bulkFail
	first  bulkFail
}

type bulkFail struct {
	Item   string        `json:"item"`
	Error  bulkFailError `json:"error"`
	status int
}

type bulkFailError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (b *bulk) ok(id string) { b.done = append(b.done, id) }

func (b *bulk) fail(item string, status int, code, msg string) {
	f := bulkFail{Item: item, Error: bulkFailError{code, msg}, status: status}
	if len(b.failed) == 0 {
		b.first = f
	}
	b.failed = append(b.failed, f)
}

// reply : 200 {done, failed[, message]} dès qu'un élément a abouti ; sinon l'erreur uniforme du premier échec.
func (b *bulk) reply(w http.ResponseWriter, msg string) {
	if len(b.done) == 0 && len(b.failed) > 0 {
		writeErr(w, b.first.status, b.first.Error.Code, b.first.Error.Message)
		return
	}
	out := map[string]any{"done": append([]string{}, b.done...), "failed": append([]bulkFail{}, b.failed...)}
	if msg != "" {
		out["message"] = msg
	}
	writeJSON(w, 200, out)
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
		writeErr(w, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
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
