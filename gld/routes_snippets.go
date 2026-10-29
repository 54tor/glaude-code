package main

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
)

// routesSnippets : snippets (commandes Claude Code) personnels et de projet.
func routesSnippets(mux *http.ServeMux) {
	snipDir = env("GLD_WEB_SNIPPETS", os.Getenv("CC_WEB_SNIPPETS"))
	mux.HandleFunc("GET /v1/snippets", func(w http.ResponseWriter, r *http.Request) {
		l, scopes := listSnippets()
		writeJSON(w, 200, map[string]any{"dir": userCmdDir(), "files": l, "scopes": scopes})
	})
	mux.HandleFunc("GET /v1/snippets/{key}", func(w http.ResponseWriter, r *http.Request) {
		t, err := readSnippet(r.PathValue("key"))
		if errors.Is(err, os.ErrNotExist) { // nouveau snippet pas encore enregistré
			writeJSON(w, 200, map[string]any{"text": "", "missing": true})
			return
		}
		if err != nil {
			writeErr(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"text": t})
	})
	mux.HandleFunc("PUT /v1/snippets/{key}", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Text   string
			Create bool // nouveau snippet : refusé s'il existe déjà
		}
		if !decodeBody(w, r, &req, snippetMax+1<<10) {
			return
		}
		key := r.PathValue("key")
		err := saveSnippet(key, req.Text, req.Create)
		journal("snippet-save %s (%d octets) : %v", key, len(req.Text), errOr(err))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_snippets.snippetEnregistre")})
	})
	mux.HandleFunc("POST /v1/snippets/drafts", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model, Instruction, Text string }
		if !decodeBody(w, r, &req, snippetMax+16<<10) {
			return
		}
		text, cost, err := snippetDraft(req.Model, req.Instruction, req.Text)
		journal("snippet-draft %s (%d caractères) : %v (%.4f $)", req.Model, len(req.Text), errOr(err), cost)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_snippets.miseEnFormePrete"), "text": text, "cost": cost})
	})
	mux.HandleFunc("PATCH /v1/snippets/{key}", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ To string }
		if !decodeBody(w, r, &req, 1<<12) {
			return
		}
		from := r.PathValue("key")
		err := renameSnippet(from, req.To)
		journal("snippet-rename %s -> %s : %v", from, req.To, errOr(err))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_snippets.snippetRenomme")})
	})
	mux.HandleFunc("DELETE /v1/snippets", func(w http.ResponseWriter, r *http.Request) {
		keys := r.URL.Query()["item"]
		if len(keys) == 0 {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		if len(keys) > itemMax {
			writeErr(w, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
			return
		}
		for _, key := range keys {
			err := removeSnippet(key)
			journal("snippet-rm %s (copie à la corbeille) : %v", key, errOr(err))
			if err != nil {
				writeErr(w, http.StatusConflict, "conflict", err.Error())
				return
			}
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_snippets.snippetSupprime")})
	})
	// Export : les snippets d'un scope en archive zip ; import : fichiers .md ou archives .zip (multipart).
	mux.HandleFunc("GET /v1/snippets/export", func(w http.ResponseWriter, r *http.Request) {
		scope := r.URL.Query().Get("scope")
		if scope != "" && !safeDir(scope) {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_snippets.scopeInvalide"))
			return
		}
		b, n, err := exportSnippets(scope)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		name := "commandes-personnelles.zip"
		if scope != "" {
			name = "commandes" + scope + ".zip"
		}
		journal("snippets-export %q : %d fichier(s)", scope, n)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		w.Write(b)
	})
	mux.HandleFunc("POST /v1/snippets/imports", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
		if r.ParseMultipartForm(16<<20) != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_snippets.envoiInvalideOuTrop"))
			return
		}
		scope := r.FormValue("scope")
		if scope != "" && !safeDir(scope) {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_snippets.scopeInvalide"))
			return
		}
		var files []ImportFile
		for _, fh := range r.MultipartForm.File["files"] {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(f)
			f.Close()
			files = append(files, ImportFile{fh.Filename, b})
		}
		done, skipped, err := importSnippets(scope, files, r.FormValue("overwrite") == "1")
		journal("snippets-import %q : %d importé(s), %d déjà présent(s) : %v", scope, len(done), len(skipped), errOr(err))
		out := L("srv.snippets.imported", "A1", len(done))
		if len(skipped) > 0 {
			out += L("srv.snippets.skipped", "A1", len(skipped), "A2", strings.Join(skipped, ", "))
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", out+"\n"+err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": out})
	})
	// Mémoire : liste, lecture, enregistrement et suppression des seuls fichiers reconnus (memory.go).
}
