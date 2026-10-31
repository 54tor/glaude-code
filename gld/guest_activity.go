package main

// Ce qu'un invité lit de l'activité d'une session : le nom de l'outil en cours, jamais sa ligne de commande ni ses arguments.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

const toolDetailSeparator = " : "

// guestToolName garde « Bash » de « Bash : go test ».
func guestToolName(toolSummary string) string {
	toolName, _, _ := strings.Cut(toolSummary, toolDetailSeparator)
	return toolName
}

// guestActivity ramène « exécute Bash : go test » à « exécute Bash » (la tournure vient de la langue courante).
func guestActivity(activity string) string {
	executionPrefix := L("srv.live.exec", "A1", "")
	toolSummary, isExecution := strings.CutPrefix(activity, executionPrefix)
	if !isExecution {
		return activity
	}
	return executionPrefix + guestToolName(toolSummary)
}

func rewriteRawString(fields map[string]json.RawMessage, key string, rewrite func(string) string) {
	var text string
	if json.Unmarshal(fields[key], &text) != nil {
		return
	}
	if rewritten, err := json.Marshal(rewrite(text)); err == nil {
		fields[key] = rewritten
	}
}

// guestLiveBody réécrit le corps de GET /v1/sessions/{id}/live ; un corps illisible est renvoyé tel quel.
func guestLiveBody(body []byte) []byte {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return body
	}
	rewriteRawString(fields, "activity", guestActivity)
	rewriteRawString(fields, "tool", guestToolName)
	var subagents []map[string]json.RawMessage
	if json.Unmarshal(fields["subs"], &subagents) == nil {
		for _, subagent := range subagents {
			rewriteRawString(subagent, "activity", guestActivity)
		}
		if rewritten, err := json.Marshal(subagents); err == nil {
			fields["subs"] = rewritten
		}
	}
	rewritten, err := json.Marshal(fields)
	if err != nil {
		return body
	}
	return rewritten
}

type bufferedResponse struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (response *bufferedResponse) Header() http.Header { return response.header }
func (response *bufferedResponse) WriteHeader(status int) {
	if response.code == 0 {
		response.code = status
	}
}
func (response *bufferedResponse) Write(content []byte) (int, error) {
	response.WriteHeader(http.StatusOK)
	return response.body.Write(content)
}

// serveGuestLive sert la route à l'hôte réel puis écrit sa réponse sans les lignes de commande.
func serveGuestLive(mux *http.ServeMux, responseWriter http.ResponseWriter, request *http.Request) {
	captured := &bufferedResponse{header: http.Header{}}
	mux.ServeHTTP(captured, request)
	for headerName, headerValues := range captured.header {
		responseWriter.Header()[headerName] = headerValues
	}
	responseWriter.Header().Del("Content-Length")
	if captured.code == 0 {
		captured.code = http.StatusOK
	}
	responseWriter.WriteHeader(captured.code)
	responseWriter.Write(guestLiveBody(captured.body.Bytes()))
}

// serveGuestQueue masque la file sauf les messages que l'invité a lui-même écrits : retrouver son texte tel quel ne confirme rien.
func serveGuestQueue(mux *http.ServeMux, responseWriter http.ResponseWriter, request *http.Request, redactor *pathRedactor) {
	captured := &bufferedResponse{header: http.Header{}}
	mux.ServeHTTP(captured, request)
	body := captured.body.Bytes()
	var items []map[string]json.RawMessage
	if json.Unmarshal(body, &items) == nil {
		for _, item := range items {
			var author string
			if json.Unmarshal(item["author"], &author) == nil && author == "guest" {
				continue
			}
			rewriteRawString(item, "text", redactor.applyString)
		}
		if rewritten, err := json.Marshal(items); err == nil {
			body = rewritten
		}
	} else {
		body = redactor.apply(body)
	}
	for headerName, headerValues := range captured.header {
		responseWriter.Header()[headerName] = headerValues
	}
	responseWriter.Header().Del("Content-Length")
	responseWriter.Header().Set("Cache-Control", "no-store")
	if captured.code == 0 {
		captured.code = http.StatusOK
	}
	responseWriter.WriteHeader(captured.code)
	responseWriter.Write(body)
}
