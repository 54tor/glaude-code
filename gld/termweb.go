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
	randomBytes := make([]byte, 16)
	rand.Read(randomBytes)
	return hex.EncodeToString(randomBytes)
}

// termGeometry : session, fenêtre et taille de la fenêtre qui porte ce panneau.
func termGeometry(pane string) (sess, win string, cols, rows int, err error) {
	geometry, err := tmuxRun("", "display-message", "-p", "-t", pane, "#{session_id} #{window_id} #{window_width} #{window_height}")
	if err != nil {
		return
	}
	fields := strings.Fields(geometry)
	if len(fields) != 4 || !tmuxRef.MatchString(fields[0]) || !tmuxRef.MatchString(fields[1]) {
		err = terr("srv.termweb.reponseTmuxInattendue")
		return
	}
	cols, _ = strconv.Atoi(fields[2])
	rows, _ = strconv.Atoi(fields[3])
	if cols < 10 || rows < 3 || cols > 500 || rows > 300 {
		err = terr("srv.termweb.tailleDeFenetreInattendue")
		return
	}
	return fields[0], fields[1], cols, rows, nil
}

func routesTerm(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/terminal", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, 200, map[string]any{"enabled": termEnabled()})
	})
	mux.HandleFunc("POST /v1/terminal", func(responseWriter http.ResponseWriter, request *http.Request) {
		fail := func(code int, slug, message string) { writeErr(responseWriter, code, slug, message) }
		if !termEnabled() {
			fail(403, "forbidden", L("srv.termweb.disabled"))
			return
		}
		var body struct{ ID string }
		if !decodeBody(responseWriter, request, &body, 1<<10) {
			return
		}
		if !idRe.MatchString(body.ID) {
			fail(400, "bad_id", L("srv.http_util.idInvalide"))
			return
		}
		panesBySession, err := sendable()
		pane, found := panesBySession[body.ID]
		if err != nil || !found || !paneRe.MatchString(pane) {
			fail(409, "conflict", L("srv.sendkeys.cetteSessionNEst2"))
			return
		}
		sess, win, cols, rows, err := termGeometry(pane)
		if err != nil {
			fail(409, "conflict", err.Error())
			return
		}
		token := termToken()
		termMu.Lock()
		for tokenKey, pending := range termToks {
			if time.Now().After(pending.exp) {
				delete(termToks, tokenKey)
			}
		}
		termToks[token] = termTok{pane, sess, win, cols, rows, body.ID, time.Now().Add(30 * time.Second)}
		termMu.Unlock()
		writeJSON(responseWriter, 201, map[string]any{"token": token, "cols": cols, "rows": rows})
	})
	mux.HandleFunc("GET /v1/terminal/ws", termWS)
}

func termWS(responseWriter http.ResponseWriter, request *http.Request) {
	if !termEnabled() {
		writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.http_util.introuvable"))
		return
	}
	behindGateway := childToken != ""                         // la passerelle a vérifié l'Origin puis l'a retiré (homes.go)
	if !behindGateway && request.Header.Get("Origin") == "" { // un navigateur l'envoie toujours ; Accept vérifie ensuite qu'elle correspond au Host
		writeErr(responseWriter, http.StatusForbidden, "forbidden", L("srv.share_guest.refuse"))
		return
	}
	termMu.Lock()
	ticket, found := termToks[request.URL.Query().Get("t")]
	delete(termToks, request.URL.Query().Get("t")) // à usage unique
	busy := termBusy[ticket.id]
	if found && time.Now().Before(ticket.exp) && !busy {
		termBusy[ticket.id] = true
	}
	termMu.Unlock()
	if !found || time.Now().After(ticket.exp) {
		writeErr(responseWriter, http.StatusForbidden, "forbidden", L("srv.termweb.badToken"))
		return
	}
	if busy {
		writeErr(responseWriter, http.StatusConflict, "conflict", L("srv.termweb.alreadyOpen"))
		return
	}
	defer func() { termMu.Lock(); delete(termBusy, ticket.id); termMu.Unlock() }()

	connection, err := websocket.Accept(responseWriter, request, &websocket.AcceptOptions{InsecureSkipVerify: behindGateway})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	connection.SetReadLimit(1 << 16)

	// La fenêtre prend la taille du dernier client actif (le navigateur), puis reprend son réglage à la fermeture.
	tmuxRun("", "set-option", "-w", "-t", ticket.win, "window-size", "latest")
	defer tmuxRun("", "set-option", "-wu", "-t", ticket.win, "window-size")
	name := "gld-web-" + termToken()[:8]
	cmd := exec.Command("tmux", "new-session", "-t", ticket.sess, "-s", name, ";",
		"set-option", "destroy-unattached", "on", ";", "select-window", "-t", ticket.win, ";", "select-pane", "-t", ticket.pane)
	cmd.Env = append(environNoAuth(), "TERM=xterm-256color")
	applyRunAs(cmd) // avant pty.StartWithSize, qui complète SysProcAttr sans le remplacer
	ptyFile, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(ticket.rows), Cols: uint16(ticket.cols)})
	if err != nil {
		connection.Close(websocket.StatusInternalError, "tmux inutilisable")
		return
	}
	journal("terminal ouvert sur la session %s", ticket.id[:8])
	defer func() {
		ptyFile.Close()
		cmd.Process.Kill()
		cmd.Wait()
		tmuxRun("", "kill-session", "-t", name)
		journal("terminal fermé sur la session %s", ticket.id[:8])
	}()

	sessionContext, cancel := context.WithCancel(request.Context())
	defer cancel()
	go func() { // sortie du terminal -> navigateur
		outputBuffer := make([]byte, 32<<10)
		for {
			readCount, readErr := ptyFile.Read(outputBuffer)
			if readCount > 0 && connection.Write(sessionContext, websocket.MessageBinary, outputBuffer[:readCount]) != nil {
				break
			}
			if readErr != nil {
				break
			}
		}
		cancel()
		connection.Close(websocket.StatusNormalClosure, L("srv.termweb.ended"))
	}()

	writable := false
	for { // navigateur -> terminal : binaire = frappes, texte = commande {"write": bool}
		readContext, cancelRead := context.WithTimeout(sessionContext, termIdle)
		messageType, data, receiveErr := connection.Read(readContext)
		cancelRead()
		if receiveErr != nil {
			return
		}
		if authWS(request, RightExec) != nil {
			connection.Close(websocket.StatusPolicyViolation, "")
			return
		}
		if messageType == websocket.MessageText {
			var command struct {
				Write  *bool
				Resize *struct{ Cols, Rows int }
			}
			if json.Unmarshal(data, &command) == nil && command.Resize != nil {
				if size := command.Resize; size.Cols >= 20 && size.Cols <= 500 && size.Rows >= 5 && size.Rows <= 300 {
					pty.Setsize(ptyFile, &pty.Winsize{Rows: uint16(size.Rows), Cols: uint16(size.Cols)})
				}
			}
			if json.Unmarshal(data, &command) == nil && command.Write != nil {
				writable = *command.Write
				journal("terminal %s : %s", ticket.id[:8], map[bool]string{true: "main prise", false: "lecture seule"}[writable])
			}
			continue
		}
		if writable {
			ptyFile.Write(data)
		}
	}
}
