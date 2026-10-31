package main

// Envoyer un message à une session ouverte dans un terminal. Seule voie fiable : tmux. Le processus claude de la session est
// retrouvé dans l'arbre des processus d'un panneau tmux, et le texte y est collé (collage « bracketed », donc sans que les
// retours à la ligne soient pris pour des validations) avant une touche Entrée. Rien ne marche pour une session qui n'est pas
// dans tmux, ni hors Linux (l'arbre des processus vient de /proc).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
)

const sendMax = 32 << 10

var paneRe = regexp.MustCompile(`^%[0-9]+$`)

// tmuxRun lance tmux, avec stdin si non vide (remplaçable dans les tests). Borné comme claudeAgents : un serveur tmux figé
// ne bloque pas les routes que l'interface interroge en boucle.
var tmuxRun = func(stdin string, args ...string) (string, error) {
	commandContext, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandContext, "tmux", args...)
	cmd.WaitDelay = time.Second
	applyRunAs(cmd)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var standardOutput, standardError bytes.Buffer
	cmd.Stdout, cmd.Stderr = &standardOutput, &standardError
	err := cmd.Run()
	if err != nil && strings.TrimSpace(standardError.String()) != "" {
		err = errors.New(strings.TrimSpace(standardError.String()))
	}
	return standardOutput.String(), err
}

// sessionPIDs : pid des sessions interactives ouvertes, par id de session (remplaçable dans les tests).
var sessionPIDs = func() map[string]int {
	agentsJSON, err := claudeAgents()
	var agents []struct {
		SessionId, Kind string
		Pid             int
	}
	pidsBySession := map[string]int{}
	if err != nil || json.Unmarshal(agentsJSON, &agents) != nil {
		return pidsBySession
	}
	for _, agent := range agents {
		if agent.SessionId != "" && agent.Pid > 0 && agent.Kind == "interactive" {
			pidsBySession[agent.SessionId] = agent.Pid
		}
	}
	return pidsBySession
}

// backgroundSessions : id court (celui de `claude stop`) des tâches de fond, par id de session (remplaçable dans les tests).
var backgroundSessions = func() map[string]string {
	agentsJSON, err := claudeAgents()
	var agents []struct{ SessionId, Id, Kind string }
	shortIDsBySession := map[string]string{}
	if err != nil || json.Unmarshal(agentsJSON, &agents) != nil {
		return shortIDsBySession
	}
	for _, agent := range agents {
		if agent.SessionId != "" && agent.Id != "" && agent.Kind == "background" {
			shortIDsBySession[agent.SessionId] = agent.Id
		}
	}
	return shortIDsBySession
}

// stopBackground arrête une tâche de fond (`claude stop`) : sa conversation est conservée (remplaçable dans les tests).
var stopBackground = func(shortID string) error {
	commandContext, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	stopCommand := withPath(exec.CommandContext(commandContext, "claude", "stop", shortID))
	stopCommand.WaitDelay = time.Second
	if combinedOutput, err := stopCommand.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(cut(string(combinedOutput), 300)))
	}
	return nil
}

// parentOf : le processus parent (lecture de /proc/<pid>/stat, ou `ps` sous macOS qui n'a pas /proc), 0 si inconnu (remplaçable dans les tests).
var parentOf = func(pid int) int {
	statContent, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		if runtime.GOOS == "darwin" {
			psOutput, _ := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
			parentPID, _ := strconv.Atoi(strings.TrimSpace(string(psOutput)))
			return parentPID
		}
		return 0
	}
	statText := string(statContent)
	nameEndIndex := strings.LastIndex(statText, ")") // le nom du processus peut contenir des espaces et des parenthèses
	if nameEndIndex < 0 {
		return 0
	}
	fieldsAfterName := strings.Fields(statText[nameEndIndex+1:])
	if len(fieldsAfterName) < 2 {
		return 0
	}
	parentPID, _ := strconv.Atoi(fieldsAfterName[1])
	return parentPID
}

// tmuxPanes : panneau tmux (« %12 ») par pid du processus racine du panneau.
func tmuxPanes() (map[int]string, error) {
	paneListing, err := tmuxRun("", "list-panes", "-a", "-F", "#{pane_id} #{pane_pid}")
	if err != nil {
		if strings.Contains(err.Error(), "error connecting") || strings.Contains(err.Error(), "no server") {
			err = terr("srv.sendkeys.aucuneSessionNA")
		}
		return nil, err
	}
	panesByPID := map[int]string{}
	for _, paneLine := range strings.Split(paneListing, "\n") {
		paneID, panePID, hasSeparator := strings.Cut(strings.TrimSpace(paneLine), " ")
		if processID, parseErr := strconv.Atoi(panePID); hasSeparator && parseErr == nil && paneRe.MatchString(paneID) {
			panesByPID[processID] = paneID
		}
	}
	return panesByPID, nil
}

// paneOf : le panneau qui porte le processus pid (lui-même ou un de ses ancêtres).
func paneOf(panes map[int]string, pid int) string {
	for depth := 0; depth < 64 && pid > 1; depth++ {
		if pane, found := panes[pid]; found {
			return pane
		}
		pid = parentOf(pid)
	}
	return ""
}

// sendable : les sessions ouvertes auxquelles on peut écrire, avec leur panneau tmux ; l'erreur dit pourquoi tmux est inutilisable
// (absent, sans serveur).
func sendable() (map[string]string, error) {
	panesBySession := map[string]string{}
	pids := sessionPIDs()
	if len(pids) == 0 {
		return panesBySession, nil
	}
	panes, err := tmuxPanes()
	if err != nil {
		return panesBySession, err
	}
	for sessionID, pid := range pids {
		if pane := paneOf(panes, pid); pane != "" {
			panesBySession[sessionID] = pane
		}
	}
	return panesBySession, nil
}

// checkSendText : du texte brut, sans caractère de contrôle : un ESC pourrait refermer le collage et faire exécuter la suite
// comme des frappes.
func checkSendText(text string) error {
	switch {
	case strings.TrimSpace(text) == "":
		return terr("srv.chat.messageVide")
	case len(text) > sendMax || !utf8.ValidString(text):
		return terr("srv.sendkeys.messageTropLongOu")
	}
	for _, character := range text {
		if character < 0x20 && character != '\n' && character != '\t' || character == 0x7f || character >= 0x80 && character < 0xa0 {
			return terr("srv.sendkeys.leMessageContientUn")
		}
	}
	return nil
}

// closeSession termine le processus claude d'une session interactive ouverte (SIGTERM : il quitte proprement, le transcript est déjà
// écrit au fil de l'eau). Le pid vient d'un relevé frais de `claude agents --json`, jamais du navigateur.
func closeSession(sessionID string) error {
	if !idRe.MatchString(sessionID) {
		return terr("srv.chat.sessionInvalide")
	}
	pid, isInteractive := sessionPIDs()[sessionID]
	if !isInteractive {
		if shortID, isBackground := backgroundSessions()[sessionID]; isBackground {
			stopErr := stopBackground(shortID)
			forgetAgents()
			if stopErr != nil {
				return terr("srv.sendkeys.fermetureImpossible", "W1", stopErr)
			}
			return nil
		}
		return terr("srv.sendkeys.sessionIntrouvableParmiLes")
	}
	if ownershipErr := runAsOwns(pid); ownershipErr != nil {
		return terr("srv.sendkeys.fermetureImpossible", "W1", ownershipErr)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if signalErr := process.Signal(syscall.SIGTERM); signalErr != nil {
		return terr("srv.sendkeys.fermetureImpossible", "W1", signalErr)
	}
	return nil
}

// closeAndWait ferme celles de ces sessions qui sont ouvertes, puis attend (6 s au plus) que leur processus ait fini : tant
// qu'il tourne, purgeSessions épargnerait la session.
func closeAndWait(items []item) {
	stillOpen := map[string]bool{}
	for _, sessionItem := range items {
		_, isBackground := backgroundSessions()[sessionItem.ID]
		if _, isInteractive := sessionPIDs()[sessionItem.ID]; (isInteractive || isBackground) && closeSession(sessionItem.ID) == nil {
			stillOpen[sessionItem.ID] = true
		}
	}
	for attempt := 0; attempt < 30 && len(stillOpen) > 0; attempt++ {
		time.Sleep(200 * time.Millisecond)
		liveIDs := liveSessions()
		for sessionID := range stillOpen {
			if !liveIDs[sessionID] {
				delete(stillOpen, sessionID)
			}
		}
	}
}

// sendToSession colle le texte dans le panneau tmux de la session, puis valide.
// sendSeq : un tampon tmux par envoi, sinon deux envois simultanés (warmTick et un utilisateur) se collent mutuellement leur texte.
var sendSeq atomic.Uint64

func sendToSession(sessionID, text string) error {
	if !idRe.MatchString(sessionID) {
		return terr("srv.chat.sessionInvalide")
	}
	if checkErr := checkSendText(text); checkErr != nil {
		return checkErr
	}
	panesBySession, err := sendable()
	if err != nil {
		return terr("srv.sendkeys.tmuxInutilisableLanceLes", "W1", err)
	}
	pane, found := panesBySession[sessionID]
	if !found {
		return terr("srv.sendkeys.cetteSessionNEst")
	}
	return sendToPane(pane, text)
}

// sendToPane colle le texte dans le panneau puis valide (le texte est déjà contrôlé par l'appelant).
func sendToPane(pane, text string) error {
	// La surcouche de /btw intercepte les touches jusqu'à Échap : sans elle, le texte ne parviendrait pas au prompt.
	if screen, captureErr := capturePane(pane); captureErr == nil && btwOpen(screen) {
		tmuxRun("", "send-keys", "-t", pane, "Escape")
		time.Sleep(150 * time.Millisecond)
	}
	// ponytail: fenêtre résiduelle de 200 ms entre le collage et l'Entrée, pendant laquelle un dialogue pourrait apparaître ; pas de parade côté tmux
	// Un texte qui finit sur « @chemin » ouvre le menu de fichiers de Claude Code, qui avale Entrée : l'espace final le ferme.
	if lastSeparatorIndex := strings.LastIndexAny(text, " \n"); strings.HasPrefix(text[lastSeparatorIndex+1:], "@") {
		text += " "
	}
	// Une commande « /… » collée derrière un texte déjà tapé dans la TUI s'y ajouterait (« abc/model opus ») et ne s'appliquerait pas : on vide la ligne d'abord (Ctrl+U).
	if strings.HasPrefix(text, "/") {
		if _, clearLineErr := tmuxRun("", "send-keys", "-t", pane, "C-u"); clearLineErr != nil {
			return terr("srv.sendkeys.tmuxSendKeys", "W1", clearLineErr)
		}
	}
	bufferName := "gld-send-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatUint(sendSeq.Add(1), 10)
	if _, loadErr := tmuxRun(text, "load-buffer", "-b", bufferName, "-"); loadErr != nil {
		return terr("srv.sendkeys.tmuxLoadBuffer", "W1", loadErr)
	}
	if _, pasteErr := tmuxRun("", "paste-buffer", "-p", "-d", "-b", bufferName, "-t", pane); pasteErr != nil {
		return terr("srv.sendkeys.tmuxPasteBuffer", "W1", pasteErr)
	}
	time.Sleep(200 * time.Millisecond) // laisse la TUI traiter le collage avant la validation
	if _, enterErr := tmuxRun("", "send-keys", "-t", pane, "Enter"); enterErr != nil {
		return terr("srv.sendkeys.tmuxSendKeys", "W1", enterErr)
	}
	return nil
}

// Interaction avec l'écran du terminal d'une session tmux : voir ce qu'elle affiche (une question de permission, un choix…) et
// répondre par des touches. Seules des touches de navigation et de réponse sont permises : jamais de texte libre par cette voie
// (le texte passe par sendToSession).
var allowedKeys = map[string]string{
	"Enter": "Enter", "Escape": "Escape", "Up": "Up", "Down": "Down", "Left": "Left", "Right": "Right",
	"Tab": "Tab", "ShiftTab": "BTab", "Backspace": "BSpace", "Space": "Space", "CtrlC": "C-c",
	"1": "1", "2": "2", "3": "3", "4": "4", "5": "5", "6": "6", "7": "7", "8": "8", "9": "9", "y": "y", "n": "n", "f": "f",
}

func paneFor(sessionID string) (string, error) {
	if !idRe.MatchString(sessionID) {
		return "", terr("srv.chat.sessionInvalide")
	}
	panesBySession, err := sendable()
	if err != nil {
		return "", terr("srv.sendkeys.tmuxInutilisable", "W1", err)
	}
	pane, found := panesBySession[sessionID]
	if !found {
		return "", terr("srv.sendkeys.cetteSessionNEst2")
	}
	return pane, nil
}

// sessionScreen : le texte du panneau de la session (écran visible et 200 lignes d'historique, sans couleurs), espaces de fin retirés.
// L'historique sert à retrouver le message de Claude qui précède une question trop longue pour l'écran.
func sessionScreen(sessionID string) (string, error) {
	pane, err := paneFor(sessionID)
	if err != nil {
		return "", err
	}
	return capturePane(pane)
}

func capturePane(pane string) (string, error) {
	capture, err := tmuxRun("", "capture-pane", "-p", "-S", "-200", "-t", pane)
	if err != nil {
		return "", err
	}
	lines := strings.Split(capture, "\n")
	for lineIndex := range lines {
		lines[lineIndex] = strings.TrimRight(lines[lineIndex], " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n"), nil
}

// sendKey envoie une touche de la liste permise au panneau de la session.
func sendKey(sessionID, key string) error {
	tmuxKey, isAllowed := allowedKeys[key]
	if !isAllowed {
		return terr("srv.sendkeys.toucheNonPermise")
	}
	pane, err := paneFor(sessionID)
	if err != nil {
		return err
	}
	_, err = tmuxRun("", "send-keys", "-t", pane, tmuxKey)
	return err
}

var btwFoot = regexp.MustCompile(`to scroll · c to copy · f to fork · Esc to close`)

// btwOpen : l'écran montre la surcouche d'une question /btw (pied d'aide dans les dernières lignes).
func btwOpen(screen string) bool {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	return btwFoot.MatchString(strings.Join(lines, "\n"))
}
