package main

import (
	"context"
	"encoding/json"
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
	mux.HandleFunc("GET /memory-list", func(w http.ResponseWriter, r *http.Request) {
		g, p, t := memList()
		writeJSON(w, 200, map[string]any{"global": g, "projects": p, "targets": t})
	})
	mux.HandleFunc("GET /memory-file", func(w http.ResponseWriter, r *http.Request) {
		t, err := memRead(r.URL.Query().Get("key"))
		if errors.Is(err, os.ErrNotExist) { // nouvelle entrée pas encore enregistrée : pas une erreur
			writeJSON(w, 200, map[string]any{"text": "", "missing": true})
			return
		}
		if err != nil {
			writeJSON(w, 404, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"text": t})
	})
	mux.HandleFunc("POST /memory-save", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key, Text string
			Index     bool // nouvelle entrée : l'ajouter aussi à MEMORY.md
		}
		if json.NewDecoder(io.LimitReader(r.Body, memMax+1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		err := memSave(req.Key, req.Text)
		if err == nil && req.Index {
			err = memIndex(req.Key, req.Text)
		}
		journal("memory-save %s (%d octets) : %v", req.Key, len(req.Text), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_sessions.enregistre")})
	})
	mux.HandleFunc("POST /memory-move", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key, To string
			Copy    bool
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		dst, err := memTransfer(req.Key, req.To, req.Copy)
		journal("memory-move %s vers %s (copie=%v) : %v", req.Key, dst, req.Copy, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		out := L("srv.routes_memory.moved")
		if req.Copy {
			out = L("srv.routes_skills.copie")
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": out, "key": dst})
	})
	mux.HandleFunc("POST /memory-rename", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key, Name string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		dst, err := memRename(req.Key, req.Name)
		journal("memory-rename %s vers %s : %v", req.Key, dst, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.renamed"), "key": dst})
	})
	// Brouillon rédigé par un petit modèle (memwrite.go) : rien n'est enregistré, l'éditeur s'ouvre avec le texte.
	mux.HandleFunc("POST /memory-draft", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model, Instruction, Key string
			Refs                    []string
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		text, cost, err := memWriteDraft(req.Model, req.Instruction, req.Key, req.Refs)
		journal("memory-draft %s %q réf=%v : %v (%.4f $)", req.Model, req.Key, req.Refs, errOr(err), cost)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.brouillonPret"), "text": text, "cost": cost})
	})
	// Santé, portée des règles, recherche plein texte, désactivation (memmeta.go).
	mux.HandleFunc("GET /memory-health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"issues": memHealth()})
	})
	mux.HandleFunc("POST /memory-repair", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		rm, add, err := memRepairIndex(req.Dir)
		journal("memory-repair %s : -%d +%d : %v", req.Dir, rm, add, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.indexRepareLigneS", "A1", rm, "A2", add)})
	})
	mux.HandleFunc("GET /memory-matches", func(w http.ResponseWriter, r *http.Request) {
		files, total, trunc, err := memMatches(r.URL.Query().Get("key"), r.URL.Query().Get("dir"))
		if err != nil {
			writeJSON(w, 200, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"files": files, "total": total, "truncated": trunc})
	})
	mux.HandleFunc("POST /memory-paths", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key   string
			Paths []string
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&req) != nil || !strings.HasPrefix(req.Key, "g/rules/") {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		text, err := memRead(req.Key)
		if err == nil {
			var out string
			if out, err = setPaths(text, req.Paths); err == nil && out != text {
				err = memSave(req.Key, out)
			}
		}
		journal("memory-paths %s %v : %v", req.Key, req.Paths, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.porteeEnregistree")})
	})
	mux.HandleFunc("POST /memory-type", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key, Type string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil || !(strings.HasPrefix(req.Key, "m/") || strings.HasPrefix(req.Key, "g/rules/")) {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		text, err := memRead(req.Key)
		if err == nil {
			var out string
			if out, err = setType(text, req.Type); err == nil && out != text {
				err = memSave(req.Key, out)
			}
		}
		journal("memory-type %s %q : %v", req.Key, req.Type, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.typeEnregistre")})
	})
	mux.HandleFunc("POST /memory-fix", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil || !(strings.HasPrefix(req.Key, "m/") || strings.HasPrefix(req.Key, "g/rules/")) {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
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
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.frontMatterCorrige")})
	})
	// Décompte réel des tokens (tokens.go) : quelques fichiers par appel, le navigateur boucle.
	mux.HandleFunc("POST /memory-tokens", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Keys []string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&req) != nil || len(req.Keys) == 0 || len(req.Keys) > 6 {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
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
		writeJSON(w, 200, map[string]any{"ok": len(errs) == 0, "counts": counts, "errors": errs, "cost": total})
	})
	// Analyse par Haiku des doublons, contradictions et entrées périmées (memlint.go) : rien n'est modifié.
	mux.HandleFunc("POST /memory-lint", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Scope string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil || req.Scope == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		findings, cost, err := memLint(req.Scope)
		journal("memory-lint %s : %d constat(s) (%.4f $) : %v", req.Scope, len(findings), cost, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error(), "cost": cost})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "findings": findings, "cost": cost})
	})
	// Ce que Claude a réellement chargé (loaded.go), d'après le hook InstructionsLoaded : par fichier, et par session.
	mux.HandleFunc("GET /memory-loaded", func(w http.ResponseWriter, r *http.Request) {
		ev := readLoaded()
		writeJSON(w, 200, map[string]any{"hook": len(ev) > 0, "files": loadsByFile(ev)})
	})
	mux.HandleFunc("GET /session-loaded", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if !idRe.MatchString(id) {
			writeJSON(w, 400, map[string]any{})
			return
		}
		ev := loadsOfSession(readLoaded(), id)
		if ev == nil {
			ev = []loadEvent{}
		}
		writeJSON(w, 200, map[string]any{"files": ev})
	})
	mux.HandleFunc("GET /session-loaded-file", func(w http.ResponseWriter, r *http.Request) {
		id, file := r.URL.Query().Get("id"), r.URL.Query().Get("file")
		if !idRe.MatchString(id) {
			writeJSON(w, 400, map[string]any{})
			return
		}
		// Seuls les fichiers que le hook a vus charger dans cette session se lisent : pas de lecture de chemin arbitraire.
		known := false
		for _, e := range loadsOfSession(readLoaded(), id) {
			known = known || e.File == file
		}
		real, rerr := filepath.EvalSymlinks(file)
		if !known || rerr != nil || !loadedReadable(real) {
			writeJSON(w, 404, map[string]any{})
			return
		}
		f, err := openTrusted(file)
		if err != nil {
			writeJSON(w, 404, map[string]any{})
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() > memMax {
			writeJSON(w, 404, map[string]any{})
			return
		}
		b, err := io.ReadAll(io.LimitReader(f, memMax+1))
		if err != nil || len(b) > memMax || !utf8.Valid(b) {
			writeJSON(w, 404, map[string]any{})
			return
		}
		writeJSON(w, 200, map[string]any{"text": string(b)})
	})
	mux.HandleFunc("GET /memory-search", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"hits": memSearch(r.URL.Query().Get("q"))})
	})
	mux.HandleFunc("POST /memory-toggle", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		dst, err := memToggle(req.Key)
		journal("memory-toggle %s : %v", req.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		out := L("srv.routes_memory.ruleOff")
		if !strings.HasSuffix(dst, ".off") {
			out = L("srv.routes_memory.ruleOn")
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": out, "key": dst})
	})
	mux.HandleFunc("POST /memory-rm", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		err := memRemove(req.Key)
		journal("memory-rm %s (copie à la corbeille) : %v", req.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.supprime")})
	})
	// Sessions ouvertes sauvegardées (tabs.go) : liste, sauvegarde manuelle (nom facultatif), suppression.
	mux.HandleFunc("POST /memory-merge", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Keys   []string
			Out    string // document de sortie : l'une des clés (vide : la première)
			Launch bool
			Args   string
			Prompt string // texte relu et modifié dans la fenêtre de saisie : remplace celui du serveur
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		prompt, cwd, err := mergePrompt(req.Keys, req.Out)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		if req.Prompt != "" {
			if len(req.Prompt) > 32<<10 || !utf8.ValidString(req.Prompt) || strings.ContainsRune(req.Prompt, 0) {
				writeJSON(w, 200, map[string]any{"ok": false, "out": L("srv.routes_memory.promptTropLongOu")})
				return
			}
			prompt = req.Prompt
		}
		if !req.Launch {
			writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.promptCopie"), "prompt": prompt})
			return
		}
		args, err := claudeArgs(req.Args)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error(), "prompt": prompt})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		out, err := matin(ctx, append(args, "--prompt", prompt, cwd)...)
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": out, "prompt": prompt})
	})
	// Conversation dans le navigateur (chat.go).
	mux.HandleFunc("GET /trash", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": trashList()})
	})
	mux.HandleFunc("GET /trash-file", func(w http.ResponseWriter, r *http.Request) {
		_, text, err := trashItem(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, 404, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("POST /trash-restore", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID string }
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		t, err := trashRestore(req.ID)
		journal("trash-restore %s → %s : %v", req.ID, t.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.restaure", "A1", t.Path), "key": t.Key})
	})
	mux.HandleFunc("POST /trash-purge", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID  string
			All bool // vider toute la corbeille : jamais implicite
		}
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		if req.ID == "" && !req.All {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.routes_memory.preciseUnElementOu")})
			return
		}
		err := trashPurge(req.ID)
		journal("trash-purge %q : %v", req.ID, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_memory.corbeilleVidee")})
	})
	// Groupes virtuels et favoris : le navigateur envoie l'ensemble, le serveur le valide et le remplace.
}
