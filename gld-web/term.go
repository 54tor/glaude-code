package main

// Ouverture d'un onglet de terminal qui lance claude. Aucun shell n'est jamais construit pour transporter un
// prompt ou un chemin : ils passent par un fichier JSON temporaire que « gld-web _run » relit dans l'onglet.
// Ainsi wt.exe (qui coupe ses arguments sur « ; »), cmd.exe et l'interopérabilité WSL n'y touchent pas.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// tabSpec : ce que l'onglet doit lancer. Args sont les arguments de claude, prompt compris.
type tabSpec struct {
	Cwd  string   `json:"cwd"`
	Args []string `json:"args"`
}

// launchers : ordre de préférence, le multiplexeur courant d'abord, puis les émulateurs. « wt » est Windows Terminal
// (natif ou depuis WSL), « console » la fenêtre de repli de Windows.
var launchers = []string{"tmux", "kitty", "wezterm", "wt", "terminator", "gnome-terminal", "konsole", "xfce4-terminal", "tilix", "console"}

func onPath(name string) bool { _, err := exec.LookPath(name); return err == nil }

func launcherAvailable(name string) bool {
	switch name {
	case "tmux":
		return os.Getenv("TMUX") != "" && onPath("tmux")
	case "kitty":
		return os.Getenv("KITTY_LISTEN_ON") != "" && onPath("kitty")
	case "wezterm":
		return os.Getenv("WEZTERM_PANE") != "" && onPath("wezterm")
	case "wt":
		return (os.Getenv("WSL_DISTRO_NAME") != "" || runtime.GOOS == "windows") && onPath("wt.exe")
	case "console":
		return runtime.GOOS == "windows"
	}
	return onPath(name)
}

// detectLauncher : le premier lanceur utilisable ici, "" s'il n'y en a aucun.
func detectLauncher() string {
	for _, l := range launchers {
		if launcherAvailable(l) {
			return l
		}
	}
	return ""
}

// shq : échappe pour une commande shell entre quotes simples (tmux, terminator, xfce4-terminal et tilix reçoivent
// une chaîne et non une liste d'arguments).
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func shJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shq(a)
	}
	return strings.Join(q, " ")
}

// launcherCmd : la commande qui ouvre un onglet intitulé title exécutant argv.
func launcherCmd(name, title string, argv []string) (*exec.Cmd, error) {
	with := func(prefix ...string) *exec.Cmd {
		return exec.Command(prefix[0], append(prefix[1:], argv...)...)
	}
	switch name {
	case "tmux":
		// -n passe par l'expansion de formats de tmux : « #(commande) » d'un titre s'exécuterait. « ## » est un « # » littéral.
		return exec.Command("tmux", "new-window", "-n", strings.ReplaceAll(title, "#", "##"), shJoin(argv)), nil
	case "kitty":
		return with("kitty", "@", "launch", "--type=tab", "--tab-title", title), nil
	case "wezterm":
		return with("wezterm", "cli", "spawn", "--"), nil
	case "wt":
		t := safeTitle(title)
		if d := os.Getenv("WSL_DISTRO_NAME"); d != "" && runtime.GOOS != "windows" {
			return with("wt.exe", "-w", "0", "new-tab", "--title", t, "wsl.exe", "-d", d, "--"), nil
		}
		return with("wt.exe", "-w", "0", "new-tab", "--title", t), nil
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
		c := exec.Command(argv[0], argv[1:]...)
		consoleAttr(c)
		return c, nil
	}
	return nil, errors.New("lanceur inconnu : " + name)
}

// openTab ouvre un onglet qui exécute spec (remplaçable dans les tests).
var openTab = func(launcher, title string, spec tabSpec) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.Contains(self, ";") {
		return errors.New("« ; » interdit dans le chemin de gld-web")
	}
	f, err := os.CreateTemp("", "gld-*.json")
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(spec); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	f.Close()
	c, err := launcherCmd(launcher, title, []string{self, "_run", f.Name()})
	if err == nil {
		err = c.Start()
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	go c.Wait() // pas de zombie dans le serveur
	return nil
}

// runTab : le corps de « gld-web _run <fichier> », exécuté dans l'onglet. Il se place dans le dossier du projet et
// lance claude à la place de ce processus (Unix) ou comme enfant (Windows).
func runTab(file string) int {
	b, err := os.ReadFile(file)
	os.Remove(file)
	var s tabSpec
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err == nil {
		err = os.Chdir(s.Cwd)
	}
	if err == nil {
		var path string
		if path, err = exec.LookPath("claude"); err == nil {
			c := withPath(exec.Command(path, s.Args...))
			return execClaude(c)
		}
	}
	// L'onglet se refermerait aussitôt : on laisse lire l'erreur.
	fmt.Fprintln(os.Stderr, "gld-web :", err)
	fmt.Fprint(os.Stderr, "[Entrée pour fermer] ")
	bufio.NewReader(os.Stdin).ReadString('\n')
	return 1
}

// claudeTabArgs : les arguments de claude pour reprendre une session (id) ou en ouvrir une nouvelle avec un prompt.
func claudeTabArgs(extra []string, id, prompt string) []string {
	a := append([]string{}, extra...)
	if id != "" {
		a = append(a, "--resume", id)
	}
	if prompt != "" {
		a = append(a, "--", prompt)
	}
	return a
}
