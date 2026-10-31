package main

// Ouverture d'un onglet de terminal qui lance claude. Aucun shell n'est jamais construit pour transporter un
// prompt ou un chemin : ils passent par un fichier JSON temporaire que « gld _run » relit dans l'onglet.
// Ainsi wt.exe (qui coupe ses arguments sur « ; »), cmd.exe et l'interopérabilité WSL n'y touchent pas.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// tabSpec : ce que l'onglet doit lancer. Args sont les arguments de claude, prompt compris.
type tabSpec struct {
	Cwd  string            `json:"cwd"`
	Args []string          `json:"args"`
	Env  map[string]string `json:"env,omitempty"`
}

// tabEnvAllowed : le seul couple que le fichier de spécification peut poser dans l'environnement de claude. Ce fichier est une
// frontière de confiance (sous GLD_RUN_AS il est relu par le compte cible) : toute autre variable, LD_PRELOAD ou PATH
// compris, est ignorée.
const tabEnvKey, tabEnvVal = "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS", "1"

// applyTabEnv ajoute à command.Env les variables autorisées de requestedEnv ; à appeler après withPath, qui écrase command.Env.
func applyTabEnv(command *exec.Cmd, requestedEnv map[string]string) {
	if requestedEnv[tabEnvKey] == tabEnvVal {
		command.Env = append(slices.DeleteFunc(command.Env, func(entry string) bool { return strings.HasPrefix(entry, tabEnvKey+"=") }), tabEnvKey+"="+tabEnvVal)
	}
}

// launchers : ordre de préférence, le multiplexeur courant d'abord, puis les émulateurs. « wt » est Windows Terminal
// (natif ou depuis WSL), « console » la fenêtre de repli de Windows.
var launchers = []string{"tmux", "kitty", "wezterm", "wt", "terminator-tmux", "terminator", "gnome-terminal", "konsole", "xfce4-terminal", "tilix", "console"}

func onPath(name string) bool { _, err := exec.LookPath(name); return err == nil }

func launcherAvailable(name string) bool {
	if tmuxDetached() { // aucun bureau sous le compte cible : seul son serveur tmux reçoit les sessions
		return name == "tmux" && onPath("tmux")
	}
	switch name {
	case "tmux":
		return os.Getenv("TMUX") != "" && onPath("tmux")
	case "kitty":
		return os.Getenv("KITTY_LISTEN_ON") != "" && onPath("kitty")
	case "wezterm":
		return os.Getenv("WEZTERM_PANE") != "" && onPath("wezterm")
	case "terminator-tmux": // un onglet Terminator qui lance la session dans tmux : joignable par « Envoyer à la session »
		return onPath("terminator") && onPath("tmux")
	case "wt":
		return (os.Getenv("WSL_DISTRO_NAME") != "" || runtime.GOOS == "windows") && onPath("wt.exe")
	case "console":
		return runtime.GOOS == "windows"
	}
	return onPath(name)
}

// detectLauncher : le premier lanceur utilisable ici, "" s'il n'y en a aucun.
func detectLauncher() string {
	for _, launcher := range launchers {
		if launcherAvailable(launcher) {
			return launcher
		}
	}
	return ""
}

// shq : échappe pour une commande shell entre quotes simples (tmux, terminator, xfce4-terminal et tilix reçoivent
// une chaîne et non une liste d'arguments).
func shq(text string) string { return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'" }

func shJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for argIndex, argument := range argv {
		quoted[argIndex] = shq(argument)
	}
	return strings.Join(quoted, " ")
}

// launcherCmd : la commande qui ouvre un onglet intitulé title exécutant argv.
func launcherCmd(name, title string, argv []string) (*exec.Cmd, error) {
	// « <émulateur>+tmux » : la session tourne dans tmux au sein de l'onglet, donc joignable par « Envoyer à la session ».
	if base, hasTmuxSuffix := strings.CutSuffix(name, "+tmux"); hasTmuxSuffix {
		return launcherCmd(base, title, []string{"tmux", "new-session", "-n", strings.ReplaceAll(title, "#", "##"), shJoin(argv),
			";", "set-option", "set-titles", "on", ";", "set-option", "set-titles-string", "#T"})
	}
	with := func(prefix ...string) *exec.Cmd {
		return exec.Command(prefix[0], append(prefix[1:], argv...)...)
	}
	switch name {
	case "tmux":
		if tmuxDetached() {
			return exec.Command("tmux", "new-session", "-d", "-x", "160", "-y", "48", "-n", strings.ReplaceAll(title, "#", "##"), shJoin(argv)), nil
		}
		// -n passe par l'expansion de formats de tmux : « #(commande) » d'un titre s'exécuterait. « ## » est un « # » littéral.
		return exec.Command("tmux", "new-window", "-n", strings.ReplaceAll(title, "#", "##"), shJoin(argv)), nil
	case "kitty":
		return with("kitty", "@", "launch", "--type=tab", "--tab-title", title), nil
	case "wezterm":
		return with("wezterm", "cli", "spawn", "--"), nil
	case "wt":
		windowsTitle := safeTitle(title)
		if distro := os.Getenv("WSL_DISTRO_NAME"); distro != "" && runtime.GOOS != "windows" {
			return with("wt.exe", "-w", "0", "new-tab", "--title", windowsTitle, "wsl.exe", "-d", distro, "--"), nil
		}
		return with("wt.exe", "-w", "0", "new-tab", "--title", windowsTitle), nil
	case "terminator-tmux":
		return exec.Command("terminator", "--new-tab", "-T", title, "-e", "tmux new-session -n "+shq(strings.ReplaceAll(title, "#", "##"))+" "+shq(shJoin(argv))+
			// le titre que Claude Code émet (OSC) devient celui de l'onglet Terminator
			` \; set-option set-titles on \; set-option set-titles-string '#T'`), nil
	case "terminator":
		return exec.Command("terminator", "--new-tab", "-T", title, "-e", shJoin(argv)), nil
	case "gnome-terminal":
		return with("gnome-terminal", "--tab", "--title="+title, "--"), nil
	case "konsole":
		return with("konsole", "--new-tab", "-p", "tabtitle="+title, "-e"), nil
	case "xfce4-terminal":
		return exec.Command("xfce4-terminal", "--tab", "-T", title, "-e", shJoin(argv)), nil
	case "tilix":
		return exec.Command("tilix", "--action=app-new-session", "-e", shJoin(argv)), nil
	case "console":
		command := exec.Command(argv[0], argv[1:]...)
		consoleAttr(command)
		return command, nil
	}
	return nil, terr("srv.term.lanceurInconnu", "A1", name)
}

// openTab ouvre un onglet qui exécute spec (remplaçable dans les tests).
var openTab = func(launcher, title string, spec tabSpec) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.Contains(self, ";") {
		return terr("srv.term.interditDansLeChemin")
	}
	spec.Args = withGuard(spec.Args)
	specFile, err := writeSpec(self, spec)
	if err != nil {
		return err
	}
	command, err := launcherCmd(launcher, title, []string{self, "_run", specFile})
	if err == nil {
		applyRunAs(command)
		err = command.Start()
	}
	if err != nil {
		if specPath(specFile) {
			os.Remove(specFile)
		}
		return err
	}
	go command.Wait() // pas de zombie dans le serveur
	return nil
}

// writeSpec : le fichier JSON que relit « gld _run » ; sous GLD_RUN_AS, créé par le compte cible qui seul doit le lire.
func writeSpec(self string, spec tabSpec) (string, error) {
	content, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	if runAsOn() {
		return writeSpecAsTarget(self, content)
	}
	specFile, err := os.CreateTemp("", "gld-*.json")
	if err != nil {
		return "", err
	}
	_, err = specFile.Write(append(content, '\n'))
	if closeErr := specFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(specFile.Name())
		return "", err
	}
	return specFile.Name(), nil
}

// specPath : name est bien un fichier gld-*.json posé directement dans le dossier temporaire ; sous GLD_RUN_AS, il vient de la
// sortie d'un processus du compte cible, que la passerelle ne doit pas laisser lui désigner un fichier à effacer.
func specPath(name string) bool {
	matches, _ := filepath.Match("gld-*.json", filepath.Base(name))
	return matches && name == filepath.Clean(name) && filepath.Dir(name) == filepath.Clean(os.TempDir())
}

// runTab : le corps de « gld _run <fichier> », exécuté dans l'onglet. Il se place dans le dossier du projet et
// lance claude à la place de ce processus (Unix) ou comme enfant (Windows).
func runTab(file string) int {
	content, err := os.ReadFile(file)
	os.Remove(file)
	var spec tabSpec
	if err == nil {
		err = json.Unmarshal(content, &spec)
	}
	if err == nil {
		err = os.Chdir(spec.Cwd)
	}
	if err == nil {
		// withPath cherche aussi dans ~/.local/bin, absent du PATH d'un onglet ouvert depuis un lanceur de bureau ou un service.
		if command := withPath(exec.Command("claude", spec.Args...)); command.Err == nil {
			applyTabEnv(command, spec.Env)
			return execClaude(command)
		} else {
			err = command.Err
		}
	}
	// L'onglet se refermerait aussitôt : on laisse lire l'erreur.
	fmt.Fprintln(os.Stderr, "gld :", err)
	fmt.Fprint(os.Stderr, L("srv.term.pressEnter")+" ")
	bufio.NewReader(os.Stdin).ReadString('\n')
	return 1
}

// claudeTabArgs : les arguments de claude pour reprendre une session (id) ou en ouvrir une nouvelle avec un prompt.
func claudeTabArgs(extra []string, id, prompt string) []string {
	args := append(builtinPromptArgs(), extra...)
	if id != "" {
		args = append(args, "--resume", id)
	}
	if prompt != "" {
		args = append(args, "--", prompt)
	}
	return args
}
