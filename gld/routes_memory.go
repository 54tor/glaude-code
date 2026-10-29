package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// routesMemory : la zone Mémoire et sa corbeille.
func routesMemory(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/memory/files", func(w http.ResponseWriter, r *http.Request) {
		g, p, t := memList()
		writeJSON(w, 200, map[string]any{"global": g, "projects": p, "targets": t})
	})
	mux.HandleFunc("GET /v1/memory/search", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"hits": memSearch(r.URL.Query().Get("q"))})
	})
	mux.HandleFunc("GET /v1/memory/files/{key}", func(w http.ResponseWriter, r *http.Request) {
		t, err := memRead(r.PathValue("key"))
		if errors.Is(err, os.ErrNotExist) { // nouvelle entrée pas encore enregistrée : pas une erreur
			writeJSON(w, 200, map[string]any{"text": "", "missing": true})
			return
		}
		if err != nil {
			writeErr(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"text": t})
	})
	mux.HandleFunc("PUT /v1/memory/files/{key}", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Text  string
			Index bool // nouvelle entrée : l'ajouter aussi à MEMORY.md
		}
		if !decodeBody(w, r, &req, memMax+1<<10) {
			return
		}
		key := r.PathValue("key")
		err := memSave(key, req.Text)
		if err == nil && req.Index {
			err = memIndex(key, req.Text)
		}
		journal("memory-save %s (%d octets) : %v", key, len(req.Text), errOr(err))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_sessions.enregistre")})
	})
	// Une seule modification par appel : renommer, déplacer ou copier, activer ou désactiver, type, portée.
	mux.HandleFunc("PATCH /v1/memory/files/{key}", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name, To, Type *string
			Copy           bool
			Enabled        *bool
			Paths          *[]string
		}
		if !decodeBody(w, r, &req, 1<<14) {
			return
		}
		key := r.PathValue("key")
		n := 0
		for _, set := range []bool{req.Name != nil, req.To != nil, req.Type != nil, req.Enabled != nil, req.Paths != nil} {
			if set {
				n++
			}
		}
		bad := func() { writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide")) }
		fail := func(status int, code string, err error) { writeErr(w, status, code, err.Error()) }
		switch {
		case n != 1:
			bad()
		case req.Name != nil:
			dst, err := memRename(key, *req.Name)
			journal("memory-rename %s vers %s : %v", key, dst, errOr(err))
			if err != nil {
				fail(http.StatusConflict, "conflict", err)
				return
			}
			writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.renamed"), "key": dst})
		case req.To != nil:
			dst, err := memTransfer(key, *req.To, req.Copy)
			journal("memory-move %s vers %s (copie=%v) : %v", key, dst, req.Copy, errOr(err))
			if err != nil {
				fail(http.StatusConflict, "conflict", err)
				return
			}
			out := L("srv.routes_memory.moved")
			if req.Copy {
				out = L("srv.routes_skills.copie")
			}
			writeJSON(w, 200, map[string]any{"message": out, "key": dst})
		case req.Enabled != nil:
			dst := key
			if *req.Enabled == strings.HasSuffix(key, ".off") {
				var err error
				if dst, err = memToggle(key); err != nil {
					journal("memory-toggle %s : %v", key, err)
					fail(http.StatusConflict, "conflict", err)
					return
				}
			}
			journal("memory-toggle %s : %v", key, errOr(nil))
			out := L("srv.routes_memory.ruleOff")
			if !strings.HasSuffix(dst, ".off") {
				out = L("srv.routes_memory.ruleOn")
			}
			writeJSON(w, 200, map[string]any{"message": out, "key": dst})
		case req.Type != nil:
			if !(strings.HasPrefix(key, "m/") || strings.HasPrefix(key, "g/rules/")) {
				bad()
				return
			}
			text, err := memRead(key)
			if err == nil {
				var out string
				if out, err = setType(text, *req.Type); err == nil && out != text {
					err = memSave(key, out)
				}
			}
			journal("memory-type %s %q : %v", key, *req.Type, errOr(err))
			if err != nil {
				fail(http.StatusBadRequest, "bad_request", err)
				return
			}
			writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.typeEnregistre")})
		default:
			if !strings.HasPrefix(key, "g/rules/") {
				bad()
				return
			}
			text, err := memRead(key)
			if err == nil {
				var out string
				if out, err = setPaths(text, *req.Paths); err == nil && out != text {
					err = memSave(key, out)
				}
			}
			journal("memory-paths %s %v : %v", key, *req.Paths, errOr(err))
			if err != nil {
				fail(http.StatusBadRequest, "bad_request", err)
				return
			}
			writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.porteeEnregistree")})
		}
	})
	// Un ou plusieurs fichiers vers la corbeille : ?item=<clé> répété.
	mux.HandleFunc("DELETE /v1/memory/files", func(w http.ResponseWriter, r *http.Request) {
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
			err := memRemove(key)
			journal("memory-rm %s (copie à la corbeille) : %v", key, errOr(err))
			if err != nil {
				status, code := http.StatusConflict, "conflict"
				if errors.Is(err, os.ErrNotExist) {
					status, code = http.StatusNotFound, "not_found"
				}
				writeErr(w, status, code, err.Error())
				return
			}
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.supprime")})
	})
	// Brouillon rédigé par un petit modèle (memwrite.go) : rien n'est enregistré, l'éditeur s'ouvre avec le texte.
	mux.HandleFunc("POST /v1/memory/drafts", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model, Instruction, Key string
			Refs                    []string
		}
		if !decodeBody(w, r, &req, 1<<16) {
			return
		}
		text, cost, err := memWriteDraft(req.Model, req.Instruction, req.Key, req.Refs)
		journal("memory-draft %s %q réf=%v : %v (%.4f $)", req.Model, req.Key, req.Refs, errOr(err), cost)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.brouillonPret"), "text": text, "cost": cost})
	})
	// Santé, portée des règles, recherche plein texte, désactivation (memmeta.go).
	mux.HandleFunc("GET /v1/memory/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"issues": memHealth()})
	})
	mux.HandleFunc("POST /v1/memory/repairs", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir string }
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		rm, add, err := memRepairIndex(req.Dir)
		journal("memory-repair %s : -%d +%d : %v", req.Dir, rm, add, errOr(err))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.indexRepareLigneS", "A1", rm, "A2", add)})
	})
	mux.HandleFunc("GET /v1/memory/matches", func(w http.ResponseWriter, r *http.Request) {
		files, total, trunc, err := memMatches(r.URL.Query().Get("key"), r.URL.Query().Get("dir"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"files": files, "total": total, "truncated": trunc})
	})
	mux.HandleFunc("POST /v1/memory/fixes", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key string }
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		if !(strings.HasPrefix(req.Key, "m/") || strings.HasPrefix(req.Key, "g/rules/")) {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		text, err := memRead(req.Key)
		if err == nil {
			var out string
			if out, err = fixFrontMatter(text); err == nil {
				err = memSave(req.Key, out)
			}
		}
		journal("memory-fix %s : %v", req.Key, errOr(err))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.frontMatterCorrige")})
	})
	// Décompte réel des tokens (tokens.go) : quelques fichiers par appel, le navigateur boucle.
	mux.HandleFunc("POST /v1/memory/token-counts", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Keys []string }
		if !decodeBody(w, r, &req, 1<<14) {
			return
		}
		if len(req.Keys) == 0 || len(req.Keys) > 6 {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		counts, errs, total := map[string]int{}, map[string]string{}, 0.0
		for _, k := range req.Keys {
			text, err := loadedText(k)
			if err == nil {
				var n int
				var c float64
				if n, c, _, err = countTokens(text); err == nil {
					counts[k] = n
					total += c
				}
			}
			if err != nil {
				errs[k] = err.Error()
			}
		}
		journal("memory-tokens %d fichier(s) : %.4f $", len(req.Keys), total)
		writeJSON(w, 200, map[string]any{"counts": counts, "errors": errs, "cost": total})
	})
	// Analyse par Haiku des doublons, contradictions et entrées périmées (memlint.go) : rien n'est modifié.
	mux.HandleFunc("POST /v1/memory/lints", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Scope string }
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		if req.Scope == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		findings, cost, err := memLint(req.Scope)
		journal("memory-lint %s : %d constat(s) (%.4f $) : %v", req.Scope, len(findings), cost, errOr(err))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"findings": findings, "cost": cost})
	})
	// Ce que Claude a réellement chargé (loaded.go), d'après le hook InstructionsLoaded : par fichier, et par session.
	mux.HandleFunc("GET /v1/memory/loaded", func(w http.ResponseWriter, r *http.Request) {
		ev := readLoaded()
		writeJSON(w, 200, map[string]any{"hook": len(ev) > 0, "files": loadsByFile(ev)})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/loaded", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		ev := loadsOfSession(readLoaded(), id)
		if ev == nil {
			ev = []loadEvent{}
		}
		writeJSON(w, 200, map[string]any{"files": ev})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/loaded/files/{file}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		file := r.PathValue("file")
		// Seuls les fichiers que le hook a vus charger dans cette session se lisent : pas de lecture de chemin arbitraire.
		known := false
		for _, e := range loadsOfSession(readLoaded(), id) {
			known = known || e.File == file
		}
		real, rerr := filepath.EvalSymlinks(file)
		if !known || rerr != nil || !loadedReadable(real) {
			notFound(w)
			return
		}
		f, err := openTrusted(file)
		if err != nil {
			notFound(w)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() > memMax {
			notFound(w)
			return
		}
		b, err := io.ReadAll(io.LimitReader(f, memMax+1))
		if err != nil || len(b) > memMax || !utf8.Valid(b) {
			notFound(w)
			return
		}
		writeJSON(w, 200, map[string]any{"text": string(b)})
	})
	mux.HandleFunc("POST /v1/memory/merges", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Keys   []string
			Out    string // document de sortie : l'une des clés (vide : la première)
			Launch bool
			Args   string
			Prompt string // texte relu et modifié dans la fenêtre de saisie : remplace celui du serveur
		}
		if !decodeBody(w, r, &req, 1<<16) {
			return
		}
		prompt, cwd, err := mergePrompt(req.Keys, req.Out)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if req.Prompt != "" {
			if len(req.Prompt) > 32<<10 || !utf8.ValidString(req.Prompt) || strings.ContainsRune(req.Prompt, 0) {
				writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_memory.promptTropLongOu"))
				return
			}
			prompt = req.Prompt
		}
		if !req.Launch {
			writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.promptCopie"), "prompt": prompt})
			return
		}
		args, err := claudeArgs(req.Args)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		out, err := matin(ctx, append(args, "--prompt", prompt, cwd)...)
		if err != nil {
			msg := strings.TrimSpace(out)
			if msg == "" {
				msg = err.Error()
			}
			writeErr(w, http.StatusInternalServerError, "internal", msg)
			return
		}
		writeJSON(w, 200, map[string]any{"message": out, "prompt": prompt})
	})
	// Conversation dans le navigateur (chat.go).
	mux.HandleFunc("GET /v1/trash/memory", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": trashList()})
	})
	mux.HandleFunc("GET /v1/trash/memory/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		_, text, err := trashItem(r.PathValue("id"))
		if err != nil {
			writeErr(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("PATCH /v1/trash/memory/{id}", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Trashed *bool }
		if !decodeBody(w, r, &req, 1<<12) {
			return
		}
		if req.Trashed == nil || *req.Trashed {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		id := r.PathValue("id")
		t, err := trashRestore(id)
		journal("trash-restore %s → %s : %v", id, t.Key, errOr(err))
		if err != nil {
			writeErr(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.restaure", "A1", t.Path), "key": t.Key})
	})
	// ?item=<id> : un élément ; ?all=1 : toute la corbeille, jamais implicite.
	mux.HandleFunc("DELETE /v1/trash/memory", func(w http.ResponseWriter, r *http.Request) {
		id, all := r.URL.Query().Get("item"), r.URL.Query().Get("all") == "1"
		if (id == "") == !all {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_memory.preciseUnElementOu"))
			return
		}
		err := trashPurge(id)
		journal("trash-purge %q : %v", id, errOr(err))
		if err != nil {
			writeErr(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_memory.corbeilleVidee")})
	})
	// Groupes virtuels et favoris : le navigateur envoie l'ensemble, le serveur le valide et le remplace.
}
