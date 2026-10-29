package main

// Terminal complet dans le navigateur, désactivé par défaut (GLD_WEB_TERMINAL=1). Il ne lance jamais de shell : il se branche, par
// une session tmux « groupée » qui ne perturbe pas le terminal de l'hôte, sur le panneau d'une session Claude déjà ouverte,
// retrouvée par son identifiant. Verrous : ouverture par POST (protégé contre le CSRF) qui donne un jeton à usage unique de 30 s,
// Origin obligatoire et identique au Host sur le WebSocket, un seul client par session, lecture seule tant que la page n'a pas
// demandé la main, délai d'inactivité. Le jeton d'accès de gld (écoute hors localhost) s'applique comme pour toute route.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

func termEnabled() bool { return os.Getenv("GLD_WEB_TERMINAL") == "1" }

const termIdle = 70 * time.Minute // au-delà du cache de prompt (1 h) : une session laissée ouverte garde son cache

var (
	termMu   sync.Mutex
	termToks = map[string]termTok{}
	termBusy = map[string]bool{}
	tmuxRef  = regexp.MustCompile(`^[$@%][0-9]+$`)
)

type termTok struct {
	pane, sess, win string
	cols, rows      int
	id              string
	exp             time.Time
}

func termToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// termGeometry : session, fenêtre et taille de la fenêtre qui porte ce panneau.
func termGeometry(pane string) (sess, win string, cols, rows int, err error) {
	out, err := tmuxRun("", "display-message", "-p", "-t", pane, "#{session_id} #{window_id} #{window_width} #{window_height}")
	if err != nil {
		return
	}
	f := strings.Fields(out)
	if len(f) != 4 || !tmuxRef.MatchString(f[0]) || !tmuxRef.MatchString(f[1]) {
		err = terr("srv.termweb.reponseTmuxInattendue")
		return
	}
	cols, _ = strconv.Atoi(f[2])
	rows, _ = strconv.Atoi(f[3])
	if cols < 10 || rows < 3 || cols > 500 || rows > 300 {
		err = terr("srv.termweb.tailleDeFenetreInattendue")
		return
	}
	return f[0], f[1], cols, rows, nil
}

func routesTerm(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/terminal", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"enabled": termEnabled()})
	})
	mux.HandleFunc("POST /v1/terminal", func(w http.ResponseWriter, r *http.Request) {
		fail := func(code int, slug, msg string) { writeErr(w, code, slug, msg) }
		if !termEnabled() {
			fail(403, "forbidden", L("srv.termweb.disabled"))
			return
		}
		var req struct{ ID string }
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		if !idRe.MatchString(req.ID) {
			fail(400, "bad_id", L("srv.http_util.idInvalide"))
			return
		}
		ids, err := sendable()
		pane, ok := ids[req.ID]
		if err != nil || !ok || !paneRe.MatchString(pane) {
			fail(409, "conflict", L("srv.sendkeys.cetteSessionNEst2"))
			return
		}
		sess, win, cols, rows, err := termGeometry(pane)
		if err != nil {
			fail(409, "conflict", err.Error())
			return
		}
		t := termToken()
		termMu.Lock()
		for k, v := range termToks {
			if time.Now().After(v.exp) {
				delete(termToks, k)
			}
		}
		termToks[t] = termTok{pane, sess, win, cols, rows, req.ID, time.Now().Add(30 * time.Second)}
		termMu.Unlock()
		writeJSON(w, 201, map[string]any{"token": t, "cols": cols, "rows": rows})
	})
	mux.HandleFunc("GET /v1/terminal/ws", termWS)
}

func termWS(w http.ResponseWriter, r *http.Request) {
	if !termEnabled() {
		writeErr(w, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
		return
	}
	behindGateway := childToken != ""                   // la passerelle a vérifié l'Origin puis l'a retiré (homes.go)
	if !behindGateway && r.Header.Get("Origin") == "" { // un navigateur l'envoie toujours ; Accept vérifie ensuite qu'elle correspond au Host
		writeErr(w, http.StatusForbidden, "forbidden", L("srv.share_guest.refuse"))
		return
	}
	termMu.Lock()
	t, ok := termToks[r.URL.Query().Get("t")]
	delete(termToks, r.URL.Query().Get("t")) // à usage unique
	busy := termBusy[t.id]
	if ok && time.Now().Before(t.exp) && !busy {
		termBusy[t.id] = true
	}
	termMu.Unlock()
	if !ok || time.Now().After(t.exp) {
		writeErr(w, http.StatusForbidden, "forbidden", L("srv.termweb.badToken"))
		return
	}
	if busy {
		writeErr(w, http.StatusConflict, "conflict", L("srv.termweb.alreadyOpen"))
		return
	}
	defer func() { termMu.Lock(); delete(termBusy, t.id); termMu.Unlock() }()

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: behindGateway})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 16)

	// La fenêtre prend la taille du dernier client actif (le navigateur), puis reprend son réglage à la fermeture.
	tmuxRun("", "set-option", "-w", "-t", t.win, "window-size", "latest")
	defer tmuxRun("", "set-option", "-wu", "-t", t.win, "window-size")
	name := "gld-web-" + termToken()[:8]
	cmd := exec.Command("tmux", "new-session", "-t", t.sess, "-s", name, ";",
		"set-option", "destroy-unattached", "on", ";", "select-window", "-t", t.win, ";", "select-pane", "-t", t.pane)
	cmd.Env = append(environNoAuth(), "TERM=xterm-256color")
	applyRunAs(cmd) // avant pty.StartWithSize, qui complète SysProcAttr sans le remplacer
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(t.rows), Cols: uint16(t.cols)})
	if err != nil {
		c.Close(websocket.StatusInternalError, "tmux inutilisable")
		return
	}
	journal("terminal ouvert sur la session %s", t.id[:8])
	defer func() {
		f.Close()
		cmd.Process.Kill()
		cmd.Wait()
		tmuxRun("", "kill-session", "-t", name)
		journal("terminal fermé sur la session %s", t.id[:8])
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() { // sortie du terminal -> navigateur
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if n > 0 && c.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
				break
			}
			if err != nil {
				break
			}
		}
		cancel()
		c.Close(websocket.StatusNormalClosure, L("srv.termweb.ended"))
	}()

	writable := false
	for { // navigateur -> terminal : binaire = frappes, texte = commande {"write": bool}
		rc, cancelRead := context.WithTimeout(ctx, termIdle)
		typ, data, err := c.Read(rc)
		cancelRead()
		if err != nil {
			return
		}
		if authWS(r, RightExec) != nil {
			c.Close(websocket.StatusPolicyViolation, "")
			return
		}
		if typ == websocket.MessageText {
			var m struct {
				Write  *bool
				Resize *struct{ Cols, Rows int }
			}
			if json.Unmarshal(data, &m) == nil && m.Resize != nil {
				if z := m.Resize; z.Cols >= 20 && z.Cols <= 500 && z.Rows >= 5 && z.Rows <= 300 {
					pty.Setsize(f, &pty.Winsize{Rows: uint16(z.Rows), Cols: uint16(z.Cols)})
				}
			}
			if json.Unmarshal(data, &m) == nil && m.Write != nil {
				writable = *m.Write
				journal("terminal %s : %s", t.id[:8], map[bool]string{true: "main prise", false: "lecture seule"}[writable])
			}
			continue
		}
		if writable {
			f.Write(data)
		}
	}
}
