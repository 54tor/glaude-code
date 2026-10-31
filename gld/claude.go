package main

// Appels à claude et gld-matin : options acceptées, sessions ouvertes ou en attente.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	resumePromptMaxBytes = 32 << 10
	helperErrorLineMax   = 300
)

// checkPrompt : un message vise une seule session, fermée (sinon deux processus écriraient le même transcript).
func checkPrompt(id, prompt string) error {
	isUnacceptable := len(prompt) > resumePromptMaxBytes || !utf8.ValidString(prompt) || strings.ContainsRune(prompt, 0)
	switch {
	case prompt == "":
		return nil
	case id == "":
		return terr("srv.claude.unMessageNeS")
	case isUnacceptable:
		return terr("srv.sendkeys.messageTropLongOu")
	}
	live, err := liveCheck()
	if err != nil {
		return err
	}
	if live[id] {
		return errOpen
	}
	return nil
}

// checkResumePrompt : comme checkPrompt, et jamais quand claude est un script .cmd ou .bat (installation npm sous Windows) :
// le message passerait par cmd.exe, qui ne sait pas l'échapper sûrement. Le claude.exe de l'installateur natif reçoit ses
// arguments tels quels.
func checkResumePrompt(id, prompt string) error {
	if prompt != "" && viaCmdExe(withPath(exec.Command("claude")).Path) {
		return terr("srv.claude.repriseAvecUnMessage")
	}
	return checkPrompt(id, prompt)
}

func viaCmdExe(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".cmd" || ext == ".bat"
}

// claudeArgs valide les options de claude (seulement des drapeaux simples : --x, -x, --x=valeur, jamais du shell)
// et les traduit en arguments de gld-matin.
func claudeArgs(raw string) ([]string, error) {
	flagList := strings.Fields(raw)
	for _, flag := range flagList {
		if !flagRe.MatchString(flag) {
			return nil, terr("srv.chat.optionRefusee", "A1", flag)
		}
	}
	if len(flagList) == 0 {
		return nil, nil
	}
	return []string{"--args", strings.Join(flagList, " ")}, nil
}

var flagRe = regexp.MustCompile(`^--?[a-zA-Z][a-zA-Z0-9-]*(=[A-Za-z0-9._:/-]+)?$`)

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

var (
	workOnce sync.Once
	workPath string
)

// workDir : dossier de travail des appels à claude qui ne doivent voir aucun projet (brouillons, comptes de tokens,
// état MCP). Un dossier neuf et privé (0700), jamais /tmp lui-même : n'importe qui peut y déposer un CLAUDE.md, un
// .mcp.json ou un .claude/settings.json que claude lirait et exécuterait avec vos droits.
func workDir() string {
	workOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gld-web-")
		if err != nil {
			dir = cfgDir() // à défaut, le dossier de configuration de l'utilisateur (privé lui aussi)
		}
		workPath = dir
	})
	return workPath
}

// removeWorkDir : à l'arrêt, retire le dossier de travail s'il a été créé ici.
func removeWorkDir() {
	if workPath != "" && workPath != cfgDir() {
		os.RemoveAll(workPath)
	}
}

// helperCmd : un appel d'aide de gld à claude (brouillon, comptage de tokens, analyse) : sans outils, sans réglages ni MCP,
// dans le dossier privé de workDir, et surtout SANS session enregistrée (--no-session-persistence) : ces appels ne doivent
// jamais apparaître dans la liste de vos sessions. La demande se passe sur stdin.
func helperCmd(callContext context.Context, model, system string) *exec.Cmd {
	cmd := withPath(exec.CommandContext(callContext, "claude", "-p", "--model", model, "--tools", "", "--disable-slash-commands",
		"--strict-mcp-config", "--setting-sources", "", "--no-session-persistence", "--system-prompt", system, "--output-format", "json"))
	cmd.Dir = workDir()
	return cmd
}

// helperSem : au plus 2 claude d'aide à la fois ; une rafale (double-clic, script local) ne lance pas autant de processus
// Node et d'appels facturés que de requêtes. Les suivants attendent leur tour, dans la limite de leur délai.
var helperSem = make(chan struct{}, 2)

func helperOutput(callContext context.Context, cmd *exec.Cmd) ([]byte, error) {
	select {
	case helperSem <- struct{}{}:
	case <-callContext.Done():
		return nil, callContext.Err()
	}
	defer func() { <-helperSem }()
	output, err := cmd.Output()
	// « exit status 1 » seul ne dit rien : on joint la dernière ligne que claude a écrite sur sa sortie d'erreur.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if lastStderrLine := lastLine(exitErr.Stderr); lastStderrLine != "" {
			err = fmt.Errorf("%w : %s", err, cut(lastStderrLine, helperErrorLineMax))
		}
	}
	return output, err
}

func lastLine(output []byte) string {
	trimmed := strings.TrimSpace(string(output))
	if newlineIndex := strings.LastIndexByte(trimmed, '\n'); newlineIndex >= 0 {
		trimmed = strings.TrimSpace(trimmed[newlineIndex+1:])
	}
	return trimmed
}

func withPath(cmd *exec.Cmd) *exec.Cmd {
	home := runAsHome()
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	localBin := filepath.Join(home, ".local", "bin") // aussi celui de l'installateur natif de claude sous Windows
	cmd.Env = append(environNoAuth(), "PATH="+localBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// exec.Command a déjà cherché le programme dans le PATH du serveur, où ~/.local/bin peut manquer
	// (service, lanceur de bureau) : on le reprend ici, avec la même priorité que le PATH ci-dessus.
	name := cmd.Args[0]
	isBareName := !strings.ContainsAny(name, `/\`)
	if isBareName {
		isWindows := runtime.GOOS == "windows"
		if isWindows && filepath.Ext(name) == "" {
			name += ".exe"
		}
		candidate := filepath.Join(localBin, name)
		fileInfo, err := os.Stat(candidate)
		isRunnable := err == nil && fileInfo.Mode().IsRegular() && (isWindows || fileInfo.Mode()&executableBits != 0)
		if isRunnable {
			cmd.Path, cmd.Err = candidate, nil
		}
	}
	applyRunAs(cmd)
	return cmd
}

// matin exécute gld-matin (cli) en direct et renvoie sa sortie ; l'erreur s'y ajoute, pour qu'un échec dise pourquoi
// (remplaçable dans les tests). Appelé depuis le web : la session s'ouvre dans un terminal de la machine, avec tmux
// dedans (le tmux courant de gld, invisible pour l'utilisateur, ne convient pas).
var matin = func(callContext context.Context, args ...string) (string, error) {
	var output strings.Builder
	if launcher := tmuxLauncher(); launcher != "" {
		args = append([]string{"--term", launcher}, args...)
	}
	err := cliMatin(callContext, args, &output)
	if err != nil {
		output.WriteString("gld-matin : " + err.Error() + "\n")
	}
	return output.String(), err
}

// tmuxLauncher : l'onglet d'émulateur où lancer une session avec tmux dedans, "" si tmux ou tout émulateur manque.
func tmuxLauncher() string {
	if launcherAvailable("terminator-tmux") {
		return "terminator-tmux"
	}
	if !onPath("tmux") {
		return ""
	}
	for _, launcher := range []string{"kitty", "wezterm", "gnome-terminal", "konsole", "xfce4-terminal", "tilix"} {
		if launcherAvailable(launcher) {
			return launcher + "+tmux"
		}
	}
	return ""
}

var cmdTimeout = 10 * time.Second // claude agents et tmux : ~130 ms et quelques ms en temps normal

// claudeAgents : la sortie de `claude agents --json`, bornée dans le temps : un claude bloqué (mise à jour, invite) ne fige ni
// /v1/state ni les boucles de fond, et n'empile pas un processus par rafraîchissement. Une erreur vaut « on ne sait pas ».
func claudeAgents() ([]byte, error) {
	agentsMu.Lock()
	defer agentsMu.Unlock()
	if time.Since(agentsAt) < agentsTTL {
		return agentsOut, agentsErr
	}
	agentsOut, agentsErr = runClaudeAgents()
	agentsAt = time.Now()
	return agentsOut, agentsErr
}

// Un relevé vaut agentsTTL, et un seul claude tourne à la fois : /v1/state demandait deux relevés d'affilée, l'écran tmux un toutes
// les 1,5 s, closeAndWait un tous les 200 ms ; chaque relevé est un processus Node de 130 ms et 210 Mo (mesuré).
// forgetAgents : avant une action qui écrit ou supprime (liveCheck), le prochain relevé est frais.
var (
	agentsMu  sync.Mutex
	agentsAt  time.Time
	agentsOut []byte
	agentsErr error
	agentsTTL = 1500 * time.Millisecond
)

func forgetAgents() {
	agentsMu.Lock()
	agentsAt = time.Time{}
	agentsMu.Unlock()
}

func runClaudeAgents() ([]byte, error) {
	commandContext, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	cmd := withPath(exec.CommandContext(commandContext, "claude", "agents", "--json"))
	cmd.WaitDelay = time.Second // un petit-enfant qui garde la sortie ouverte ne bloque pas non plus
	return cmd.Output()
}

// alive : ids des sessions ouvertes, ou nil si `claude agents --json` a échoué (on ne sait pas).
func alive() map[string]bool {
	agentsJSON, err := claudeAgents()
	var agents []struct{ SessionId string }
	if err != nil || json.Unmarshal(agentsJSON, &agents) != nil {
		return nil // inconnu : liveCheck fait refuser les actions qui en dépendent
	}
	openIDs := map[string]bool{}
	for _, agent := range agents {
		openIDs[agent.SessionId] = true
	}
	return openIDs
}

// waiting : ids des sessions ouvertes qui attendent (interactive « idle », en arrière-plan « blocked »).
// Une demande d'autorisation en cours n'est pas distinguée d'un tour terminé.
var waiting = func() []string {
	agentsJSON, err := claudeAgents()
	var agents []struct{ SessionId, Status, State string }
	waitingIDs := []string{}
	if err != nil || json.Unmarshal(agentsJSON, &agents) != nil {
		return waitingIDs
	}
	for _, agent := range agents {
		if agent.Status == "idle" || agent.State == "blocked" {
			waitingIDs = append(waitingIDs, agent.SessionId)
		}
	}
	return waitingIDs
}
