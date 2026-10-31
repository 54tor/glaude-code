package main

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
)

const snippetUploadMax = 16 << 20

// routesSnippets : snippets (commandes Claude Code) personnels et de projet.
func routesSnippets(mux *http.ServeMux) {
	snipDir = env("GLD_WEB_SNIPPETS", os.Getenv("CC_WEB_SNIPPETS"))
	mux.HandleFunc("GET /v1/snippets", func(responseWriter http.ResponseWriter, request *http.Request) {
		snippets, scopes := listSnippets()
		writeJSON(responseWriter, 200, map[string]any{"dir": userCmdDir(), "files": snippets, "scopes": scopes})
	})
	mux.HandleFunc("GET /v1/snippets/{key}", func(responseWriter http.ResponseWriter, request *http.Request) {
		text, err := readSnippet(request.PathValue("key"))
		if errors.Is(err, os.ErrNotExist) { // nouveau snippet pas encore enregistré
			writeJSON(responseWriter, 200, map[string]any{"text": "", "missing": true})
			return
		}
		if err != nil {
			writeErr(responseWriter, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("PUT /v1/snippets/{key}", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Text   string
			Create bool // nouveau snippet : refusé s'il existe déjà
		}
		if !decodeBody(responseWriter, request, &body, snippetMax+1<<10) {
			return
		}
		key := request.PathValue("key")
		err := saveSnippet(key, body.Text, body.Create)
		journal("snippet-save %s (%d octets) : %v", key, len(body.Text), errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_snippets.snippetEnregistre")})
	})
	mux.HandleFunc("POST /v1/snippets/drafts", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Model, Instruction, Text string }
		if !decodeBody(responseWriter, request, &body, snippetMax+16<<10) {
			return
		}
		text, cost, err := snippetDraft(body.Model, body.Instruction, body.Text)
		journal("snippet-draft %s (%d caractères) : %v (%.4f $)", body.Model, len(body.Text), errOr(err), cost)
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_snippets.miseEnFormePrete"), "text": text, "cost": cost})
	})
	mux.HandleFunc("POST /v1/snippets/merges", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Keys   []string
			Out    string // document de sortie : l'une des clés (vide : la première)
			Launch bool
			Args   string
			Prompt string
		}
		if !decodeBody(responseWriter, request, &body, 1<<16) {
			return
		}
		prompt, cwd, err := snippetMergePrompt(body.Keys, body.Out)
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		mergeReply(responseWriter, request, prompt, cwd, body.Prompt, body.Launch, body.Args)
	})
	mux.HandleFunc("PATCH /v1/snippets/{key}", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ To string }
		if !decodeBody(responseWriter, request, &body, 1<<12) {
			return
		}
		from := request.PathValue("key")
		err := renameSnippet(from, body.To)
		journal("snippet-rename %s -> %s : %v", from, body.To, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_snippets.snippetRenomme")})
	})
	mux.HandleFunc("DELETE /v1/snippets", func(responseWriter http.ResponseWriter, request *http.Request) {
		keys := request.URL.Query()["item"]
		if len(keys) == 0 {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		if len(keys) > itemMax {
			writeErr(responseWriter, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
			return
		}
		var bulkReply bulk
		for _, key := range keys {
			err := removeSnippet(key)
			journal("snippet-rm %s (copie à la corbeille) : %v", key, errOr(err))
			if err != nil {
				bulkReply.fail(key, http.StatusConflict, "conflict", err.Error())
				continue
			}
			bulkReply.ok(key)
		}
		bulkReply.reply(responseWriter, L("srv.routes_snippets.snippetSupprime"))
	})
	// Export : les snippets d'un scope en archive zip ; import : fichiers .md ou archives .zip (multipart).
	mux.HandleFunc("GET /v1/snippets/export", func(responseWriter http.ResponseWriter, request *http.Request) {
		scope := request.URL.Query().Get("scope")
		if scope != "" && !safeDir(scope) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_snippets.scopeInvalide"))
			return
		}
		archive, fileCount, err := exportSnippets(scope)
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		name := "commandes-personnelles.zip"
		if scope != "" {
			name = "commandes" + scope + ".zip"
		}
		journal("snippets-export %q : %d fichier(s)", scope, fileCount)
		responseWriter.Header().Set("Content-Type", "application/zip")
		responseWriter.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		responseWriter.Write(archive)
	})
	mux.HandleFunc("POST /v1/snippets/imports", func(responseWriter http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(responseWriter, request.Body, snippetUploadMax)
		if request.ParseMultipartForm(snippetUploadMax) != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_snippets.envoiInvalideOuTrop"))
			return
		}
		scope := request.FormValue("scope")
		if scope != "" && !safeDir(scope) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_snippets.scopeInvalide"))
			return
		}
		var files []ImportFile
		for _, fileHeader := range request.MultipartForm.File["files"] {
			uploadedFile, openErr := fileHeader.Open()
			if openErr != nil {
				continue
			}
			content, _ := io.ReadAll(uploadedFile)
			uploadedFile.Close()
			files = append(files, ImportFile{fileHeader.Filename, content})
		}
		done, skipped, err := importSnippets(scope, files, request.FormValue("overwrite") == "1")
		journal("snippets-import %q : %d importé(s), %d déjà présent(s) : %v", scope, len(done), len(skipped), errOr(err))
		message := L("srv.snippets.imported", "A1", len(done))
		if len(skipped) > 0 {
			message += L("srv.snippets.skipped", "A1", len(skipped), "A2", strings.Join(skipped, ", "))
		}
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", message+"\n"+err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": message})
	})
	// Mémoire : liste, lecture, enregistrement et suppression des seuls fichiers reconnus (memory.go).
}
