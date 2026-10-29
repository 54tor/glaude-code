// Conversation depuis le navigateur : `claude -p --resume <id>` continue la session sans terminal,
// et sa sortie stream-json est relayée ligne à ligne (NDJSON) au lecteur.
package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Modes d'autorisation proposés : sans terminal, Claude ne peut pas demander la permission,
// donc par défaut tout ce qui en exige une (écrire, lancer une commande) est refusé.
var chatModes = []string{"", "plan", "acceptEdits", "auto"}

var (
	chatMu  sync.Mutex
	chatRun = map[string]*exec.Cmd{} // session -> claude -p en cours
)

// chatAllowed : les seules options de Réglages transmises au chat. Sans terminal, personne ne voit ce que fait Claude :
// les autorisations ne viennent que du menu du chat (chatModes), jamais d'une option comme --dangerously-skip-permissions,
// --allowedTools, --add-dir ou --mcp-config, réservées aux sessions ouvertes dans un terminal.
var chatAllowed = map[string]bool{"--model": true, "--fallback-model": true, "--effort": true}

// claudeFlags valide les options de Réglages (drapeaux simples seulement) et ne garde que celles permises au chat ;
// ignored liste les autres, pour le signaler.
func claudeFlags(raw string) (kept, ignored []string, err error) {
	for _, f := range strings.Fields(raw) {
		if !flagRe.MatchString(f) {
			return nil, nil, terr("srv.chat.optionRefusee", "A1", f)
		}
		if name, _, _ := strings.Cut(f, "="); chatAllowed[name] {
			kept = append(kept, f)
		} else {
			ignored = append(ignored, f)
		}
	}
	return kept, ignored, nil
}

// chatArgs : la ligne de commande de claude. Le message vient après « -- » : même s'il commence par « - »,
// ce n'est jamais une option.
func chatArgs(flags []string, id, mode, prompt string) []string {
	a := append(slices.Clone(flags), "-p", "--resume", id, "--output-format", "stream-json", "--verbose")
	if mode != "" {
		a = append(a, "--permission-mode", mode)
	}
	// Une image déposée par l'interface se lit avec @chemin : son dossier est ajouté (avant « -- », car l'option prend des listes).
	if strings.Contains(prompt, imagesDir()+string(filepath.Separator)) {
		a = append(a, "--add-dir", imagesDir())
	}
	return append(a, "--", prompt)
}

type chatEvent struct {
	Msgs  []Msg   `json:"msgs,omitempty"`
	Done  bool    `json:"done,omitempty"`
	OK    bool    `json:"ok,omitempty"`
	Error string  `json:"error,omitempty"`
	Cost  float64 `json:"cost,omitempty"`
	Note  string  `json:"note,omitempty"`
}

var upstreamLimit = regexp.MustCompile(`\b(429|529)\b`)

// relay traduit la sortie stream-json de claude en événements pour le lecteur.
func relay(out io.Reader, emit func(chatEvent)) (sawResult bool) {
	r := bufio.NewReaderSize(out, 1<<20)
	for {
		b, err := r.ReadBytes('\n')
		if len(b) > 0 {
			var h struct {
				Type, Subtype, Result string
				IsError               bool    `json:"is_error"`
				Cost                  float64 `json:"total_cost_usd"`
				Attempt               int     `json:"attempt"`
				MaxRetries            int     `json:"max_retries"`
				DelayMs               int     `json:"retry_delay_ms"`
				Status                int     `json:"error_status"`
				Level, Content        string
			}
			if json.Unmarshal(b, &h) == nil {
				switch h.Type {
				case "assistant", "user":
					if ms := lineMsgs(b, true); len(ms) > 0 {
						emit(chatEvent{Msgs: ms})
					}
				case "system":
					if h.Subtype == "api_retry" && (h.Status == 429 || h.Status == 529) {
						emit(chatEvent{Note: L("srv.chat.apiRetry", "A1", strconv.Itoa(h.Status), "A2", strconv.Itoa(h.Attempt), "A3", strconv.Itoa(h.MaxRetries), "A4", strconv.Itoa((h.DelayMs+999)/1000))})
					} else if h.Subtype == "informational" && h.Level == "warning" && strings.TrimSpace(h.Content) != "" {
						emit(chatEvent{Note: cut(strings.TrimSpace(h.Content), 500)})
					}
				case "result":
					sawResult = true
					e := chatEvent{Done: true, OK: !h.IsError && h.Subtype == "success", Cost: h.Cost}
					if !e.OK {
						e.Error = strings.TrimSpace(h.Subtype + " " + cut(h.Result, 500))
						if m := upstreamLimit.FindString(h.Result); m != "" {
							e.Error = L("srv.chat.apiLimit", "A1", m) + " : " + e.Error
						}
					}
					emit(e)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct{ Prompt, Args, Mode string }
	if !decodeBody(w, r, &req, 1<<16) {
		return
	}
	p, found := transcript(r.URL.Query().Get("dir"), id)
	flags, ignored, err := claudeFlags(req.Args)
	status := http.StatusBadRequest
	switch {
	case !found:
		err, status = terr("srv.chat.sessionInvalide"), http.StatusNotFound
	case err != nil:
	case strings.TrimSpace(req.Prompt) == "":
		err = terr("srv.chat.messageVide")
	case !slices.Contains(chatModes, req.Mode):
		err = terr("srv.chat.modeDAutorisationInconnu")
	default:
		err = checkPrompt(id, req.Prompt) // aucun shell ici : le message est un simple argument, Windows compris
	}
	if err != nil {
		writeErr(w, status, "bad_request", err.Error())
		return
	}
	st, _ := os.Stat(p)
	cwd := info(p, st).Cwd
	if fi, err := os.Stat(cwd); cwd == "" || err != nil || !fi.IsDir() {
		writeErr(w, http.StatusNotFound, "not_found", L("srv.routes_sessions.dossierDuProjetIntrouvable", "A1", cwd))
		return
	}

	chatMu.Lock()
	if chatRun[id] != nil {
		chatMu.Unlock()
		writeErr(w, http.StatusConflict, "conflict", L("srv.chat.claudeRepondDejaDans"))
		return
	}
	// Pas lié à la requête : fermer l'onglet ne coupe pas Claude au milieu d'un tour (seul « Arrêter » le fait).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	cmd := withPath(exec.CommandContext(ctx, "claude", withGuard(chatArgs(flags, id, req.Mode, req.Prompt))...))
	cmd.Dir = cwd
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		chatMu.Unlock()
		cancel()
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	chatRun[id] = cmd
	chatMu.Unlock()
	// Session révoquée ou droit retiré (withAuthWatch) : le tour s'arrête, contrairement à un onglet fermé.
	stopRevoked := context.AfterFunc(r.Context(), func() {
		if errors.Is(context.Cause(r.Context()), errAuthRevoked) {
			cancel()
		}
	})
	defer stopRevoked()
	journal("chat %s mode=%q options=%v ignorées=%v (%d caractères)", id, req.Mode, flags, ignored, len(req.Prompt))
	defer func() {
		cancel()
		chatMu.Lock()
		delete(chatRun, id)
		chatMu.Unlock()
	}()

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fl, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	emit := func(e chatEvent) {
		if enc.Encode(e) == nil && fl != nil {
			fl.Flush()
		}
	}
	if len(ignored) > 0 {
		emit(chatEvent{Note: L("srv.chat.ignoredOptions", "A1", strings.Join(ignored, " "))})
	}
	saw := relay(out, emit)
	werr := cmd.Wait()
	if !saw { // claude s'est arrêté sans résultat : arrêt demandé, option refusée, échec au lancement…
		msg := strings.TrimSpace(cut(stderr.String(), 800))
		if msg == "" && werr != nil {
			msg = werr.Error()
		}
		emit(chatEvent{Done: true, Error: msg})
	}
}

func chatStopHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	chatMu.Lock()
	cmd := chatRun[id]
	chatMu.Unlock()
	if cmd == nil || cmd.Process == nil {
		writeErr(w, http.StatusNotFound, "not_found", L("srv.chat.rienEnCours"))
		return
	}
	if cmd.Process.Signal(os.Interrupt) != nil { // Windows : pas de signal, on termine le processus
		cmd.Process.Kill()
	}
	writeJSON(w, 200, map[string]any{"message": L("srv.chat.arretDemande")})
}

// newSessionHandler ouvre une nouvelle session claude, dans le dossier de travail d'une session existante (dir/id) ou dans un dossier
// choisi (Cwd, absolu et existant),
// avec un prompt (ex. les références des sessions d'un groupe). Via gld-matin (matin.go), qui passe le prompt par un fichier temporaire.
func newSessionHandler(w http.ResponseWriter, r *http.Request) {
	var req struct{ Dir, ID, Prompt, Args, Cwd string }
	if !decodeBody(w, r, &req, 1<<16) {
		return
	}
	p, ok := transcript(req.Dir, req.ID)
	if req.Cwd != "" {
		ok = filepath.IsAbs(req.Cwd) && !strings.ContainsRune(req.Cwd, 0)
	}
	args, err := claudeArgs(req.Args)
	switch {
	case !ok && req.Cwd != "":
		err = terr("srv.chat.dossierInvalideCheminAbsolu")
	case !ok:
		err = terr("srv.chat.sessionInvalide")
	case err != nil:
	case strings.TrimSpace(req.Prompt) == "":
		err = terr("srv.chat.promptVide")
	default:
		id := req.ID
		if req.Cwd != "" {
			id = "-" // aucune session visée : seule la validité du texte compte
		}
		err = checkResumePrompt(id, req.Prompt)
		if errors.Is(err, errOpen) || errors.Is(err, errLive) {
			err = nil // une session ouverte n'empêche pas d'en démarrer une nouvelle à côté (même sans savoir lesquelles le sont)
		}
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	cwd := filepath.Clean(req.Cwd)
	if req.Cwd == "" {
		st, _ := os.Stat(p)
		cwd = info(p, st).Cwd
	}
	if fi, err := os.Stat(cwd); cwd == "" || err != nil || !fi.IsDir() {
		writeErr(w, http.StatusNotFound, "not_found", L("srv.routes_sessions.dossierDuProjetIntrouvable", "A1", cwd))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out, err := matin(ctx, append(args, "--prompt", req.Prompt, cwd)...)
	journal("new-session dans %s (%d caractères) : %v", cwd, len(req.Prompt), errOr(err))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "launch_failed", cmp.Or(out, err.Error()))
		return
	}
	writeJSON(w, 201, map[string]any{"message": out})
}
