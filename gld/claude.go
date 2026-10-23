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

// checkPrompt : un message vise une seule session, fermée (sinon deux processus écriraient le même transcript).
func checkPrompt(id, prompt string) error {
	switch {
	case prompt == "":
		return nil
	case id == "":
		return terr("srv.claude.unMessageNeS")
	case len(prompt) > 32<<10 || !utf8.ValidString(prompt) || strings.ContainsRune(prompt, 0):
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

// checkResumePrompt : comme checkPrompt, et jamais sous Windows, où le message passerait par cmd.exe
// (qui ne sait pas l'échapper sûrement).
func checkResumePrompt(id, prompt string) error {
	if prompt != "" && runtime.GOOS == "windows" {
		return terr("srv.claude.repriseAvecUnMessage")
	}
	return checkPrompt(id, prompt)
}

// claudeArgs valide les options de claude (seulement des drapeaux simples : --x, -x, --x=valeur, jamais du shell)
// et les traduit en arguments de gld-matin.
func claudeArgs(raw string) ([]string, error) {
	fs := strings.Fields(raw)
	for _, f := range fs {
		if !flagRe.MatchString(f) {
			return nil, terr("srv.chat.optionRefusee", "A1", f)
		}
	}
	if len(fs) == 0 {
		return nil, nil
	}
	return []string{"--args", strings.Join(fs, " ")}, nil
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
		d, err := os.MkdirTemp("", "gld-web-")
		if err != nil {
			d = cfgDir() // à défaut, le dossier de configuration de l'utilisateur (privé lui aussi)
		}
		workPath = d
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
func helperCmd(ctx context.Context, model, system string) *exec.Cmd {
	cmd := withPath(exec.CommandContext(ctx, "claude", "-p", "--model", model, "--tools", "", "--disable-slash-commands",
		"--strict-mcp-config", "--setting-sources", "", "--no-session-persistence", "--system-prompt", system, "--output-format", "json"))
	cmd.Dir = workDir()
	return cmd
}

// helperSem : au plus 2 claude d'aide à la fois ; une rafale (double-clic, script local) ne lance pas autant de processus
// Node et d'appels facturés que de requêtes. Les suivants attendent leur tour, dans la limite de leur délai.
var helperSem = make(chan struct{}, 2)

func helperOutput(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	select {
	case helperSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-helperSem }()
	out, err := cmd.Output()
	// « exit status 1 » seul ne dit rien : on joint la dernière ligne que claude a écrite sur sa sortie d'erreur.
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if l := lastLine(ee.Stderr); l != "" {
			err = fmt.Errorf("%w : %s", err, cut(l, 300))
		}
	}
	return out, err
}

func lastLine(b []byte) string {
	t := strings.TrimSpace(string(b))
	if i := strings.LastIndexByte(t, '\n'); i >= 0 {
		t = strings.TrimSpace(t[i+1:])
	}
	return t
}

func withPath(c *exec.Cmd) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return c
	}
	home, _ := os.UserHomeDir()
	local := filepath.Join(home, ".local/bin")
	c.Env = append(os.Environ(), "PATH="+local+":"+os.Getenv("PATH"))
	// exec.Command a déjà cherché le programme dans le PATH du serveur, où ~/.local/bin peut manquer
	// (service, lanceur de bureau) : on le reprend ici, avec la même priorité que le PATH ci-dessus.
	if name := c.Args[0]; !strings.ContainsRune(name, '/') {
		if fi, err := os.Stat(filepath.Join(local, name)); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			c.Path, c.Err = filepath.Join(local, name), nil
		}
	}
	return c
}

// matin exécute gld-matin (cli) en direct et renvoie sa sortie ; l'erreur s'y ajoute, pour qu'un échec dise pourquoi
// (remplaçable dans les tests). Appelé depuis le web : la session s'ouvre dans un terminal de la machine, avec tmux
// dedans (le tmux courant de gld, invisible pour l'utilisateur, ne convient pas).
var matin = func(ctx context.Context, args ...string) (string, error) {
	var buf strings.Builder
	if l := tmuxLauncher(); l != "" {
		args = append([]string{"--term", l}, args...)
	}
	err := cliMatin(ctx, args, &buf)
	if err != nil {
		buf.WriteString("gld-matin : " + err.Error() + "\n")
	}
	return buf.String(), err
}

// tmuxLauncher : l'onglet d'émulateur où lancer une session avec tmux dedans, "" si tmux ou tout émulateur manque.
func tmuxLauncher() string {
	if launcherAvailable("terminator-tmux") {
		return "terminator-tmux"
	}
	if !onPath("tmux") {
		return ""
	}
	for _, l := range []string{"kitty", "wezterm", "gnome-terminal", "konsole", "xfce4-terminal", "tilix"} {
		if launcherAvailable(l) {
			return l + "+tmux"
		}
	}
	return ""
}

var cmdTimeout = 10 * time.Second // claude agents et tmux : ~130 ms et quelques ms en temps normal

// claudeAgents : la sortie de `claude agents --json`, bornée dans le temps : un claude bloqué (mise à jour, invite) ne fige ni
// /api ni les boucles de fond, et n'empile pas un processus par rafraîchissement. Une erreur vaut « on ne sait pas ».
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

// Un relevé vaut agentsTTL, et un seul claude tourne à la fois : /api demandait deux relevés d'affilée, l'écran tmux un toutes
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
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	c := withPath(exec.CommandContext(ctx, "claude", "agents", "--json"))
	c.WaitDelay = time.Second // un petit-enfant qui garde la sortie ouverte ne bloque pas non plus
	return c.Output()
}

// alive : ids des sessions ouvertes, ou nil si `claude agents --json` a échoué (on ne sait pas).
func alive() map[string]bool {
	out, err := claudeAgents()
	var a []struct{ SessionId string }
	if err != nil || json.Unmarshal(out, &a) != nil {
		return nil // inconnu : liveCheck fait refuser les actions qui en dépendent
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x.SessionId] = true
	}
	return m
}

// waiting : ids des sessions ouvertes qui attendent (interactive « idle », en arrière-plan « blocked »).
// Une demande d'autorisation en cours n'est pas distinguée d'un tour terminé.
var waiting = func() []string {
	out, err := claudeAgents()
	var a []struct{ SessionId, Status, State string }
	ids := []string{}
	if err != nil || json.Unmarshal(out, &a) != nil {
		return ids
	}
	for _, x := range a {
		if x.Status == "idle" || x.State == "blocked" {
			ids = append(ids, x.SessionId)
		}
	}
	return ids
}
