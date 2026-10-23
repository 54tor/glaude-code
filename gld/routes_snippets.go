package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
)

// routesSnippets : snippets (commandes Claude Code) personnels et de projet.
func routesSnippets(mux *http.ServeMux) {
	snipDir = env("GLD_WEB_SNIPPETS", os.Getenv("CC_WEB_SNIPPETS"))
	mux.HandleFunc("GET /snippet-list", func(w http.ResponseWriter, r *http.Request) {
		l, scopes := listSnippets()
		writeJSON(w, 200, map[string]any{"dir": userCmdDir(), "files": l, "scopes": scopes})
	})
	mux.HandleFunc("GET /snippet", func(w http.ResponseWriter, r *http.Request) {
		t, err := readSnippet(r.URL.Query().Get("key"))
		if errors.Is(err, os.ErrNotExist) { // nouveau snippet pas encore enregistré
			writeJSON(w, 200, map[string]any{"text": "", "missing": true})
			return
		}
		if err != nil {
			writeJSON(w, 404, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"text": t})
	})
	mux.HandleFunc("POST /snippet-save", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key, Text string
			Create    bool // nouveau snippet : refusé s'il existe déjà
		}
		if json.NewDecoder(io.LimitReader(r.Body, snippetMax+1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		err := saveSnippet(req.Key, req.Text, req.Create)
		journal("snippet-save %s (%d octets) : %v", req.Key, len(req.Text), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_snippets.snippetEnregistre")})
	})
	mux.HandleFunc("POST /snippet-draft", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model, Instruction, Text string }
		if json.NewDecoder(io.LimitReader(r.Body, snippetMax+16<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		text, cost, err := snippetDraft(req.Model, req.Instruction, req.Text)
		journal("snippet-draft %s (%d caractères) : %v (%.4f $)", req.Model, len(req.Text), errOr(err), cost)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_snippets.miseEnFormePrete"), "text": text, "cost": cost})
	})
	mux.HandleFunc("POST /snippet-rm", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		err := removeSnippet(req.Key)
		journal("snippet-rm %s (copie à la corbeille) : %v", req.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_snippets.snippetSupprime")})
	})
	// Export : les snippets d'un scope en archive zip ; import : fichiers .md ou archives .zip (multipart).
	mux.HandleFunc("GET /snippets-export", func(w http.ResponseWriter, r *http.Request) {
		scope := r.URL.Query().Get("scope")
		if scope != "" && !safeDir(scope) {
			http.Error(w, L("srv.routes_snippets.scopeInvalide"), 400)
			return
		}
		b, n, err := exportSnippets(scope)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		name := "commandes-personnelles.zip"
		if scope != "" {
			name = "commandes" + scope + ".zip"
		}
		journal("snippets-export %q : %d fichier(s)", scope, n)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Write(b)
	})
	mux.HandleFunc("POST /snippets-import", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
		if r.ParseMultipartForm(16<<20) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.routes_snippets.envoiInvalideOuTrop")})
			return
		}
		scope := r.FormValue("scope")
		if scope != "" && !safeDir(scope) {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.routes_snippets.scopeInvalide")})
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
			out += "\n" + err.Error()
		}
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": out})
	})
	// Mémoire : liste, lecture, enregistrement et suppression des seuls fichiers reconnus (memory.go).
}
