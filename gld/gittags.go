package main

// Tags locaux proposés à la poussée : les plus récents d'abord.

import (
	"net/http"
	"strings"
)

const gitTagsMax = 30

func gitTagNames(dir string) []string {
	tagOutput, _ := gitOut1(dir, "tag", "--list", "--sort=-creatordate")
	tagNames := []string{}
	for _, tagName := range strings.Split(tagOutput, "\n") {
		if tagName != "" {
			tagNames = append(tagNames, tagName)
		}
	}
	return tagNames
}

func routesGitTags(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/git/tags", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir := request.URL.Query().Get("dir")
		if !knownRepo(dir) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.dossierInvalide"))
			return
		}
		tagNames := gitTagNames(dir)
		writeJSON(responseWriter, http.StatusOK, map[string]any{"tags": tagNames[:min(len(tagNames), gitTagsMax)]})
	})
}
