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
	for _, flag := range strings.Fields(raw) {
		if !flagRe.MatchString(flag) {
			return nil, nil, terr("srv.chat.optionRefusee", "A1", flag)
		}
		if name, _, _ := strings.Cut(flag, "="); chatAllowed[name] {
			kept = append(kept, flag)
		} else {
			ignored = append(ignored, flag)
		}
	}
	return kept, ignored, nil
}

// chatArgs : la ligne de commande de claude. Le message vient après « -- » : même s'il commence par « - »,
// ce n'est jamais une option.
func chatArgs(flags []string, id, mode, prompt string) []string {
	args := append(slices.Clone(flags), "-p", "--resume", id, "--output-format", "stream-json", "--verbose")
	if mode != "" {
		args = append(args, "--permission-mode", mode)
	}
	// Une image déposée par l'interface se lit avec @chemin : son dossier est ajouté (avant « -- », car l'option prend des listes).
	if strings.Contains(prompt, imagesDir()+string(filepath.Separator)) {
		args = append(args, "--add-dir", imagesDir())
	}
	return append(args, "--", prompt)
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

const (
	upstreamOverloadedStatus = 529
	chatNoteMaxLen           = 500
	chatStderrMaxLen         = 800
	chatBodyMax              = 1 << 16
	chatTurnTimeout          = 30 * time.Minute
	newSessionTimeout        = 30 * time.Second
)

// relay traduit la sortie stream-json de claude en événements pour le lecteur.
func relay(output io.Reader, emit func(chatEvent)) (sawResult bool) {
	reader := bufio.NewReaderSize(output, jsonLineBufferSize)
	for {
		streamLine, err := reader.ReadBytes('\n')
		if len(streamLine) > 0 {
			var lineHeader struct {
				Type, Subtype, Result string
				IsError               bool    `json:"is_error"`
				Cost                  float64 `json:"total_cost_usd"`
				Attempt               int     `json:"attempt"`
				MaxRetries            int     `json:"max_retries"`
				DelayMs               int     `json:"retry_delay_ms"`
				Status                int     `json:"error_status"`
				Level, Content        string
			}
			if json.Unmarshal(streamLine, &lineHeader) == nil {
				switch lineHeader.Type {
				case "assistant", "user":
					if messages := lineMsgs(streamLine, true); len(messages) > 0 {
						emit(chatEvent{Msgs: messages})
					}
				case "system":
					isUpstreamLimit := lineHeader.Status == http.StatusTooManyRequests || lineHeader.Status == upstreamOverloadedStatus
					isUpstreamRetry := lineHeader.Subtype == "api_retry" && isUpstreamLimit
					warningText := strings.TrimSpace(lineHeader.Content)
					isWarning := lineHeader.Subtype == "informational" && lineHeader.Level == "warning" && warningText != ""
					if isUpstreamRetry {
						delaySeconds := (lineHeader.DelayMs + millisecondsPerSecond - 1) / millisecondsPerSecond
						emit(chatEvent{Note: L("srv.chat.apiRetry",
							"A1", strconv.Itoa(lineHeader.Status),
							"A2", strconv.Itoa(lineHeader.Attempt),
							"A3", strconv.Itoa(lineHeader.MaxRetries),
							"A4", strconv.Itoa(delaySeconds))})
					} else if isWarning {
						emit(chatEvent{Note: cut(warningText, chatNoteMaxLen)})
					}
				case "result":
					sawResult = true
					succeeded := !lineHeader.IsError && lineHeader.Subtype == "success"
					resultEvent := chatEvent{Done: true, OK: succeeded, Cost: lineHeader.Cost}
					if !resultEvent.OK {
						resultEvent.Error = strings.TrimSpace(lineHeader.Subtype + " " + cut(lineHeader.Result, chatNoteMaxLen))
						if limitCode := upstreamLimit.FindString(lineHeader.Result); limitCode != "" {
							resultEvent.Error = L("srv.chat.apiLimit", "A1", limitCode) + " : " + resultEvent.Error
						}
					}
					emit(resultEvent)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func chatHandler(responseWriter http.ResponseWriter, request *http.Request) {
	id, isValid := pathID(responseWriter, request, "id")
	if !isValid {
		return
	}
	var payload struct{ Prompt, Args, Mode string }
	if !decodeBody(responseWriter, request, &payload, chatBodyMax) {
		return
	}
	transcriptPath, found := transcript(request.URL.Query().Get("dir"), id)
	flags, ignored, err := claudeFlags(payload.Args)
	status := http.StatusBadRequest
	switch {
	case !found:
		err, status = terr("srv.chat.sessionInvalide"), http.StatusNotFound
	case err != nil:
	case strings.TrimSpace(payload.Prompt) == "":
		err = terr("srv.chat.messageVide")
	case !slices.Contains(chatModes, payload.Mode):
		err = terr("srv.chat.modeDAutorisationInconnu")
	default:
		err = checkPrompt(id, payload.Prompt) // aucun shell ici : le message est un simple argument, Windows compris
	}
	if err != nil {
		writeErr(responseWriter, status, "bad_request", err.Error())
		return
	}
	transcriptInfo, _ := os.Stat(transcriptPath)
	cwd := info(transcriptPath, transcriptInfo).Cwd
	cwdInfo, statErr := os.Stat(cwd)
	cwdMissing := cwd == "" || statErr != nil || !cwdInfo.IsDir()
	if cwdMissing {
		writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.routes_sessions.dossierDuProjetIntrouvable", "A1", cwd))
		return
	}

	chatMu.Lock()
	if chatRun[id] != nil {
		chatMu.Unlock()
		writeErr(responseWriter, http.StatusConflict, "conflict", L("srv.chat.claudeRepondDejaDans"))
		return
	}
	// Pas lié à la requête : fermer l'onglet ne coupe pas Claude au milieu d'un tour (seul « Arrêter » le fait).
	runContext, cancel := context.WithTimeout(context.Background(), chatTurnTimeout)
	commandArgs := withGuard(append(builtinPromptArgs(), chatArgs(flags, id, payload.Mode, payload.Prompt)...))
	cmd := withPath(exec.CommandContext(runContext, "claude", commandArgs...))
	cmd.Dir = cwd
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		chatMu.Unlock()
		cancel()
		writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	chatRun[id] = cmd
	chatMu.Unlock()
	// Session révoquée ou droit retiré (withAuthWatch) : le tour s'arrête, contrairement à un onglet fermé.
	stopRevoked := context.AfterFunc(request.Context(), func() {
		if errors.Is(context.Cause(request.Context()), errAuthRevoked) {
			cancel()
		}
	})
	defer stopRevoked()
	journal("chat %s mode=%q options=%v ignorées=%v (%d caractères)", id, payload.Mode, flags, ignored, len(payload.Prompt))
	defer func() {
		cancel()
		chatMu.Lock()
		delete(chatRun, id)
		chatMu.Unlock()
	}()

	responseWriter.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	responseWriter.Header().Set("Cache-Control", "no-store")
	flusher, _ := responseWriter.(http.Flusher)
	encoder := json.NewEncoder(responseWriter)
	emit := func(event chatEvent) {
		if encoder.Encode(event) == nil && flusher != nil {
			flusher.Flush()
		}
	}
	if len(ignored) > 0 {
		emit(chatEvent{Note: L("srv.chat.ignoredOptions", "A1", strings.Join(ignored, " "))})
	}
	sawResult := relay(stdout, emit)
	waitErr := cmd.Wait()
	if !sawResult { // claude s'est arrêté sans résultat : arrêt demandé, option refusée, échec au lancement…
		errorMessage := strings.TrimSpace(cut(stderr.String(), chatStderrMaxLen))
		if errorMessage == "" && waitErr != nil {
			errorMessage = waitErr.Error()
		}
		emit(chatEvent{Done: true, Error: errorMessage})
	}
}

func chatStopHandler(responseWriter http.ResponseWriter, request *http.Request) {
	id, isValid := pathID(responseWriter, request, "id")
	if !isValid {
		return
	}
	chatMu.Lock()
	cmd := chatRun[id]
	chatMu.Unlock()
	if cmd == nil || cmd.Process == nil {
		writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.chat.rienEnCours"))
		return
	}
	if cmd.Process.Signal(os.Interrupt) != nil { // Windows : pas de signal, on termine le processus
		cmd.Process.Kill()
	}
	writeJSON(responseWriter, http.StatusOK, map[string]any{"message": L("srv.chat.arretDemande")})
}

// newSessionHandler ouvre une nouvelle session claude, dans le dossier de travail d'une session existante (dir/id) ou dans un dossier
// choisi (Cwd, absolu et existant),
// avec un prompt (ex. les références des sessions d'un groupe). Via gld-matin (matin.go), qui passe le prompt par un fichier temporaire.
func newSessionHandler(responseWriter http.ResponseWriter, request *http.Request) {
	var payload struct{ Dir, ID, Prompt, Args, Cwd string }
	if !decodeBody(responseWriter, request, &payload, chatBodyMax) {
		return
	}
	transcriptPath, isValid := transcript(payload.Dir, payload.ID)
	if payload.Cwd != "" {
		isValid = filepath.IsAbs(payload.Cwd) && !strings.ContainsRune(payload.Cwd, 0)
	}
	args, err := claudeArgs(payload.Args)
	switch {
	case !isValid && payload.Cwd != "":
		err = terr("srv.chat.dossierInvalideCheminAbsolu")
	case !isValid:
		err = terr("srv.chat.sessionInvalide")
	case err != nil:
	case strings.TrimSpace(payload.Prompt) == "":
		err = terr("srv.chat.promptVide")
	default:
		resumeID := payload.ID
		if payload.Cwd != "" {
			resumeID = "-" // aucune session visée : seule la validité du texte compte
		}
		err = checkResumePrompt(resumeID, payload.Prompt)
		if errors.Is(err, errOpen) || errors.Is(err, errLive) {
			err = nil // une session ouverte n'empêche pas d'en démarrer une nouvelle à côté (même sans savoir lesquelles le sont)
		}
	}
	if err != nil {
		writeErr(responseWriter, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	cwd := filepath.Clean(payload.Cwd)
	if payload.Cwd == "" {
		transcriptInfo, _ := os.Stat(transcriptPath)
		cwd = info(transcriptPath, transcriptInfo).Cwd
	}
	cwdInfo, statErr := os.Stat(cwd)
	cwdMissing := cwd == "" || statErr != nil || !cwdInfo.IsDir()
	if cwdMissing {
		writeErr(responseWriter, http.StatusNotFound, "not_found", L("srv.routes_sessions.dossierDuProjetIntrouvable", "A1", cwd))
		return
	}
	launchContext, cancel := context.WithTimeout(request.Context(), newSessionTimeout)
	defer cancel()
	launchOutput, err := matin(launchContext, append(args, "--prompt", payload.Prompt, cwd)...)
	journal("new-session dans %s (%d caractères) : %v", cwd, len(payload.Prompt), errOr(err))
	if err != nil {
		writeErr(responseWriter, http.StatusInternalServerError, "launch_failed", cmp.Or(launchOutput, err.Error()))
		return
	}
	writeJSON(responseWriter, http.StatusCreated, map[string]any{"message": launchOutput})
}
