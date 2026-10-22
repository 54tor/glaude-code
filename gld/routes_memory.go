package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
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
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Enregistré"})
	})
	mux.HandleFunc("POST /memory-move", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key, To string
			Copy    bool
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		dst, err := memTransfer(req.Key, req.To, req.Copy)
		journal("memory-move %s vers %s (copie=%v) : %v", req.Key, dst, req.Copy, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		out := "Déplacé"
		if req.Copy {
			out = "Copié"
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": out, "key": dst})
	})
	// Brouillon rédigé par un petit modèle (memwrite.go) : rien n'est enregistré, l'éditeur s'ouvre avec le texte.
	mux.HandleFunc("POST /memory-draft", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model, Instruction, Key string
			Refs                    []string
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		text, cost, err := memWriteDraft(req.Model, req.Instruction, req.Key, req.Refs)
		journal("memory-draft %s %q réf=%v : %v (%.4f $)", req.Model, req.Key, req.Refs, errOr(err), cost)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Brouillon prêt", "text": text, "cost": cost})
	})
	// Santé, portée des règles, recherche plein texte, désactivation (memmeta.go).
	mux.HandleFunc("GET /memory-health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"issues": memHealth()})
	})
	mux.HandleFunc("POST /memory-repair", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		rm, add, err := memRepairIndex(req.Dir)
		journal("memory-repair %s : -%d +%d : %v", req.Dir, rm, add, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": fmt.Sprintf("Index réparé : %d ligne(s) retirée(s), %d ajoutée(s)", rm, add)})
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
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
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
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Portée enregistrée"})
	})
	mux.HandleFunc("POST /memory-type", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key, Type string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil || !(strings.HasPrefix(req.Key, "m/") || strings.HasPrefix(req.Key, "g/rules/")) {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
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
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Type enregistré"})
	})
	// Décompte réel des tokens (tokens.go) : quelques fichiers par appel, le navigateur boucle.
	mux.HandleFunc("POST /memory-tokens", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Keys []string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&req) != nil || len(req.Keys) == 0 || len(req.Keys) > 6 {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
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
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
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
	mux.HandleFunc("GET /memory-search", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"hits": memSearch(r.URL.Query().Get("q"))})
	})
	mux.HandleFunc("POST /memory-toggle", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		dst, err := memToggle(req.Key)
		journal("memory-toggle %s : %v", req.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		out := "Règle désactivée"
		if !strings.HasSuffix(dst, ".off") {
			out = "Règle réactivée"
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": out, "key": dst})
	})
	mux.HandleFunc("POST /memory-rm", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		err := memRemove(req.Key)
		journal("memory-rm %s (copie à la corbeille) : %v", req.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Supprimé"})
	})
	// Sessions ouvertes sauvegardées (tabs.go) : liste, sauvegarde manuelle (nom facultatif), suppression.
	mux.HandleFunc("POST /memory-merge", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Keys   []string
			Launch bool
			Args   string
			Prompt string // texte relu et modifié dans la fenêtre de saisie : remplace celui du serveur
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		prompt, cwd, err := mergePrompt(req.Keys)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		if req.Prompt != "" {
			if len(req.Prompt) > 32<<10 || !utf8.ValidString(req.Prompt) || strings.ContainsRune(req.Prompt, 0) {
				writeJSON(w, 200, map[string]any{"ok": false, "out": "prompt trop long ou invalide"})
				return
			}
			prompt = req.Prompt
		}
		if !req.Launch {
			writeJSON(w, 200, map[string]any{"ok": true, "out": "Prompt copié", "prompt": prompt})
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
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Restauré : " + t.Path, "key": t.Key})
	})
	mux.HandleFunc("POST /trash-purge", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID  string
			All bool // vider toute la corbeille : jamais implicite
		}
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
		if req.ID == "" && !req.All {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "précise un élément, ou all: true pour tout vider"})
			return
		}
		err := trashPurge(req.ID)
		journal("trash-purge %q : %v", req.ID, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Corbeille vidée"})
	})
	// Groupes virtuels et favoris : le navigateur envoie l'ensemble, le serveur le valide et le remplace.
}
