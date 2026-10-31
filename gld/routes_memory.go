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

const (
	tokenCountBatchMax     = 6
	promptOverrideMaxBytes = 32 << 10
)

// routesMemory : la zone Mémoire et sa corbeille.
func routesMemory(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/memory/files", func(responseWriter http.ResponseWriter, request *http.Request) {
		globalFiles, projects, targets := memList()
		writeJSON(responseWriter, 200, map[string]any{"global": globalFiles, "projects": projects, "targets": targets})
	})
	mux.HandleFunc("GET /v1/memory/search", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, 200, map[string]any{"hits": memSearch(request.URL.Query().Get("q"))})
	})
	mux.HandleFunc("GET /v1/memory/files/{key}", func(responseWriter http.ResponseWriter, request *http.Request) {
		text, err := memRead(request.PathValue("key"))
		if errors.Is(err, os.ErrNotExist) { // nouvelle entrée pas encore enregistrée : pas une erreur
			writeJSON(responseWriter, 200, map[string]any{"text": "", "missing": true})
			return
		}
		if err != nil {
			writeErr(responseWriter, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("PUT /v1/memory/files/{key}", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Text  string
			Index bool // nouvelle entrée : l'ajouter aussi à MEMORY.md
		}
		if !decodeBody(responseWriter, request, &body, memMax+1<<10) {
			return
		}
		key := request.PathValue("key")
		err := memSave(key, body.Text)
		if err == nil && body.Index {
			err = memIndex(key, body.Text)
		}
		journal("memory-save %s (%d octets) : %v", key, len(body.Text), errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_sessions.enregistre")})
	})
	// Une seule modification par appel : renommer, déplacer ou copier, activer ou désactiver, type, portée.
	mux.HandleFunc("PATCH /v1/memory/files/{key}", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Name, To, Type *string
			Copy           bool
			Enabled        *bool
			Paths          *[]string
		}
		if !decodeBody(responseWriter, request, &body, 1<<14) {
			return
		}
		key := request.PathValue("key")
		changeCount := 0
		for _, isSet := range []bool{body.Name != nil, body.To != nil, body.Type != nil, body.Enabled != nil, body.Paths != nil} {
			if isSet {
				changeCount++
			}
		}
		replyBadRequest := func() {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
		}
		replyFailure := func(status int, code string, err error) { writeErr(responseWriter, status, code, err.Error()) }
		switch {
		case changeCount != 1:
			replyBadRequest()
		case body.Name != nil:
			destinationKey, err := memRename(key, *body.Name)
			journal("memory-rename %s vers %s : %v", key, destinationKey, errOr(err))
			if err != nil {
				replyFailure(http.StatusConflict, "conflict", err)
				return
			}
			writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_memory.renamed"), "key": destinationKey})
		case body.To != nil:
			destinationKey, err := memTransfer(key, *body.To, body.Copy)
			journal("memory-move %s vers %s (copie=%v) : %v", key, destinationKey, body.Copy, errOr(err))
			if err != nil {
				replyFailure(http.StatusConflict, "conflict", err)
				return
			}
			message := L("srv.routes_memory.moved")
			if body.Copy {
				message = L("srv.routes_skills.copie")
			}
			writeJSON(responseWriter, 200, map[string]any{"message": message, "key": destinationKey})
		case body.Enabled != nil:
			destinationKey := key
			if *body.Enabled == strings.HasSuffix(key, ".off") {
				var err error
				if destinationKey, err = memToggle(key); err != nil {
					journal("memory-toggle %s : %v", key, err)
					replyFailure(http.StatusConflict, "conflict", err)
					return
				}
			}
			journal("memory-toggle %s : %v", key, errOr(nil))
			message := L("srv.routes_memory.ruleOff")
			if !strings.HasSuffix(destinationKey, ".off") {
				message = L("srv.routes_memory.ruleOn")
			}
			writeJSON(responseWriter, 200, map[string]any{"message": message, "key": destinationKey})
		case body.Type != nil:
			if !(strings.HasPrefix(key, "m/") || strings.HasPrefix(key, "g/rules/")) {
				replyBadRequest()
				return
			}
			text, err := memRead(key)
			if err == nil {
				var updatedText string
				if updatedText, err = setType(text, *body.Type); err == nil && updatedText != text {
					err = memSave(key, updatedText)
				}
			}
			journal("memory-type %s %q : %v", key, *body.Type, errOr(err))
			if err != nil {
				replyFailure(http.StatusBadRequest, "bad_request", err)
				return
			}
			writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_memory.typeEnregistre")})
		default:
			if !strings.HasPrefix(key, "g/rules/") {
				replyBadRequest()
				return
			}
			text, err := memRead(key)
			if err == nil {
				var updatedText string
				if updatedText, err = setPaths(text, *body.Paths); err == nil && updatedText != text {
					err = memSave(key, updatedText)
				}
			}
			journal("memory-paths %s %v : %v", key, *body.Paths, errOr(err))
			if err != nil {
				replyFailure(http.StatusBadRequest, "bad_request", err)
				return
			}
			writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_memory.porteeEnregistree")})
		}
	})
	// Un ou plusieurs fichiers vers la corbeille : ?item=<clé> répété.
	mux.HandleFunc("DELETE /v1/memory/files", func(responseWriter http.ResponseWriter, request *http.Request) {
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
			err := memRemove(key)
			journal("memory-rm %s (copie à la corbeille) : %v", key, errOr(err))
			if err != nil {
				status, code := http.StatusConflict, "conflict"
				if errors.Is(err, os.ErrNotExist) {
					status, code = http.StatusNotFound, "not_found"
				}
				bulkReply.fail(key, status, code, err.Error())
				continue
			}
			bulkReply.ok(key)
		}
		bulkReply.reply(responseWriter, L("srv.routes_memory.supprime"))
	})
	// Brouillon rédigé par un petit modèle (memwrite.go) : rien n'est enregistré, l'éditeur s'ouvre avec le texte.
	mux.HandleFunc("POST /v1/memory/drafts", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Model, Instruction, Key string
			Refs                    []string
		}
		if !decodeBody(responseWriter, request, &body, 1<<16) {
			return
		}
		text, cost, err := memWriteDraft(body.Model, body.Instruction, body.Key, body.Refs)
		journal("memory-draft %s %q réf=%v : %v (%.4f $)", body.Model, body.Key, body.Refs, errOr(err), cost)
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_memory.brouillonPret"), "text": text, "cost": cost})
	})
	// Santé, portée des règles, recherche plein texte, désactivation (memmeta.go).
	mux.HandleFunc("GET /v1/memory/health", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, 200, map[string]any{"issues": memHealth()})
	})
	mux.HandleFunc("POST /v1/memory/repairs", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Dir string }
		if !decodeBody(responseWriter, request, &body, 1<<10) {
			return
		}
		removedCount, addedCount, err := memRepairIndex(body.Dir)
		journal("memory-repair %s : -%d +%d : %v", body.Dir, removedCount, addedCount, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_memory.indexRepareLigneS", "A1", removedCount, "A2", addedCount)})
	})
	mux.HandleFunc("GET /v1/memory/matches", func(responseWriter http.ResponseWriter, request *http.Request) {
		files, total, truncated, err := memMatches(request.URL.Query().Get("key"), request.URL.Query().Get("dir"))
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if !canFsRead(request) {
			files = []string{} // sans fs_read, le nombre seul : pas de noms de fichiers
		}
		writeJSON(responseWriter, 200, map[string]any{"files": files, "total": total, "truncated": truncated})
	})
	mux.HandleFunc("POST /v1/memory/fixes", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Key string }
		if !decodeBody(responseWriter, request, &body, 1<<10) {
			return
		}
		if !(strings.HasPrefix(body.Key, "m/") || strings.HasPrefix(body.Key, "g/rules/")) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		text, err := memRead(body.Key)
		if err == nil {
			var updatedText string
			if updatedText, err = fixFrontMatter(text); err == nil {
				err = memSave(body.Key, updatedText)
			}
		}
		journal("memory-fix %s : %v", body.Key, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_memory.frontMatterCorrige")})
	})
	// Décompte réel des tokens (tokens.go) : quelques fichiers par appel, le navigateur boucle.
	mux.HandleFunc("POST /v1/memory/token-counts", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Keys []string }
		if !decodeBody(responseWriter, request, &body, 1<<14) {
			return
		}
		if len(body.Keys) == 0 || len(body.Keys) > tokenCountBatchMax {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		counts, errs, total := map[string]int{}, map[string]string{}, 0.0
		for _, key := range body.Keys {
			text, err := loadedText(key)
			if err == nil {
				var tokenCount int
				var tokenCost float64
				if tokenCount, tokenCost, _, err = countTokens(text); err == nil {
					counts[key] = tokenCount
					total += tokenCost
				}
			}
			if err != nil {
				errs[key] = err.Error()
			}
		}
		journal("memory-tokens %d fichier(s) : %.4f $", len(body.Keys), total)
		writeJSON(responseWriter, 200, map[string]any{"counts": counts, "errors": errs, "cost": total})
	})
	// Analyse par Haiku des doublons, contradictions et entrées périmées (memlint.go) : rien n'est modifié.
	mux.HandleFunc("POST /v1/memory/lints", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Scope string }
		if !decodeBody(responseWriter, request, &body, 1<<10) {
			return
		}
		if body.Scope == "" {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		findings, cost, err := memLint(body.Scope)
		journal("memory-lint %s : %d constat(s) (%.4f $) : %v", body.Scope, len(findings), cost, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"findings": findings, "cost": cost})
	})
	// Ce que Claude a réellement chargé (loaded.go), d'après le hook InstructionsLoaded : par fichier, et par session.
	mux.HandleFunc("GET /v1/memory/loaded", func(responseWriter http.ResponseWriter, request *http.Request) {
		events := readLoaded()
		writeJSON(responseWriter, 200, map[string]any{"hook": len(events) > 0, "files": loadsByFile(events)})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/loaded", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		events := loadsOfSession(readLoaded(), sessionID)
		if events == nil {
			events = []loadEvent{}
		}
		writeJSON(responseWriter, 200, map[string]any{"files": events})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/loaded/files/{file}", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		file := request.PathValue("file")
		// Seuls les fichiers que le hook a vus charger dans cette session se lisent : pas de lecture de chemin arbitraire.
		known := false
		for _, event := range loadsOfSession(readLoaded(), sessionID) {
			known = known || event.File == file
		}
		resolvedPath, evalErr := filepath.EvalSymlinks(file)
		if !known || evalErr != nil || !loadedReadable(resolvedPath) {
			notFound(responseWriter)
			return
		}
		openedFile, err := openTrusted(file)
		if err != nil {
			notFound(responseWriter)
			return
		}
		defer openedFile.Close()
		fileInfo, err := openedFile.Stat()
		if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() > memMax {
			notFound(responseWriter)
			return
		}
		content, err := io.ReadAll(io.LimitReader(openedFile, memMax+1))
		if err != nil || len(content) > memMax || !utf8.Valid(content) {
			notFound(responseWriter)
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"text": string(content)})
	})
	mux.HandleFunc("POST /v1/memory/merges", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Keys   []string
			Out    string // document de sortie : l'une des clés (vide : la première)
			Launch bool
			Args   string
			Prompt string // texte relu et modifié dans la fenêtre de saisie : remplace celui du serveur
		}
		if !decodeBody(responseWriter, request, &body, 1<<16) {
			return
		}
		prompt, cwd, err := mergePrompt(body.Keys, body.Out)
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		mergeReply(responseWriter, request, prompt, cwd, body.Prompt, body.Launch, body.Args)
	})
	// Conversation dans le navigateur (chat.go).
	mux.HandleFunc("GET /v1/trash/memory", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, 200, map[string]any{"items": trashList()})
	})
	mux.HandleFunc("GET /v1/trash/memory/{id}/file", func(responseWriter http.ResponseWriter, request *http.Request) {
		_, text, err := trashItem(request.PathValue("id"))
		if err != nil {
			writeErr(responseWriter, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("PATCH /v1/trash/memory/{id}", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Trashed *bool }
		if !decodeBody(responseWriter, request, &body, 1<<12) {
			return
		}
		if body.Trashed == nil || *body.Trashed {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		trashID := request.PathValue("id")
		restored, err := trashRestore(trashID)
		journal("trash-restore %s → %s : %v", trashID, restored.Key, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_memory.restaure", "A1", restored.Path), "key": restored.Key})
	})
	// ?item=<id> : un élément ; ?all=1 : toute la corbeille, jamais implicite.
	mux.HandleFunc("DELETE /v1/trash/memory", func(responseWriter http.ResponseWriter, request *http.Request) {
		trashID, purgeAll := request.URL.Query().Get("item"), request.URL.Query().Get("all") == "1"
		if (trashID == "") == !purgeAll {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_memory.preciseUnElementOu"))
			return
		}
		err := trashPurge(trashID)
		journal("trash-purge %q : %v", trashID, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_memory.corbeilleVidee")})
	})
	// Groupes virtuels et favoris : le navigateur envoie l'ensemble, le serveur le valide et le remplace.
}

// mergeReply : renvoie le prompt de fusion, ou le lance dans une nouvelle session (mémoire et snippets).
func mergeReply(responseWriter http.ResponseWriter, request *http.Request, prompt, cwd, override string, launch bool, rawArgs string) {
	if override != "" {
		isOverrideRefused := len(override) > promptOverrideMaxBytes || !utf8.ValidString(override) || strings.ContainsRune(override, 0)
		if isOverrideRefused {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_memory.promptTropLongOu"))
			return
		}
		prompt = override
	}
	if !launch {
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_memory.promptCopie"), "prompt": prompt})
		return
	}
	args, err := claudeArgs(rawArgs)
	if err != nil {
		writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	launchContext, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	output, err := matin(launchContext, append(args, "--prompt", prompt, cwd)...)
	if err != nil {
		message := strings.TrimSpace(output)
		if message == "" {
			message = err.Error()
		}
		writeErr(responseWriter, http.StatusInternalServerError, "internal", message)
		return
	}
	writeJSON(responseWriter, 200, map[string]any{"message": output, "prompt": prompt})
}

// canFsRead : sans garde (mode ouvert ou jeton) tout est permis ; sinon le droit fs_read du home courant est requis.
func canFsRead(request *http.Request) bool { return callerHas(request, RightFsRead) }
