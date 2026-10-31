package main

// Aides communes aux routes : réponse JSON, lecture des sessions visées, journal.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func writeJSON(responseWriter http.ResponseWriter, code int, payload any) {
	responseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	responseWriter.WriteHeader(code)
	json.NewEncoder(responseWriter).Encode(payload)
}

// statusCodes : code par défaut d'un statut ; les codes métier ne s'ajoutent que s'ils apportent un sens.
var statusCodes = map[int]string{400: "bad_request", 401: "unauthorized", 403: "forbidden", 404: "not_found", 409: "conflict", 413: "too_large", 415: "unsupported", 429: "rate_limited", 500: "internal", 503: "unavailable"}

func statusCode(status int) string {
	if statusLabel, found := statusCodes[status]; found {
		return statusLabel
	}
	return "error"
}

// notFound : 404 uniforme.
func notFound(responseWriter http.ResponseWriter) {
	writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
}

// writeErr : seul format d'erreur de l'API /v1, avec le vrai statut HTTP.
func writeErr(responseWriter http.ResponseWriter, status int, code, message string) {
	writeJSON(responseWriter, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// writeErrExtra : l'enveloppe d'erreur plus des champs propres à un cas (rétrocompatible : code et message restent).
func writeErrExtra(responseWriter http.ResponseWriter, status int, code, message string, extra map[string]any) {
	errorFields := map[string]any{"code": code, "message": message}
	for fieldName, fieldValue := range extra {
		errorFields[fieldName] = fieldValue
	}
	writeJSON(responseWriter, status, map[string]any{"error": errorFields})
}

// forbid refuse une requête avant routage : erreur JSON sur /v1, texte brut pour les pages.
func forbid(responseWriter http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/v1/") {
		writeErr(responseWriter, http.StatusForbidden, "forbidden", L("srv.http_util.refuse"))
		return
	}
	http.Error(responseWriter, "forbidden", http.StatusForbidden)
}

// decodeBody lit un corps JSON borné ; false après avoir répondu 400.
func decodeBody(responseWriter http.ResponseWriter, request *http.Request, destination any, limit int64) bool {
	if json.NewDecoder(io.LimitReader(request.Body, limit)).Decode(destination) != nil {
		writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
		return false
	}
	return true
}

// pathID lit un segment {name} qui doit être un identifiant de session ; false après avoir répondu 400.
func pathID(responseWriter http.ResponseWriter, request *http.Request, name string) (string, bool) {
	id := request.PathValue(name)
	if !idRe.MatchString(id) {
		writeErr(responseWriter, http.StatusBadRequest, "bad_id", L("srv.http_util.idInvalide"))
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

func (result *bulk) ok(id string) {
	result.done = append(result.done, id)
}

func (result *bulk) fail(item string, status int, code, message string) {
	failure := bulkFail{Item: item, Error: bulkFailError{code, message}, status: status}
	if len(result.failed) == 0 {
		result.first = failure
	}
	result.failed = append(result.failed, failure)
}

// reply : 200 {done, failed[, message]} dès qu'un élément a abouti ; sinon l'erreur uniforme du premier échec.
func (result *bulk) reply(responseWriter http.ResponseWriter, message string) {
	nothingSucceeded := len(result.done) == 0 && len(result.failed) > 0
	if nothingSucceeded {
		writeErr(responseWriter, result.first.status, result.first.Error.Code, result.first.Error.Message)
		return
	}
	response := map[string]any{
		"done":   append([]string{}, result.done...),
		"failed": append([]bulkFail{}, result.failed...),
	}
	if message != "" {
		response["message"] = message
	}
	writeJSON(responseWriter, http.StatusOK, response)
}

type item struct{ Dir, ID string }

// itemMax : plafond d'éléments d'une requête multiple (?item=… répété, ou items du corps) ; le client découpe en lots (100 éléments, URL sous ~6 Ko).
const itemMax = 100

const itemsBodyMax = 1 << 22

// validItems n'accepte que des sessions qui existent vraiment ; false après avoir répondu.
func validItems(responseWriter http.ResponseWriter, items []item) bool {
	if len(items) == 0 {
		writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
		return false
	}
	if len(items) > itemMax {
		writeErr(responseWriter, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
		return false
	}
	for _, requestedItem := range items {
		if _, exists := transcript(requestedItem.Dir, requestedItem.ID); !exists {
			writeErr(responseWriter, http.StatusBadRequest, "bad_id", L("srv.http_util.idInvalide"))
			return false
		}
	}
	return true
}

// withChildren ajoute aux éléments les sessions qu'ils ont lancées (Parent), récursivement : supprimer une session emporte ses sous-sessions.
func withChildren(items []item) []item {
	have := map[string]bool{}
	for _, requestedItem := range items {
		have[requestedItem.ID] = true
	}
	for grew := true; grew; {
		grew = false
		for _, group := range sessions() {
			for _, session := range group.Sessions {
				isNewChild := session.Parent != "" && have[session.Parent] && !have[session.ID]
				if isNewChild && idRe.MatchString(session.ID) {
					have[session.ID] = true
					items = append(items, item{session.Dir, session.ID})
					grew = true
				}
			}
		}
	}
	return items
}

// itemsQuery lit ?item=<dossier>:<id> répété : l'identifiant ne contient jamais « : », le nom de dossier peut en porter.
func itemsQuery(responseWriter http.ResponseWriter, request *http.Request) ([]item, bool) {
	var items []item
	for _, itemParameter := range request.URL.Query()["item"] {
		separatorIndex := strings.LastIndex(itemParameter, ":")
		if separatorIndex < 0 {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return nil, false
		}
		items = append(items, item{itemParameter[:separatorIndex], itemParameter[separatorIndex+1:]})
	}
	return items, validItems(responseWriter, items)
}

// readItems décode {items:[{dir,id}], to} (PATCH /v1/sessions, POST /v1/trash/sessions).
func readItems(responseWriter http.ResponseWriter, request *http.Request) ([]item, string, bool) {
	var body struct {
		Items []item
		To    string
	}
	if !decodeBody(responseWriter, request, &body, itemsBodyMax) || !validItems(responseWriter, body.Items) {
		return nil, "", false
	}
	return body.Items, body.To, true
}

// ids : les identifiants courts de sessions, pour le journal.
func ids(items []item) []string {
	shortIDs := make([]string, len(items))
	for itemIndex, requestedItem := range items {
		shortIDs[itemIndex] = short(requestedItem.ID)
	}
	return shortIDs
}

func errOr(err error) string {
	if err != nil {
		return L("srv.common.failed", "A1", err.Error())
	}
	return "ok"
}
