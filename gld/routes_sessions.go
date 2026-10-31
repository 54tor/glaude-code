package main

import (
	"cmp"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// routesSessions : liste, lecture, suppression, réassignation, recherche et réouverture des sessions.
var subRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

const (
	exportPageMessages    = 1000
	conversationPageMax   = 5000
	searchQueryMinLength  = 2
	searchQueryMaxLength  = 200
	sessionTitleMaxLength = 200
)

func routesSessions(mux *http.ServeMux) {
	routesTerm(mux)
	home, _ := os.UserHomeDir()
	mux.HandleFunc("GET /v1/state", func(responseWriter http.ResponseWriter, request *http.Request) {
		lastAPI.Store(time.Now().UnixNano())
		liveIDs := []string{}
		for sessionID := range liveSessions() {
			liveIDs = append(liveIDs, sessionID)
		}
		background := []string{}
		for sessionID := range backgroundSessions() {
			background = append(background, sessionID)
		}
		groupsMu.Lock()
		store := loadStore()
		groupsMu.Unlock()
		writeJSON(responseWriter, 200, map[string]any{
			"home":       home,
			"root":       root,
			"groups":     withEmptyDirs(sessions(), store.Dirs),
			"dirs":       store.Dirs,
			"live":       liveIDs,
			"background": background,
			"waiting":    waiting(),
			"rc":         rcSessions(),
			"asking":     askingSessions(),
			"vgroups":    store.Groups,
			"favs":       store.Favs,
			"projects":   store.Projects,
			"providers":  providerInfos(),
			"lang":       normalizeLang(userLanguageRaw()),
		})
	})
	// Dossiers enregistrés à la main (sans session) : ajout (children : ses sous-dossiers) ou retrait.
	saveDirs := func(responseWriter http.ResponseWriter, operation, dir string, add []string) {
		groupsMu.Lock()
		store := loadStore()
		keptDirs := []string{}
		isDropped := map[string]bool{dir: operation != "addChildren"}
		for _, dirPath := range add {
			isDropped[dirPath] = true
		}
		for _, dirPath := range store.Dirs {
			if !isDropped[dirPath] {
				keptDirs = append(keptDirs, dirPath)
			}
		}
		keptDirs = append(keptDirs, add...)
		store.Dirs = keptDirs
		err := saveStore(store)
		groupsMu.Unlock()
		journal("dirs %s %s : %v", operation, dir, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_sessions.enregistre"), "n": len(add)})
	}
	requireAbsoluteDir := func(responseWriter http.ResponseWriter, rawDir string) (string, bool) {
		if !filepath.IsAbs(rawDir) || strings.ContainsRune(rawDir, 0) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return "", false
		}
		return filepath.Clean(rawDir), true
	}
	mux.HandleFunc("POST /v1/directories", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Dir      string
			Children bool // ajoute les sous-dossiers directs plutôt que le dossier
		}
		if !decodeBody(responseWriter, request, &body, 1<<12) {
			return
		}
		dir, isValidDir := requireAbsoluteDir(responseWriter, body.Dir)
		if !isValidDir {
			return
		}
		if dirInfo, err := os.Stat(dir); err != nil || !dirInfo.IsDir() {
			writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.routes_sessions.dossierIntrouvable", "A1", dir))
			return
		}
		if body.Children {
			saveDirs(responseWriter, "addChildren", dir, childDirs(dir))
			return
		}
		saveDirs(responseWriter, "add", dir, []string{dir})
	})
	mux.HandleFunc("DELETE /v1/directories", func(responseWriter http.ResponseWriter, request *http.Request) {
		if dir, isValidDir := requireAbsoluteDir(responseWriter, request.URL.Query().Get("dir")); isValidDir {
			saveDirs(responseWriter, "remove", dir, []string{})
		}
	})
	// Date de modification la plus récente d'un transcript : l'interface relit la liste quand elle change (un prompt tapé dans un
	// terminal fait remonter la session sans attendre le rafraîchissement périodique). ponytail: un stat par transcript à chaque appel,
	// à remplacer par une surveillance de fichiers si les transcripts se comptent en dizaines de milliers.
	mux.HandleFunc("GET /v1/state/stamp", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, 200, map[string]any{"stamp": newestTranscript()})
	})
	// État en direct d'une session (sessionlive.go) et informations du compte (account.go).
	mux.HandleFunc("GET /v1/sessions/{id}/live", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		liveState := sessionLive(request.URL.Query().Get("dir"), sessionID)
		liveState.Pending = visibleQueue(request, liveState.Pending)
		writeJSON(responseWriter, 200, liveState)
	})
	mux.HandleFunc("GET /v1/sessions/{id}/tasks/{task}/output", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		transcriptPath, found := transcript(request.URL.Query().Get("dir"), sessionID)
		if !found {
			writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
			return
		}
		output, found := bgOutput(transcriptPath, request.PathValue("task"))
		if !found {
			writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"text": output})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/workflows", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		transcriptPath, found := transcript(request.URL.Query().Get("dir"), sessionID)
		if !found {
			writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"workflows": sessionWorkflows(transcriptPath)})
	})
	// Relevé trop vieux : on le rafraîchit en arrière-plan (la page relit toutes les minutes). Le forcer lance claude :
	// c'est un POST, que CrossOriginProtection garde (un GET serait déclenchable par une image sur un site tiers).
	account := func(responseWriter http.ResponseWriter, force bool) {
		accountState := accountInfo()
		accountState.Refreshing = refreshUsage(accountState.LimitsAt, force)
		writeJSON(responseWriter, 200, accountState)
	}
	mux.HandleFunc("GET /v1/account", func(responseWriter http.ResponseWriter, request *http.Request) { account(responseWriter, false) })
	mux.HandleFunc("POST /v1/account/refreshes", func(responseWriter http.ResponseWriter, request *http.Request) { account(responseWriter, true) })
	// Envoi d'un message à une session ouverte dans tmux (sendkeys.go) : lesquelles le permettent, puis l'envoi.
	mux.HandleFunc("GET /v1/sessions/send-info", func(responseWriter http.ResponseWriter, request *http.Request) {
		sendableIDs, err := sendable()
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		idList := []string{}
		for sessionID := range sendableIDs {
			idList = append(idList, sessionID)
		}
		writeJSON(responseWriter, 200, map[string]any{"ids": idList, "why": reason})
	})
	// État d'une session ouverte : attend-elle votre message (idle) ou travaille-t-elle ?
	mux.HandleFunc("GET /v1/sessions/{id}/state", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		idle := false
		for _, waitingID := range waiting() {
			idle = idle || waitingID == sessionID
		}
		writeJSON(responseWriter, 200, map[string]any{"idle": idle})
	})
	mux.HandleFunc("DELETE /v1/sessions/{id}/process", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		err := closeSession(sessionID)
		journal("session-close %s : %v", sessionID, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": L("srv.routes_sessions.sessionFermee")})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/screen", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		text, err := sessionScreen(sessionID)
		if err != nil {
			writeErr(responseWriter, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("POST /v1/sessions/{id}/keystrokes", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		var body struct{ Key string }
		if !decodeBody(responseWriter, request, &body, 1<<10) {
			return
		}
		err := sendKey(sessionID, body.Key)
		journal("session-key %s %s : %v", sessionID, body.Key, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(responseWriter, 201, map[string]any{"message": L("srv.routes_sessions.toucheEnvoyee")})
	})
	mux.HandleFunc("POST /v1/sessions/{id}/messages", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		var body struct{ Text string }
		if !decodeBody(responseWriter, request, &body, sendMax+1<<10) {
			return
		}
		err := sendToSession(sessionID, body.Text)
		journal("session-send %s (%d caractères) : %v", sessionID, len(body.Text), errOr(err)) // jamais le texte
		if err != nil {
			writeErr(responseWriter, http.StatusConflict, "conflict", err.Error())
			return
		}
		markSent(sessionID)
		writeJSON(responseWriter, 201, map[string]any{"message": L("srv.routes_sessions.messageEnvoyeALa")})
	})
	// Explorateur de dossiers pour « Réassigner » / « Déplacer » : sous-dossiers d'un chemin.
	// Un chemin disparu remonte au plus proche parent qui existe.
	mux.HandleFunc("GET /v1/directories", func(responseWriter http.ResponseWriter, request *http.Request) {
		browsedPath := request.URL.Query().Get("path")
		if browsedPath == "" {
			browsedPath = home
		}
		separator := string(filepath.Separator)
		if browsedPath == "::drives" && runtime.GOOS == "windows" { // liste des lecteurs
			drives := []string{}
			for driveLetter := 'A'; driveLetter <= 'Z'; driveLetter++ {
				if _, driveStatErr := os.Stat(string(driveLetter) + `:\`); driveStatErr == nil {
					drives = append(drives, string(driveLetter)+`:\`)
				}
			}
			writeJSON(responseWriter, 200, map[string]any{"path": "", "parent": "", "dirs": drives, "sep": separator})
			return
		}
		if !filepath.IsAbs(browsedPath) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.cheminAbsoluRequis"))
			return
		}
		browsedPath = filepath.Clean(browsedPath)
		for {
			if dirInfo, statErr := os.Stat(browsedPath); statErr == nil && dirInfo.IsDir() {
				break
			}
			parentDir := filepath.Dir(browsedPath)
			if parentDir == browsedPath {
				break
			}
			browsedPath = parentDir
		}
		entries, readErr := readDirTrusted(browsedPath)
		if readErr != nil {
			writeErr(responseWriter, http.StatusForbidden, "forbidden", readErr.Error())
			return
		}
		dirs := []string{}
		for _, entry := range entries {
			isDir := entry.IsDir()
			if !isDir && entry.Type()&os.ModeSymlink != 0 { // lien : on ne suit que ceux qui mènent à un dossier
				targetInfo, linkStatErr := os.Stat(filepath.Join(browsedPath, entry.Name()))
				isDir = linkStatErr == nil && targetInfo.IsDir()
			}
			if isDir {
				dirs = append(dirs, entry.Name())
			}
		}
		sort.Strings(dirs)
		parent := filepath.Dir(browsedPath)
		if parent == browsedPath { // racine : sous Windows on remonte à la liste des lecteurs
			parent = ""
			if runtime.GOOS == "windows" {
				parent = "::drives"
			}
		}
		writeJSON(responseWriter, 200, map[string]any{"path": browsedPath, "parent": parent, "dirs": dirs, "sep": separator})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/conversation", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		query := request.URL.Query()
		transcriptPath, found := transcript(query.Get("dir"), sessionID)
		if !found {
			notFound(responseWriter)
			return
		}
		if sub := query.Get("sub"); sub != "" { // transcript d'un sous-agent de cette session
			if !subRe.MatchString(sub) {
				notFound(responseWriter)
				return
			}
			if idRe.MatchString(sub) && isTeamTranscript(transcriptPath, sessionID, sub) { // coéquipier : une session voisine
				transcriptPath = filepath.Join(filepath.Dir(transcriptPath), sub+".jsonl")
			} else {
				transcriptPath = filepath.Join(strings.TrimSuffix(transcriptPath, ".jsonl"), "subagents", "agent-"+sub+".jsonl")
			}
			if subagentInfo, subagentStatErr := os.Stat(transcriptPath); subagentStatErr != nil || !subagentInfo.Mode().IsRegular() {
				notFound(responseWriter)
				return
			}
		}
		// Par la fin : « before » est le décalage rendu par la page suivante. Sans limit (export), des pages de 1000 messages
		// aux textes entiers : un transcript de plusieurs centaines de Mo ne se charge plus d'un bloc.
		pageLimit, _ := strconv.Atoi(query.Get("limit"))
		isWholeExport := pageLimit <= 0
		if isWholeExport {
			pageLimit = exportPageMessages
		}
		before, _ := strconv.ParseInt(query.Get("before"), 10, 64)
		messages, start, more, err := convPageMode(transcriptPath, query.Get("tools") == "1", query.Get("anchors") == "1", before, min(pageLimit, conversationPageMax))
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if !isWholeExport {
			capMsgs(messages)
		}
		writeJSON(responseWriter, 200, map[string]any{"msgs": messages, "start": start, "more": more})
	})
	// Outils d'une ancre dépliée : lignes du transcript dans [from, until).
	mux.HandleFunc("GET /v1/sessions/{id}/conversation/tools", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		query := request.URL.Query()
		transcriptPath, found := transcript(query.Get("dir"), sessionID)
		fromOffset, fromErr := strconv.ParseInt(query.Get("from"), 10, 64)
		until, untilErr := strconv.ParseInt(query.Get("until"), 10, 64)
		if !found || fromErr != nil || untilErr != nil || fromOffset < 0 || until < 0 {
			notFound(responseWriter)
			return
		}
		messages, err := convTools(transcriptPath, fromOffset, until)
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"msgs": messages})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/conversation/text", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		query := request.URL.Query()
		transcriptPath, found := transcript(query.Get("dir"), sessionID)
		offset, offsetErr := strconv.ParseInt(query.Get("o"), 10, 64)
		messageCount, countErr := strconv.Atoi(query.Get("n"))
		fromIndex, fromErr := strconv.Atoi(query.Get("from"))
		if !found || offsetErr != nil || countErr != nil || fromErr != nil {
			notFound(responseWriter)
			return
		}
		textChunk, cutOffset, err := convText(transcriptPath, offset, messageCount, query.Get("tools") == "1", fromIndex)
		if err != nil {
			notFound(responseWriter)
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"t": textChunk, "cut": cutOffset})
	})
	mux.HandleFunc("GET /v1/info", func(responseWriter http.ResponseWriter, request *http.Request) {
		transcriptPath, found := transcript(request.URL.Query().Get("dir"), request.URL.Query().Get("id"))
		if !found {
			notFound(responseWriter)
			return
		}
		writeJSON(responseWriter, 200, detail(transcriptPath))
	})
	mux.HandleFunc("DELETE /v1/sessions", func(responseWriter http.ResponseWriter, request *http.Request) {
		items, found := itemsQuery(responseWriter, request)
		if !found {
			return
		}
		items = withChildren(items)
		if request.URL.Query().Get("close") == "1" { // suppression voulue d'une session ouverte : on la ferme d'abord
			closeAndWait(items)
		}
		// Pas de contexte lié à la requête : un navigateur qui se déconnecte ne doit pas
		// interrompre une suppression en cours (sessions à moitié purgées).
		deletedCount, spared, notes, err := func() (int, int, []string, error) {
			sessionsMu.Lock()
			defer sessionsMu.Unlock()
			return purgeSessions(items)
		}()
		journal("rm %d session(s) : %d supprimée(s), %d épargnée(s) %v", len(items), deletedCount, spared, ids(items))
		summary := rmSummary(deletedCount, spared)
		if len(notes) > 0 {
			summary += "\n" + strings.Join(notes, "\n")
		}
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", summary+"\n"+err.Error())
			return
		}
		if deletedCount == 0 && spared > 0 {
			writeErr(responseWriter, http.StatusConflict, "conflict", summary)
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"deleted": deletedCount, "spared": spared, "message": summary})
	})
	mux.HandleFunc("PATCH /v1/sessions", func(responseWriter http.ResponseWriter, request *http.Request) {
		items, targetDir, found := readItems(responseWriter, request)
		if !found {
			return
		}
		if !filepath.IsAbs(targetDir) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.cheminAbsoluRequis"))
			return
		}
		movedCount, err := func() (int, error) {
			sessionsMu.Lock()
			defer sessionsMu.Unlock()
			return reassign(items, targetDir)
		}()
		journal("reassign %v vers %s : %v", ids(items), targetDir, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"n": movedCount, "message": L("srv.routes_sessions.sessionSReassigneeS", "A1", movedCount, "A2", targetDir)})
	})
	// Recherche dans le texte des conversations (au moins 2 caractères).
	mux.HandleFunc("GET /v1/sessions", func(responseWriter http.ResponseWriter, request *http.Request) {
		searchText := request.URL.Query().Get("q")
		queryLength := utf8.RuneCountInString(strings.TrimSpace(searchText))
		if queryLength < searchQueryMinLength || queryLength > searchQueryMaxLength {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"hits": searchAll(searchText)})
	})
	mux.HandleFunc("PATCH /v1/sessions/{id}", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		var body struct {
			Title string
			Auto  bool
			Keep  bool // avec Auto : ne touche pas à un titre déjà posé
		}
		if !decodeBody(responseWriter, request, &body, 1<<16) {
			return
		}
		transcriptPath, found := transcript(request.URL.Query().Get("dir"), sessionID)
		title := strings.TrimSpace(spaceRe.ReplaceAllString(body.Title, " "))
		isTitleMissing := title == "" && !body.Auto
		isTitleTooLong := utf8.RuneCountInString(title) > sessionTitleMaxLength
		if !found || isTitleMissing || isTitleTooLong {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.titreOuSessionInvalide"))
			return
		}
		// Une session ouverte réécrit son propre titre : le nôtre serait écrasé.
		liveIDs, liveErr := liveCheck()
		if liveErr != nil {
			writeErr(responseWriter, http.StatusServiceUnavailable, "unavailable", liveErr.Error())
			return
		}
		if liveIDs[sessionID] && body.Auto && body.Keep {
			deferTitle(sessionID, transcriptPath)
			writeJSON(responseWriter, 200, map[string]any{"title": "", "message": ""})
			return
		}
		if liveIDs[sessionID] {
			writeErr(responseWriter, http.StatusConflict, "conflict", L("srv.routes_sessions.sessionOuverteRenommeLa"))
			return
		}
		if body.Auto && body.Keep && hasTitle(transcriptPath) {
			writeJSON(responseWriter, 200, map[string]any{"title": "", "message": ""})
			return
		}
		if body.Auto {
			var titleErr error
			if title, titleErr = autoTitle(transcriptPath); titleErr != nil {
				writeErr(responseWriter, http.StatusBadGateway, "upstream_failed", L("srv.routes_sessions.titreAutomatiqueImpossible", "A1", titleErr.Error()))
				return
			}
		}
		err := setTitle(transcriptPath, sessionID, title)
		journal("rename %s « %s » : %v", sessionID, title, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"title": title, "message": L("srv.routes_sessions.renommee", "A1", title)})
	})
	mux.HandleFunc("DELETE /v1/sessions/{id}/conversation", func(responseWriter http.ResponseWriter, request *http.Request) {
		sessionID, found := pathID(responseWriter, request, "id")
		if !found {
			return
		}
		after, afterErr := strconv.ParseInt(request.URL.Query().Get("after"), 10, 64)
		transcriptPath, found := transcript(request.URL.Query().Get("dir"), sessionID)
		if afterErr != nil || !found {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		sessionsMu.Lock()
		defer sessionsMu.Unlock()
		// Un processus qui écrit ce transcript le rallongerait depuis sa propre mémoire de la conversation.
		liveIDs, err := liveCheck()
		if err == nil && liveIDs[sessionID] {
			err = terr("srv.rewind.sessionOuverte")
		}
		chatMu.Lock()
		if err == nil && chatRun[sessionID] != nil {
			err = terr("srv.chat.claudeRepondDejaDans")
		}
		var removedCount int
		if err == nil {
			removedCount, err = rewind(transcriptPath, after)
		}
		chatMu.Unlock()
		journal("conv-rewind %s @%d : %d message(s), %v", sessionID, after, removedCount, errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"n": removedCount, "message": L("srv.rewind.fait", "A1", removedCount)})
	})
	// Snippets = commandes Claude Code (commands.go) : personnelles (~/.claude/commands, ou GLD_WEB_SNIPPETS) et de projet.
	mux.HandleFunc("PUT /v1/vgroups", func(responseWriter http.ResponseWriter, request *http.Request) {
		var store Store
		if !decodeBody(responseWriter, request, &store, 1<<20) {
			return
		}
		if store.Groups == nil || store.Favs == nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		groupsMu.Lock()
		if store.Projects == nil || store.Dirs == nil { // un client qui ne connaît pas un champ ne l'efface pas
			storedState := loadStore()
			if store.Dirs == nil {
				store.Dirs = storedState.Dirs
			}
			if store.Projects == nil {
				store.Projects = storedState.Projects
			}
		}
		err := saveStore(store)
		groupsMu.Unlock()
		journal("vgroups %d groupe(s), %d favori(s) : %v", len(store.Groups), len(store.Favs), errOr(err))
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})
	// Rouvre dans un onglet de terminal via gld-matin : une session (id) ou les récentes du projet (dir seul).
	// Le cwd vient du transcript côté serveur, jamais du navigateur.
	openIn := func(responseWriter http.ResponseWriter, request *http.Request, sessionID, dir string) {
		var body struct{ Args, Prompt string }
		if !decodeBody(responseWriter, request, &body, 1<<16) {
			return
		}
		var transcriptPath string
		args, err := claudeArgs(body.Args)
		if err == nil {
			err = checkResumePrompt(sessionID, body.Prompt)
		}
		if err != nil {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if sessionID != "" {
			var found bool
			if transcriptPath, found = transcript(dir, sessionID); !found {
				notFound(responseWriter)
				return
			}
			args = append(args, "--id", sessionID)
			if body.Prompt != "" { // reprise avec un message : transmis tel quel, gld-matin l'échappe pour le shell
				args = append(args, "--prompt", body.Prompt)
			}
		} else if strings.ContainsAny(dir, "/\\") || dir == "" || dir == ".." {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.dossierInvalide"))
			return
		} else if matches, _ := filepath.Glob(filepath.Join(root, dir, "*.jsonl")); len(matches) > 0 {
			transcriptPath = matches[0]
		}
		transcriptInfo, err := os.Stat(transcriptPath)
		if err != nil {
			writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.routes_sessions.aucuneSession"))
			return
		}
		cwd := info(transcriptPath, transcriptInfo).Cwd
		if cwdInfo, cwdStatErr := os.Stat(cwd); cwd == "" || cwdStatErr != nil || !cwdInfo.IsDir() {
			writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.routes_sessions.dossierDuProjetIntrouvable", "A1", cwd))
			return
		}
		launchContext, cancel := context.WithTimeout(request.Context(), 60*time.Second)
		defer cancel()
		output, err := matin(launchContext, append(args, cwd)...)
		if err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "launch_failed", cmp.Or(output, err.Error()))
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": output})
	}
	mux.HandleFunc("PUT /v1/sessions/{id}/process", func(responseWriter http.ResponseWriter, request *http.Request) {
		if sessionID, found := pathID(responseWriter, request, "id"); found {
			openIn(responseWriter, request, sessionID, request.URL.Query().Get("dir"))
		}
	})
	mux.HandleFunc("PUT /v1/projects/{dir}/process", func(responseWriter http.ResponseWriter, request *http.Request) {
		openIn(responseWriter, request, "", request.PathValue("dir"))
	})

	// Anti DNS-rebinding : seul le Host local est servi. Anti CSRF : http.CrossOriginProtection
	// (bibliothèque standard) refuse toute requête modifiante venue d'une autre origine.
}

// rmSummary : bilan d'une suppression, lisible tel quel dans une notification.
func rmSummary(deletedCount, spared int) string {
	summary := L("srv.rm.none")
	if deletedCount > 0 {
		summary = L("srv.rm.deleted", "Count", deletedCount)
	}
	if spared > 0 {
		summary += L("srv.rm.spared", "Count", spared)
	}
	return summary
}

// newestTranscript : la date de modification la plus récente des transcripts, gardée stampTTL ; plusieurs onglets ouverts
// interrogent la route toutes les 3 s, chacun parcourait les 316 transcripts (11 ms mesurés) à chaque fois.
var (
	stampMu   sync.Mutex
	stampAt   time.Time
	stampRoot string
	stampVal  int64
	stampTTL  = time.Second
)

func newestTranscript() int64 {
	stampMu.Lock()
	defer stampMu.Unlock()
	if stampRoot == root && time.Since(stampAt) < stampTTL {
		return stampVal
	}
	var newest int64
	files, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	for _, transcriptPath := range files {
		if fileInfo, err := os.Stat(transcriptPath); err == nil && fileInfo.ModTime().UnixNano() > newest {
			newest = fileInfo.ModTime().UnixNano()
		}
	}
	stampAt, stampRoot, stampVal = time.Now(), root, newest
	return newest
}

// Sous-dossiers directs de dir, triés, sans les dossiers cachés ni les liens symboliques.
func childDirs(dir string) []string {
	entries, _ := os.ReadDir(dir)
	dirs := []string{}
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			dirs = append(dirs, filepath.Join(dir, entry.Name()))
		}
	}
	return dirs
}
