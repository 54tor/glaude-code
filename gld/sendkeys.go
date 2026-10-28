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
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", args...)
	cmd.WaitDelay = time.Second
	applyRunAs(cmd)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil && strings.TrimSpace(errb.String()) != "" {
		err = errors.New(strings.TrimSpace(errb.String()))
	}
	return out.String(), err
}

// sessionPIDs : pid des sessions interactives ouvertes, par id de session (remplaçable dans les tests).
var sessionPIDs = func() map[string]int {
	out, err := claudeAgents()
	var a []struct {
		SessionId, Kind string
		Pid             int
	}
	m := map[string]int{}
	if err != nil || json.Unmarshal(out, &a) != nil {
		return m
	}
	for _, x := range a {
		if x.SessionId != "" && x.Pid > 0 && x.Kind == "interactive" {
			m[x.SessionId] = x.Pid
		}
	}
	return m
}

// backgroundSessions : id court (celui de `claude stop`) des tâches de fond, par id de session (remplaçable dans les tests).
var backgroundSessions = func() map[string]string {
	out, err := claudeAgents()
	var a []struct{ SessionId, Id, Kind string }
	m := map[string]string{}
	if err != nil || json.Unmarshal(out, &a) != nil {
		return m
	}
	for _, x := range a {
		if x.SessionId != "" && x.Id != "" && x.Kind == "background" {
			m[x.SessionId] = x.Id
		}
	}
	return m
}

// stopBackground arrête une tâche de fond (`claude stop`) : sa conversation est conservée (remplaçable dans les tests).
var stopBackground = func(short string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	c := withPath(exec.CommandContext(ctx, "claude", "stop", short))
	c.WaitDelay = time.Second
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(cut(string(out), 300)))
	}
	return nil
}

// parentOf : le processus parent (lecture de /proc/<pid>/stat, ou `ps` sous macOS qui n'a pas /proc), 0 si inconnu (remplaçable dans les tests).
var parentOf = func(pid int) int {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		if runtime.GOOS == "darwin" {
			out, _ := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
			n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
			return n
		}
		return 0
	}
	s := string(b)
	i := strings.LastIndex(s, ")") // le nom du processus peut contenir des espaces et des parenthèses
	if i < 0 {
		return 0
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(f[1])
	return n
}

// tmuxPanes : panneau tmux (« %12 ») par pid du processus racine du panneau.
func tmuxPanes() (map[int]string, error) {
	out, err := tmuxRun("", "list-panes", "-a", "-F", "#{pane_id} #{pane_pid}")
	if err != nil {
		if strings.Contains(err.Error(), "error connecting") || strings.Contains(err.Error(), "no server") {
			err = terr("srv.sendkeys.aucuneSessionNA")
		}
		return nil, err
	}
	m := map[int]string{}
	for _, l := range strings.Split(out, "\n") {
		id, pid, ok := strings.Cut(strings.TrimSpace(l), " ")
		if n, e := strconv.Atoi(pid); ok && e == nil && paneRe.MatchString(id) {
			m[n] = id
		}
	}
	return m, nil
}

// paneOf : le panneau qui porte le processus pid (lui-même ou un de ses ancêtres).
func paneOf(panes map[int]string, pid int) string {
	for i := 0; i < 64 && pid > 1; i++ {
		if p, ok := panes[pid]; ok {
			return p
		}
		pid = parentOf(pid)
	}
	return ""
}

// sendable : les sessions ouvertes auxquelles on peut écrire, avec leur panneau tmux ; l'erreur dit pourquoi tmux est inutilisable
// (absent, sans serveur).
func sendable() (map[string]string, error) {
	out := map[string]string{}
	pids := sessionPIDs()
	if len(pids) == 0 {
		return out, nil
	}
	panes, err := tmuxPanes()
	if err != nil {
		return out, err
	}
	for id, pid := range pids {
		if p := paneOf(panes, pid); p != "" {
			out[id] = p
		}
	}
	return out, nil
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
	for _, r := range text {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f || r >= 0x80 && r < 0xa0 {
			return terr("srv.sendkeys.leMessageContientUn")
		}
	}
	return nil
}

// closeSession termine le processus claude d'une session interactive ouverte (SIGTERM : il quitte proprement, le transcript est déjà
// écrit au fil de l'eau). Le pid vient d'un relevé frais de `claude agents --json`, jamais du navigateur.
func closeSession(id string) error {
	if !idRe.MatchString(id) {
		return terr("srv.chat.sessionInvalide")
	}
	pid, ok := sessionPIDs()[id]
	if !ok {
		if short, bg := backgroundSessions()[id]; bg {
			err := stopBackground(short)
			forgetAgents()
			if err != nil {
				return terr("srv.sendkeys.fermetureImpossible", "W1", err)
			}
			return nil
		}
		return terr("srv.sendkeys.sessionIntrouvableParmiLes")
	}
	if err := runAsOwns(pid); err != nil {
		return terr("srv.sendkeys.fermetureImpossible", "W1", err)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return terr("srv.sendkeys.fermetureImpossible", "W1", err)
	}
	return nil
}

// closeAndWait ferme celles de ces sessions qui sont ouvertes, puis attend (6 s au plus) que leur processus ait fini : tant
// qu'il tourne, purgeSessions épargnerait la session.
func closeAndWait(items []item) {
	open := map[string]bool{}
	for _, it := range items {
		_, isBg := backgroundSessions()[it.ID]
		if _, ok := sessionPIDs()[it.ID]; (ok || isBg) && closeSession(it.ID) == nil {
			open[it.ID] = true
		}
	}
	for i := 0; i < 30 && len(open) > 0; i++ {
		time.Sleep(200 * time.Millisecond)
		live := liveSessions()
		for id := range open {
			if !live[id] {
				delete(open, id)
			}
		}
	}
}

// sendToSession colle le texte dans le panneau tmux de la session, puis valide.
// sendSeq : un tampon tmux par envoi, sinon deux envois simultanés (warmTick et un utilisateur) se collent mutuellement leur texte.
var sendSeq atomic.Uint64

func sendToSession(id, text string) error {
	if !idRe.MatchString(id) {
		return terr("srv.chat.sessionInvalide")
	}
	if err := checkSendText(text); err != nil {
		return err
	}
	ids, err := sendable()
	if err != nil {
		return terr("srv.sendkeys.tmuxInutilisableLanceLes", "W1", err)
	}
	pane, ok := ids[id]
	if !ok {
		return terr("srv.sendkeys.cetteSessionNEst")
	}
	return sendToPane(pane, text)
}

// sendToPane colle le texte dans le panneau puis valide (le texte est déjà contrôlé par l'appelant).
func sendToPane(pane, text string) error {
	// ponytail: fenêtre résiduelle de 200 ms entre le collage et l'Entrée, pendant laquelle un dialogue pourrait apparaître ; pas de parade côté tmux
	// Un texte qui finit sur « @chemin » ouvre le menu de fichiers de Claude Code, qui avale Entrée : l'espace final le ferme.
	if i := strings.LastIndexAny(text, " \n"); strings.HasPrefix(text[i+1:], "@") {
		text += " "
	}
	// Une commande « /… » collée derrière un texte déjà tapé dans la TUI s'y ajouterait (« abc/model opus ») et ne s'appliquerait pas : on vide la ligne d'abord (Ctrl+U).
	if strings.HasPrefix(text, "/") {
		if _, err := tmuxRun("", "send-keys", "-t", pane, "C-u"); err != nil {
			return terr("srv.sendkeys.tmuxSendKeys", "W1", err)
		}
	}
	buf := "gld-send-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatUint(sendSeq.Add(1), 10)
	if _, err := tmuxRun(text, "load-buffer", "-b", buf, "-"); err != nil {
		return terr("srv.sendkeys.tmuxLoadBuffer", "W1", err)
	}
	if _, err := tmuxRun("", "paste-buffer", "-p", "-d", "-b", buf, "-t", pane); err != nil {
		return terr("srv.sendkeys.tmuxPasteBuffer", "W1", err)
	}
	time.Sleep(200 * time.Millisecond) // laisse la TUI traiter le collage avant la validation
	if _, err := tmuxRun("", "send-keys", "-t", pane, "Enter"); err != nil {
		return terr("srv.sendkeys.tmuxSendKeys", "W1", err)
	}
	return nil
}

// Interaction avec l'écran du terminal d'une session tmux : voir ce qu'elle affiche (une question de permission, un choix…) et
// répondre par des touches. Seules des touches de navigation et de réponse sont permises : jamais de texte libre par cette voie
// (le texte passe par sendToSession).
var allowedKeys = map[string]string{
	"Enter": "Enter", "Escape": "Escape", "Up": "Up", "Down": "Down", "Left": "Left", "Right": "Right",
	"Tab": "Tab", "ShiftTab": "BTab", "Backspace": "BSpace", "Space": "Space", "CtrlC": "C-c",
	"1": "1", "2": "2", "3": "3", "4": "4", "5": "5", "6": "6", "7": "7", "8": "8", "9": "9", "y": "y", "n": "n",
}

func paneFor(id string) (string, error) {
	if !idRe.MatchString(id) {
		return "", terr("srv.chat.sessionInvalide")
	}
	ids, err := sendable()
	if err != nil {
		return "", terr("srv.sendkeys.tmuxInutilisable", "W1", err)
	}
	p, ok := ids[id]
	if !ok {
		return "", terr("srv.sendkeys.cetteSessionNEst2")
	}
	return p, nil
}

// sessionScreen : le texte du panneau de la session (écran visible et 200 lignes d'historique, sans couleurs), espaces de fin retirés.
// L'historique sert à retrouver le message de Claude qui précède une question trop longue pour l'écran.
func sessionScreen(id string) (string, error) {
	p, err := paneFor(id)
	if err != nil {
		return "", err
	}
	return capturePane(p)
}

func capturePane(p string) (string, error) {
	out, err := tmuxRun("", "capture-pane", "-p", "-S", "-200", "-t", p)
	if err != nil {
		return "", err
	}
	lines := strings.Split(out, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n"), nil
}

// sendKey envoie une touche de la liste permise au panneau de la session.
func sendKey(id, key string) error {
	k, ok := allowedKeys[key]
	if !ok {
		return terr("srv.sendkeys.toucheNonPermise")
	}
	p, err := paneFor(id)
	if err != nil {
		return err
	}
	_, err = tmuxRun("", "send-keys", "-t", p, k)
	return err
}
