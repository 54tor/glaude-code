package main

import (
	"context"
	"encoding/json"
	"io"
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
	mux.HandleFunc("GET /api", func(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, 200, map[string]any{"home": home, "root": root, "groups": withEmptyDirs(sessions(), st.Dirs), "dirs": st.Dirs, "live": live, "background": background, "waiting": waiting(), "asking": askingSessions(), "vgroups": st.Groups, "favs": st.Favs, "tags": st.Tags, "projects": st.Projects, "providers": providerInfos(), "lang": normalizeLang(userLanguageRaw())})
	})
	// Dossiers enregistrés à la main (sans session) : ajout ou retrait.
	mux.HandleFunc("POST /dirs", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Op, Dir string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil || (req.Op != "add" && req.Op != "remove") || !filepath.IsAbs(req.Dir) || strings.ContainsRune(req.Dir, 0) {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		dir := filepath.Clean(req.Dir)
		if fi, err := os.Stat(dir); req.Op == "add" && (err != nil || !fi.IsDir()) {
			writeJSON(w, 200, map[string]any{"ok": false, "out": L("srv.routes_sessions.dossierIntrouvable", "A1", dir)})
			return
		}
		groupsMu.Lock()
		st := loadStore()
		next := []string{}
		for _, d := range st.Dirs {
			if d != dir {
				next = append(next, d)
			}
		}
		if req.Op == "add" {
			next = append(next, dir)
		}
		st.Dirs = next
		err := saveStore(st)
		groupsMu.Unlock()
		journal("dirs %s %s : %v", req.Op, dir, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_sessions.enregistre")})
	})
	// Date de modification la plus récente d'un transcript : l'interface relit la liste quand elle change (un prompt tapé dans un
	// terminal fait remonter la session sans attendre le rafraîchissement périodique). ponytail: un stat par transcript à chaque appel,
	// à remplacer par une surveillance de fichiers si les transcripts se comptent en dizaines de milliers.
	mux.HandleFunc("GET /sessions-stamp", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"stamp": newestTranscript()})
	})
	// Envoi d'un message à une session ouverte dans tmux (sendkeys.go) : lesquelles le permettent, puis l'envoi.
	// État en direct d'une session (sessionlive.go) et informations du compte (account.go).
	mux.HandleFunc("GET /session-live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, sessionLive(r.URL.Query().Get("dir"), r.URL.Query().Get("id")))
	})
	// Relevé trop vieux : on le rafraîchit en arrière-plan (la page relit toutes les minutes). Le forcer lance claude :
	// c'est un POST, que CrossOriginProtection garde (un GET serait déclenchable par une image sur un site tiers).
	account := func(w http.ResponseWriter, force bool) {
		a := accountInfo()
		a.Refreshing = refreshUsage(a.LimitsAt, force)
		writeJSON(w, 200, a)
	}
	mux.HandleFunc("GET /account", func(w http.ResponseWriter, r *http.Request) { account(w, false) })
	mux.HandleFunc("POST /account-refresh", func(w http.ResponseWriter, r *http.Request) { account(w, true) })
	mux.HandleFunc("GET /session-send-info", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("GET /session-state", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		idle := false
		for _, x := range waiting() {
			idle = idle || x == id
		}
		writeJSON(w, 200, map[string]any{"idle": idle})
	})
	mux.HandleFunc("POST /session-close", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		err := closeSession(req.ID)
		journal("session-close %s : %v", req.ID, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_sessions.sessionFermee")})
	})
	mux.HandleFunc("GET /session-screen", func(w http.ResponseWriter, r *http.Request) {
		text, err := sessionScreen(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "text": text})
	})
	mux.HandleFunc("POST /session-key", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID, Key string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		err := sendKey(req.ID, req.Key)
		journal("session-key %s %s : %v", req.ID, req.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_sessions.toucheEnvoyee")})
	})
	mux.HandleFunc("POST /session-send", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID, Text string }
		if json.NewDecoder(io.LimitReader(r.Body, sendMax+1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		err := sendToSession(req.ID, req.Text)
		journal("session-send %s (%d caractères) : %v", req.ID, len(req.Text), errOr(err)) // jamais le texte
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_sessions.messageEnvoyeALa")})
	})
	// Explorateur de dossiers pour « Réassigner » / « Déplacer » : sous-dossiers d'un chemin.
	// Un chemin disparu remonte au plus proche parent qui existe.
	mux.HandleFunc("GET /ls", func(w http.ResponseWriter, r *http.Request) {
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
			writeJSON(w, 400, map[string]any{"error": L("srv.routes_sessions.cheminAbsoluRequis")})
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
		ents, err := os.ReadDir(p)
		if err != nil {
			writeJSON(w, 403, map[string]any{"error": err.Error()})
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
	mux.HandleFunc("GET /conv", func(w http.ResponseWriter, r *http.Request) {
		p, ok := transcript(r.URL.Query().Get("dir"), r.URL.Query().Get("id"))
		if !ok {
			writeJSON(w, 404, map[string]any{})
			return
		}
		q := r.URL.Query()
		if sub := q.Get("sub"); sub != "" { // transcript d'un sous-agent de cette session
			if !subRe.MatchString(sub) {
				writeJSON(w, 404, map[string]any{})
				return
			}
			p = filepath.Join(strings.TrimSuffix(p, ".jsonl"), "subagents", "agent-"+sub+".jsonl")
			if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
				writeJSON(w, 404, map[string]any{})
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
		msgs, start, more, err := convPage(p, q.Get("tools") == "1", before, min(lim, 5000))
		if err != nil {
			writeJSON(w, 500, map[string]any{})
			return
		}
		if !whole {
			capMsgs(msgs)
		}
		writeJSON(w, 200, map[string]any{"msgs": msgs, "start": start, "more": more})
	})
	mux.HandleFunc("GET /conv-text", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		p, ok := transcript(q.Get("dir"), q.Get("id"))
		o, e1 := strconv.ParseInt(q.Get("o"), 10, 64)
		n, e2 := strconv.Atoi(q.Get("n"))
		from, e3 := strconv.Atoi(q.Get("from"))
		if !ok || e1 != nil || e2 != nil || e3 != nil {
			writeJSON(w, 404, map[string]any{})
			return
		}
		t, next, err := convText(p, o, n, q.Get("tools") == "1", from)
		if err != nil {
			writeJSON(w, 404, map[string]any{})
			return
		}
		writeJSON(w, 200, map[string]any{"t": t, "cut": next})
	})
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		p, ok := transcript(r.URL.Query().Get("dir"), r.URL.Query().Get("id"))
		if !ok {
			writeJSON(w, 404, map[string]any{})
			return
		}
		writeJSON(w, 200, detail(p))
	})
	mux.HandleFunc("POST /rm", func(w http.ResponseWriter, r *http.Request) {
		items, _, ok := readItems(w, r)
		if !ok {
			return
		}
		if r.URL.Query().Get("close") == "1" { // suppression voulue d'une session ouverte : on la ferme d'abord
			closeAndWait(items)
		}
		// Pas de contexte lié à la requête : un navigateur qui se déconnecte ne doit pas
		// interrompre une suppression en cours (sessions à moitié purgées).
		sessionsMu.Lock()
		n, spared, notes, err := purgeSessions(items)
		sessionsMu.Unlock()
		journal("rm %d session(s) : %d supprimée(s), %d épargnée(s) %v", len(items), n, spared, ids(items))
		msg := rmSummary(n, spared)
		if len(notes) > 0 {
			msg += "\n" + strings.Join(notes, "\n")
		}
		if err != nil {
			msg += "\n" + err.Error()
		}
		writeJSON(w, 200, map[string]any{"ok": err == nil && (n > 0 || spared == 0), "out": msg})
	})
	mux.HandleFunc("POST /reassign", func(w http.ResponseWriter, r *http.Request) {
		items, to, ok := readItems(w, r)
		if !ok {
			return
		}
		sessionsMu.Lock()
		n, err := reassign(items, to)
		sessionsMu.Unlock()
		journal("reassign %v vers %s : %v", ids(items), to, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_sessions.sessionSReassigneeS", "A1", n, "A2", to)})
	})
	// Recherche dans le texte des conversations (au moins 2 caractères).
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if n := utf8.RuneCountInString(strings.TrimSpace(q)); n < 2 || n > 200 {
			writeJSON(w, 400, map[string]any{"hits": []Hit{}})
			return
		}
		writeJSON(w, 200, map[string]any{"hits": searchAll(q)})
	})
	mux.HandleFunc("POST /rename", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Dir, ID, Title string
			Auto           bool
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		p, ok := transcript(req.Dir, req.ID)
		title := strings.TrimSpace(spaceRe.ReplaceAllString(req.Title, " "))
		if !ok || (title == "" && !req.Auto) || utf8.RuneCountInString(title) > 200 {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.routes_sessions.titreOuSessionInvalide")})
			return
		}
		// Une session ouverte réécrit son propre titre : le nôtre serait écrasé.
		live, lerr := liveCheck()
		if lerr != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": lerr.Error()})
			return
		}
		if live[req.ID] {
			writeJSON(w, 200, map[string]any{"ok": false, "out": L("srv.routes_sessions.sessionOuverteRenommeLa")})
			return
		}
		if req.Auto {
			var terr error
			if title, terr = autoTitle(p); terr != nil {
				writeJSON(w, 200, map[string]any{"ok": false, "out": L("srv.routes_sessions.titreAutomatiqueImpossible", "A1", terr.Error())})
				return
			}
		}
		err := setTitle(p, req.ID, title)
		journal("rename %s « %s » : %v", req.ID, title, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_sessions.renommee", "A1", title)})
	})
	// Snippets = commandes Claude Code (commands.go) : personnelles (~/.claude/commands, ou GLD_WEB_SNIPPETS) et de projet.
	mux.HandleFunc("POST /vgroups", func(w http.ResponseWriter, r *http.Request) {
		var st Store
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&st) != nil || st.Groups == nil || st.Favs == nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		groupsMu.Lock()
		if st.Tags == nil || st.Projects == nil || st.Dirs == nil { // un client qui ne connaît pas un champ ne l'efface pas
			old := loadStore()
			if st.Dirs == nil {
				st.Dirs = old.Dirs
			}
			if st.Tags == nil {
				st.Tags = old.Tags
			}
			if st.Projects == nil {
				st.Projects = old.Projects
			}
		}
		err := saveStore(st)
		groupsMu.Unlock()
		journal("vgroups %d groupe(s), %d favori(s) : %v", len(st.Groups), len(st.Favs), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.routes_sessions.groupesEnregistres")})
	})
	// Rouvre dans un onglet de terminal via gld-matin : une session (id) ou les récentes du projet.
	// Le cwd vient du transcript côté serveur, jamais du navigateur.
	mux.HandleFunc("POST /open", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir, ID, Args, Prompt string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		var p string
		args, err := claudeArgs(req.Args)
		if err == nil {
			err = checkResumePrompt(req.ID, req.Prompt)
		}
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		if req.ID != "" {
			var ok bool
			if p, ok = transcript(req.Dir, req.ID); !ok {
				writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.idInvalide")})
				return
			}
			args = append(args, "--id", req.ID)
			if req.Prompt != "" { // reprise avec un message : transmis tel quel, gld-matin l'échappe pour le shell
				args = append(args, "--prompt", req.Prompt)
			}
		} else if strings.ContainsAny(req.Dir, "/\\") || req.Dir == "" || req.Dir == ".." {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.routes_sessions.dossierInvalide")})
			return
		} else if fs, _ := filepath.Glob(filepath.Join(root, req.Dir, "*.jsonl")); len(fs) > 0 {
			p = fs[0]
		}
		st, err := os.Stat(p)
		if err != nil {
			writeJSON(w, 404, map[string]any{"ok": false, "out": L("srv.routes_sessions.aucuneSession")})
			return
		}
		cwd := info(p, st).Cwd
		if fi, err := os.Stat(cwd); cwd == "" || err != nil || !fi.IsDir() {
			writeJSON(w, 200, map[string]any{"ok": false, "out": L("srv.routes_sessions.dossierDuProjetIntrouvable", "A1", cwd)})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		out, err := matin(ctx, append(args, cwd)...)
		writeJSON(w, 200, map[string]any{"ok": err == nil, "out": out})
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
