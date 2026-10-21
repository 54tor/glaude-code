package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// routesSessions : liste, lecture, suppression, réassignation, recherche et réouverture des sessions.
func routesSessions(mux *http.ServeMux) {
	routesTerm(mux)
	home, _ := os.UserHomeDir()
	mux.HandleFunc("GET /api", func(w http.ResponseWriter, r *http.Request) {
		lastAPI.Store(time.Now().UnixNano())
		live := []string{}
		for id := range liveSessions() {
			live = append(live, id)
		}
		groupsMu.Lock()
		st := loadStore()
		groupsMu.Unlock()
		writeJSON(w, 200, map[string]any{"home": home, "root": root, "groups": sessions(), "live": live, "waiting": waiting(), "vgroups": st.Groups, "favs": st.Favs, "tags": st.Tags})
	})
	// Envoi d'un message à une session ouverte dans tmux (sendkeys.go) : lesquelles le permettent, puis l'envoi.
	// État en direct d'une session (sessionlive.go) et informations du compte (account.go).
	mux.HandleFunc("GET /session-live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, sessionLive(r.URL.Query().Get("dir"), r.URL.Query().Get("id")))
	})
	mux.HandleFunc("GET /account", func(w http.ResponseWriter, r *http.Request) {
		a := accountInfo()
		// Relevé trop vieux : on le rafraîchit en arrière-plan (la page relit toutes les minutes) ; refresh=1 le force.
		a.Refreshing = refreshUsage(a.LimitsAt, r.URL.Query().Get("refresh") == "1")
		writeJSON(w, 200, a)
	})
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
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		err := closeSession(req.ID)
		journal("session-close %s : %v", req.ID, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Session fermée"})
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
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		err := sendKey(req.ID, req.Key)
		journal("session-key %s %s : %v", req.ID, req.Key, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Touche envoyée"})
	})
	mux.HandleFunc("POST /session-send", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID, Text string }
		if json.NewDecoder(io.LimitReader(r.Body, sendMax+1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		err := sendToSession(req.ID, req.Text)
		journal("session-send %s (%d caractères) : %v", req.ID, len(req.Text), errOr(err)) // jamais le texte
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Message envoyé à la session"})
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
			writeJSON(w, 400, map[string]any{"error": "chemin absolu requis"})
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
		if lim, _ := strconv.Atoi(q.Get("limit")); lim > 0 { // par la fin : « before » est le décalage rendu par la page suivante
			before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
			msgs, start, more, err := convPage(p, q.Get("tools") == "1", before, min(lim, 5000))
			if err != nil {
				writeJSON(w, 500, map[string]any{})
				return
			}
			writeJSON(w, 200, map[string]any{"msgs": msgs, "start": start, "more": more})
			return
		}
		writeJSON(w, 200, map[string]any{"msgs": conv(p, q.Get("tools") == "1")})
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
		n, spared, notes, err := purgeSessions(items)
		journal("rm %d session(s) : %d supprimée(s), %d épargnée(s) %v", len(items), n, spared, ids(items))
		msg := fmt.Sprintf("%d supprimée(s), %d vivante(s) épargnée(s)", n, spared)
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
		n, err := reassign(items, to)
		journal("reassign %v vers %s : %v", ids(items), to, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": fmt.Sprintf("%d session(s) réassignée(s) vers %s", n, to)})
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
		var req struct{ Dir, ID, Title string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		p, ok := transcript(req.Dir, req.ID)
		title := strings.TrimSpace(spaceRe.ReplaceAllString(req.Title, " "))
		if !ok || title == "" || utf8.RuneCountInString(title) > 200 {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "titre ou session invalide"})
			return
		}
		// Une session ouverte réécrit son propre titre : le nôtre serait écrasé.
		live, lerr := liveCheck()
		if lerr != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": lerr.Error()})
			return
		}
		if live[req.ID] {
			writeJSON(w, 200, map[string]any{"ok": false, "out": "session ouverte : renomme-la avec /rename dans Claude, ou ferme-la d'abord"})
			return
		}
		err := setTitle(p, req.ID, title)
		journal("rename %s « %s » : %v", req.ID, title, errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Renommée : " + title})
	})
	// Snippets = commandes Claude Code (commands.go) : personnelles (~/.claude/commands, ou GLD_WEB_SNIPPETS) et de projet.
	mux.HandleFunc("POST /vgroups", func(w http.ResponseWriter, r *http.Request) {
		var st Store
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&st) != nil || st.Groups == nil || st.Favs == nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		groupsMu.Lock()
		if st.Tags == nil { // un client qui ne connaît pas les étiquettes ne les efface pas
			st.Tags = loadStore().Tags
		}
		err := saveStore(st)
		groupsMu.Unlock()
		journal("vgroups %d groupe(s), %d favori(s) : %v", len(st.Groups), len(st.Favs), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Groupes enregistrés"})
	})
	// Rouvre dans un onglet de terminal via gld-matin : une session (id) ou les récentes du projet.
	// Le cwd vient du transcript côté serveur, jamais du navigateur.
	mux.HandleFunc("POST /open", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Dir, ID, Args, Prompt string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
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
				writeJSON(w, 400, map[string]any{"ok": false, "out": "id invalide"})
				return
			}
			args = append(args, "--id", req.ID)
			if req.Prompt != "" { // reprise avec un message : transmis tel quel, gld-matin l'échappe pour le shell
				args = append(args, "--prompt", req.Prompt)
			}
		} else if strings.ContainsAny(req.Dir, "/\\") || req.Dir == "" || req.Dir == ".." {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "dossier invalide"})
			return
		} else if fs, _ := filepath.Glob(filepath.Join(root, req.Dir, "*.jsonl")); len(fs) > 0 {
			p = fs[0]
		}
		st, err := os.Stat(p)
		if err != nil {
			writeJSON(w, 404, map[string]any{"ok": false, "out": "aucune session"})
			return
		}
		cwd := info(p, st).Cwd
		if fi, err := os.Stat(cwd); cwd == "" || err != nil || !fi.IsDir() {
			writeJSON(w, 200, map[string]any{"ok": false, "out": "dossier du projet introuvable : " + cwd})
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
