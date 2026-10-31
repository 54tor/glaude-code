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

func routesSessions(mux *http.ServeMux) {
	routesTerm(mux)
	home, _ := os.UserHomeDir()
	mux.HandleFunc("GET /v1/state", func(w http.ResponseWriter, r *http.Request) {
		lastAPI.Store(time.Now().UnixNano())
		live := []string{}
		for id := range liveSessions() {
			live = append(live, id)
		}
		background := []string{}
		for id := range backgroundSessions() {
			background = append(background, id)
		}
		groupsMu.Lock()
		st := loadStore()
		groupsMu.Unlock()
		writeJSON(w, 200, map[string]any{"home": home, "root": root, "groups": withEmptyDirs(sessions(), st.Dirs), "dirs": st.Dirs, "live": live, "background": background, "waiting": waiting(), "rc": rcSessions(), "asking": askingSessions(), "vgroups": st.Groups, "favs": st.Favs, "projects": st.Projects, "providers": providerInfos(), "lang": normalizeLang(userLanguageRaw())})
	})
	// Dossiers enregistrés à la main (sans session) : ajout (children : ses sous-dossiers) ou retrait.
	saveDirs := func(w http.ResponseWriter, op, dir string, add []string) {
		groupsMu.Lock()
		st := loadStore()
		next := []string{}
		drop := map[string]bool{dir: op != "addChildren"}
		for _, d := range add {
			drop[d] = true
		}
		for _, d := range st.Dirs {
			if !drop[d] {
				next = append(next, d)
			}
		}
		next = append(next, add...)
		st.Dirs = next
		err := saveStore(st)
		groupsMu.Unlock()
		journal("dirs %s %s : %v", op, dir, errOr(err))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_sessions.enregistre"), "n": len(add)})
	}
	absDir := func(w http.ResponseWriter, d string) (string, bool) {
		if !filepath.IsAbs(d) || strings.ContainsRune(d, 0) {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return "", false
		}
		return filepath.Clean(d), true
	}
	mux.HandleFunc("POST /v1/directories", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Dir      string
			Children bool // ajoute les sous-dossiers directs plutôt que le dossier
		}
		if !decodeBody(w, r, &req, 1<<12) {
			return
		}
		dir, ok := absDir(w, req.Dir)
		if !ok {
			return
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			writeErr(w, http.StatusNotFound, "not_found", L("srv.routes_sessions.dossierIntrouvable", "A1", dir))
			return
		}
		if req.Children {
			saveDirs(w, "addChildren", dir, childDirs(dir))
			return
		}
		saveDirs(w, "add", dir, []string{dir})
	})
	mux.HandleFunc("DELETE /v1/directories", func(w http.ResponseWriter, r *http.Request) {
		if dir, ok := absDir(w, r.URL.Query().Get("dir")); ok {
			saveDirs(w, "remove", dir, []string{})
		}
	})
	// Date de modification la plus récente d'un transcript : l'interface relit la liste quand elle change (un prompt tapé dans un
	// terminal fait remonter la session sans attendre le rafraîchissement périodique). ponytail: un stat par transcript à chaque appel,
	// à remplacer par une surveillance de fichiers si les transcripts se comptent en dizaines de milliers.
	mux.HandleFunc("GET /v1/state/stamp", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"stamp": newestTranscript()})
	})
	// État en direct d'une session (sessionlive.go) et informations du compte (account.go).
	mux.HandleFunc("GET /v1/sessions/{id}/live", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		st := sessionLive(r.URL.Query().Get("dir"), id)
		st.Pending = visibleQueue(r, st.Pending)
		writeJSON(w, 200, st)
	})
	mux.HandleFunc("GET /v1/sessions/{id}/tasks/{task}/output", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		p, ok := transcript(r.URL.Query().Get("dir"), id)
		if !ok {
			writeErr(w, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
			return
		}
		out, ok := bgOutput(p, r.PathValue("task"))
		if !ok {
			writeErr(w, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
			return
		}
		writeJSON(w, 200, map[string]any{"text": out})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/workflows", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		p, ok := transcript(r.URL.Query().Get("dir"), id)
		if !ok {
			writeErr(w, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
			return
		}
		writeJSON(w, 200, map[string]any{"workflows": sessionWorkflows(p)})
	})
	// Relevé trop vieux : on le rafraîchit en arrière-plan (la page relit toutes les minutes). Le forcer lance claude :
	// c'est un POST, que CrossOriginProtection garde (un GET serait déclenchable par une image sur un site tiers).
	account := func(w http.ResponseWriter, force bool) {
		a := accountInfo()
		a.Refreshing = refreshUsage(a.LimitsAt, force)
		writeJSON(w, 200, a)
	}
	mux.HandleFunc("GET /v1/account", func(w http.ResponseWriter, r *http.Request) { account(w, false) })
	mux.HandleFunc("POST /v1/account/refreshes", func(w http.ResponseWriter, r *http.Request) { account(w, true) })
	// Envoi d'un message à une session ouverte dans tmux (sendkeys.go) : lesquelles le permettent, puis l'envoi.
	mux.HandleFunc("GET /v1/sessions/send-info", func(w http.ResponseWriter, r *http.Request) {
		ids, err := sendable()
		why := ""
		if err != nil {
			why = err.Error()
		}
		keys := []string{}
		for id := range ids {
			keys = append(keys, id)
		}
		writeJSON(w, 200, map[string]any{"ids": keys, "why": why})
	})
	// État d'une session ouverte : attend-elle votre message (idle) ou travaille-t-elle ?
	mux.HandleFunc("GET /v1/sessions/{id}/state", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		idle := false
		for _, x := range waiting() {
			idle = idle || x == id
		}
		writeJSON(w, 200, map[string]any{"idle": idle})
	})
	mux.HandleFunc("DELETE /v1/sessions/{id}/process", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		err := closeSession(id)
		journal("session-close %s : %v", id, errOr(err))
		if err != nil {
			writeErr(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": L("srv.routes_sessions.sessionFermee")})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/screen", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		text, err := sessionScreen(id)
		if err != nil {
			writeErr(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"text": text})
	})
	mux.HandleFunc("POST /v1/sessions/{id}/keystrokes", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		var req struct{ Key string }
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		err := sendKey(id, req.Key)
		journal("session-key %s %s : %v", id, req.Key, errOr(err))
		if err != nil {
			writeErr(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"message": L("srv.routes_sessions.toucheEnvoyee")})
	})
	mux.HandleFunc("POST /v1/sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		var req struct{ Text string }
		if !decodeBody(w, r, &req, sendMax+1<<10) {
			return
		}
		err := sendToSession(id, req.Text)
		journal("session-send %s (%d caractères) : %v", id, len(req.Text), errOr(err)) // jamais le texte
		if err != nil {
			writeErr(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		markSent(id)
		writeJSON(w, 201, map[string]any{"message": L("srv.routes_sessions.messageEnvoyeALa")})
	})
	// Explorateur de dossiers pour « Réassigner » / « Déplacer » : sous-dossiers d'un chemin.
	// Un chemin disparu remonte au plus proche parent qui existe.
	mux.HandleFunc("GET /v1/directories", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		if p == "" {
			p = home
		}
		sep := string(filepath.Separator)
		if p == "::drives" && runtime.GOOS == "windows" { // liste des lecteurs
			drives := []string{}
			for c := 'A'; c <= 'Z'; c++ {
				if _, err := os.Stat(string(c) + `:\`); err == nil {
					drives = append(drives, string(c)+`:\`)
				}
			}
			writeJSON(w, 200, map[string]any{"path": "", "parent": "", "dirs": drives, "sep": sep})
			return
		}
		if !filepath.IsAbs(p) {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.cheminAbsoluRequis"))
			return
		}
		p = filepath.Clean(p)
		for {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				break
			}
			up := filepath.Dir(p)
			if up == p {
				break
			}
			p = up
		}
		ents, err := readDirTrusted(p)
		if err != nil {
			writeErr(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		dirs := []string{}
		for _, e := range ents {
			isDir := e.IsDir()
			if !isDir && e.Type()&os.ModeSymlink != 0 { // lien : on ne suit que ceux qui mènent à un dossier
				fi, err := os.Stat(filepath.Join(p, e.Name()))
				isDir = err == nil && fi.IsDir()
			}
			if isDir {
				dirs = append(dirs, e.Name())
			}
		}
		sort.Strings(dirs)
		parent := filepath.Dir(p)
		if parent == p { // racine : sous Windows on remonte à la liste des lecteurs
			parent = ""
			if runtime.GOOS == "windows" {
				parent = "::drives"
			}
		}
		writeJSON(w, 200, map[string]any{"path": p, "parent": parent, "dirs": dirs, "sep": sep})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/conversation", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		q := r.URL.Query()
		p, ok := transcript(q.Get("dir"), id)
		if !ok {
			notFound(w)
			return
		}
		if sub := q.Get("sub"); sub != "" { // transcript d'un sous-agent de cette session
			if !subRe.MatchString(sub) {
				notFound(w)
				return
			}
			p = filepath.Join(strings.TrimSuffix(p, ".jsonl"), "subagents", "agent-"+sub+".jsonl")
			if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
				notFound(w)
				return
			}
		}
		// Par la fin : « before » est le décalage rendu par la page suivante. Sans limit (export), des pages de 1000 messages
		// aux textes entiers : un transcript de plusieurs centaines de Mo ne se charge plus d'un bloc.
		lim, _ := strconv.Atoi(q.Get("limit"))
		whole := lim <= 0
		if whole {
			lim = 1000
		}
		before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
		msgs, start, more, err := convPageMode(p, q.Get("tools") == "1", q.Get("anchors") == "1", before, min(lim, 5000))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if !whole {
			capMsgs(msgs)
		}
		writeJSON(w, 200, map[string]any{"msgs": msgs, "start": start, "more": more})
	})
	// Outils d'une ancre dépliée : lignes du transcript dans [from, until).
	mux.HandleFunc("GET /v1/sessions/{id}/conversation/tools", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		q := r.URL.Query()
		p, ok := transcript(q.Get("dir"), id)
		from, e1 := strconv.ParseInt(q.Get("from"), 10, 64)
		until, e2 := strconv.ParseInt(q.Get("until"), 10, 64)
		if !ok || e1 != nil || e2 != nil || from < 0 || until < 0 {
			notFound(w)
			return
		}
		msgs, err := convTools(p, from, until)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"msgs": msgs})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/conversation/text", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		q := r.URL.Query()
		p, ok := transcript(q.Get("dir"), id)
		o, e1 := strconv.ParseInt(q.Get("o"), 10, 64)
		n, e2 := strconv.Atoi(q.Get("n"))
		from, e3 := strconv.Atoi(q.Get("from"))
		if !ok || e1 != nil || e2 != nil || e3 != nil {
			notFound(w)
			return
		}
		t, next, err := convText(p, o, n, q.Get("tools") == "1", from)
		if err != nil {
			notFound(w)
			return
		}
		writeJSON(w, 200, map[string]any{"t": t, "cut": next})
	})
	mux.HandleFunc("GET /v1/info", func(w http.ResponseWriter, r *http.Request) {
		p, ok := transcript(r.URL.Query().Get("dir"), r.URL.Query().Get("id"))
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, 200, detail(p))
	})
	mux.HandleFunc("DELETE /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		items, ok := itemsQuery(w, r)
		if !ok {
			return
		}
		items = withChildren(items)
		if r.URL.Query().Get("close") == "1" { // suppression voulue d'une session ouverte : on la ferme d'abord
			closeAndWait(items)
		}
		// Pas de contexte lié à la requête : un navigateur qui se déconnecte ne doit pas
		// interrompre une suppression en cours (sessions à moitié purgées).
		n, spared, notes, err := func() (int, int, []string, error) {
			sessionsMu.Lock()
			defer sessionsMu.Unlock()
			return purgeSessions(items)
		}()
		journal("rm %d session(s) : %d supprimée(s), %d épargnée(s) %v", len(items), n, spared, ids(items))
		msg := rmSummary(n, spared)
		if len(notes) > 0 {
			msg += "\n" + strings.Join(notes, "\n")
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", msg+"\n"+err.Error())
			return
		}
		if n == 0 && spared > 0 {
			writeErr(w, http.StatusConflict, "conflict", msg)
			return
		}
		writeJSON(w, 200, map[string]any{"deleted": n, "spared": spared, "message": msg})
	})
	mux.HandleFunc("PATCH /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		items, to, ok := readItems(w, r)
		if !ok {
			return
		}
		if !filepath.IsAbs(to) {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.cheminAbsoluRequis"))
			return
		}
		n, err := func() (int, error) {
			sessionsMu.Lock()
			defer sessionsMu.Unlock()
			return reassign(items, to)
		}()
		journal("reassign %v vers %s : %v", ids(items), to, errOr(err))
		if err != nil {
			writeErr(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"n": n, "message": L("srv.routes_sessions.sessionSReassigneeS", "A1", n, "A2", to)})
	})
	// Recherche dans le texte des conversations (au moins 2 caractères).
	mux.HandleFunc("GET /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if n := utf8.RuneCountInString(strings.TrimSpace(q)); n < 2 || n > 200 {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		writeJSON(w, 200, map[string]any{"hits": searchAll(q)})
	})
	mux.HandleFunc("PATCH /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		var req struct {
			Title string
			Auto  bool
			Keep  bool // avec Auto : ne touche pas à un titre déjà posé
		}
		if !decodeBody(w, r, &req, 1<<16) {
			return
		}
		p, ok := transcript(r.URL.Query().Get("dir"), id)
		title := strings.TrimSpace(spaceRe.ReplaceAllString(req.Title, " "))
		if !ok || (title == "" && !req.Auto) || utf8.RuneCountInString(title) > 200 {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.titreOuSessionInvalide"))
			return
		}
		// Une session ouverte réécrit son propre titre : le nôtre serait écrasé.
		live, lerr := liveCheck()
		if lerr != nil {
			writeErr(w, http.StatusServiceUnavailable, "unavailable", lerr.Error())
			return
		}
		if live[id] && req.Auto && req.Keep {
			deferTitle(id, p)
			writeJSON(w, 200, map[string]any{"title": "", "message": ""})
			return
		}
		if live[id] {
			writeErr(w, http.StatusConflict, "conflict", L("srv.routes_sessions.sessionOuverteRenommeLa"))
			return
		}
		if req.Auto && req.Keep && hasTitle(p) {
			writeJSON(w, 200, map[string]any{"title": "", "message": ""})
			return
		}
		if req.Auto {
			var terr error
			if title, terr = autoTitle(p); terr != nil {
				writeErr(w, http.StatusBadGateway, "upstream_failed", L("srv.routes_sessions.titreAutomatiqueImpossible", "A1", terr.Error()))
				return
			}
		}
		err := setTitle(p, id, title)
		journal("rename %s « %s » : %v", id, title, errOr(err))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"title": title, "message": L("srv.routes_sessions.renommee", "A1", title)})
	})
	mux.HandleFunc("DELETE /v1/sessions/{id}/conversation", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		after, perr := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		p, ok := transcript(r.URL.Query().Get("dir"), id)
		if perr != nil || !ok {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		sessionsMu.Lock()
		defer sessionsMu.Unlock()
		// Un processus qui écrit ce transcript le rallongerait depuis sa propre mémoire de la conversation.
		live, err := liveCheck()
		if err == nil && live[id] {
			err = terr("srv.rewind.sessionOuverte")
		}
		chatMu.Lock()
		if err == nil && chatRun[id] != nil {
			err = terr("srv.chat.claudeRepondDejaDans")
		}
		var n int
		if err == nil {
			n, err = rewind(p, after)
		}
		chatMu.Unlock()
		journal("conv-rewind %s @%d : %d message(s), %v", id, after, n, errOr(err))
		if err != nil {
			writeErr(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"n": n, "message": L("srv.rewind.fait", "A1", n)})
	})
	// Snippets = commandes Claude Code (commands.go) : personnelles (~/.claude/commands, ou GLD_WEB_SNIPPETS) et de projet.
	mux.HandleFunc("PUT /v1/vgroups", func(w http.ResponseWriter, r *http.Request) {
		var st Store
		if !decodeBody(w, r, &st, 1<<20) {
			return
		}
		if st.Groups == nil || st.Favs == nil {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		groupsMu.Lock()
		if st.Projects == nil || st.Dirs == nil { // un client qui ne connaît pas un champ ne l'efface pas
			old := loadStore()
			if st.Dirs == nil {
				st.Dirs = old.Dirs
			}
			if st.Projects == nil {
				st.Projects = old.Projects
			}
		}
		err := saveStore(st)
		groupsMu.Unlock()
		journal("vgroups %d groupe(s), %d favori(s) : %v", len(st.Groups), len(st.Favs), errOr(err))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// Rouvre dans un onglet de terminal via gld-matin : une session (id) ou les récentes du projet (dir seul).
	// Le cwd vient du transcript côté serveur, jamais du navigateur.
	openIn := func(w http.ResponseWriter, r *http.Request, id, dir string) {
		var req struct{ Args, Prompt string }
		if !decodeBody(w, r, &req, 1<<16) {
			return
		}
		var p string
		args, err := claudeArgs(req.Args)
		if err == nil {
			err = checkResumePrompt(id, req.Prompt)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if id != "" {
			var ok bool
			if p, ok = transcript(dir, id); !ok {
				notFound(w)
				return
			}
			args = append(args, "--id", id)
			if req.Prompt != "" { // reprise avec un message : transmis tel quel, gld-matin l'échappe pour le shell
				args = append(args, "--prompt", req.Prompt)
			}
		} else if strings.ContainsAny(dir, "/\\") || dir == "" || dir == ".." {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.routes_sessions.dossierInvalide"))
			return
		} else if fs, _ := filepath.Glob(filepath.Join(root, dir, "*.jsonl")); len(fs) > 0 {
			p = fs[0]
		}
		st, err := os.Stat(p)
		if err != nil {
			writeErr(w, http.StatusNotFound, "not_found", L("srv.routes_sessions.aucuneSession"))
			return
		}
		cwd := info(p, st).Cwd
		if fi, err := os.Stat(cwd); cwd == "" || err != nil || !fi.IsDir() {
			writeErr(w, http.StatusNotFound, "not_found", L("srv.routes_sessions.dossierDuProjetIntrouvable", "A1", cwd))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		out, err := matin(ctx, append(args, cwd)...)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "launch_failed", cmp.Or(out, err.Error()))
			return
		}
		writeJSON(w, 200, map[string]any{"message": out})
	}
	mux.HandleFunc("PUT /v1/sessions/{id}/process", func(w http.ResponseWriter, r *http.Request) {
		if id, ok := pathID(w, r, "id"); ok {
			openIn(w, r, id, r.URL.Query().Get("dir"))
		}
	})
	mux.HandleFunc("PUT /v1/projects/{dir}/process", func(w http.ResponseWriter, r *http.Request) {
		openIn(w, r, "", r.PathValue("dir"))
	})

	// Anti DNS-rebinding : seul le Host local est servi. Anti CSRF : http.CrossOriginProtection
	// (bibliothèque standard) refuse toute requête modifiante venue d'une autre origine.
}

// rmSummary : bilan d'une suppression, lisible tel quel dans une notification.
func rmSummary(n, spared int) string {
	s := L("srv.rm.none")
	if n > 0 {
		s = L("srv.rm.deleted", "Count", n)
	}
	if spared > 0 {
		s += L("srv.rm.spared", "Count", spared)
	}
	return s
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
	for _, p := range files {
		if st, err := os.Stat(p); err == nil && st.ModTime().UnixNano() > newest {
			newest = st.ModTime().UnixNano()
		}
	}
	stampAt, stampRoot, stampVal = time.Now(), root, newest
	return newest
}

// Sous-dossiers directs de dir, triés, sans les dossiers cachés ni les liens symboliques.
func childDirs(dir string) []string {
	es, _ := os.ReadDir(dir)
	out := []string{}
	for _, e := range es {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}
