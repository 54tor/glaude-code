package main

// gld-matin : rouvre chaque sujet Claude d'un projet dans un onglet de terminal.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func matinHelp() string { return L("cli.matin.help") }

type matinOpts struct {
	dry, nommees                bool
	id, project, prompt, launch string
	args                        []string
	age                         int
	min                         int64
	pause                       time.Duration
	exclure                     string
	env                         map[string]string
}

func cliMatin(ctx context.Context, args []string, out io.Writer) error {
	resume, resumeName := false, ""
	var rest []string
	var env map[string]string
	for i := 0; i < len(args); i++ { // --resume prend un nom facultatif
		if args[i] == "--env" { // seul le couple de tabEnvKey est admis ; jamais par --args
			if i+1 >= len(args) || args[i+1] != tabEnvKey+"="+tabEnvVal {
				return terr("srv.matin.envRefuse")
			}
			i++
			env = map[string]string{tabEnvKey: tabEnvVal}
			continue
		}
		if args[i] == "--resume" {
			resume = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				resumeName = args[i]
			}
			continue
		}
		rest = append(rest, args[i])
	}
	f, err := parseFlags(rest, []string{"--dry-run", "--nommees"}, []string{"--id", "--args", "--prompt", "--term", "--age", "--min"}, map[string]string{"-n": "--dry-run"})
	if err != nil {
		if err == errHelp {
			return helpText(out, matinHelp())
		}
		return err
	}
	pause := 0.7
	if v, err := strconv.ParseFloat(envStr("", "GLD_MATIN_PAUSE", "CC_MATIN_PAUSE"), 64); err == nil && v >= 0 {
		pause = v
	}
	o := matinOpts{
		dry: f.bools["--dry-run"], nommees: f.bools["--nommees"], id: f.vals["--id"], prompt: f.vals["--prompt"],
		launch:  envStr("", "GLD_MATIN_TERM", "CC_MATIN_TERM"),
		age:     envInt(7, "GLD_MATIN_AGE", "CC_MATIN_AGE"),
		min:     int64(envInt(50000, "GLD_MATIN_MIN", "CC_MATIN_MIN")),
		pause:   time.Duration(pause * float64(time.Second)),
		exclure: envStr("", "GLD_MATIN_EXCLURE", "CC_MATIN_EXCLURE"),
		args:    strings.Fields(envStr("", "GLD_MATIN_ARGS", "CC_MATIN_ARGS")),
		env:     env,
	}
	if len(f.pos) > 0 {
		o.project = f.pos[len(f.pos)-1]
	}
	if v, ok := f.vals["--term"]; ok {
		o.launch = v
	}
	if v, ok := f.vals["--args"]; ok {
		o.args = strings.Fields(v)
	}
	o.age, o.min = f.int("--age", o.age), int64(f.int("--min", int(o.min)))

	if resume {
		return matinResume(ctx, o, resumeName, out)
	}
	return matinRun(ctx, o, out)
}

// matinResume rouvre les sessions d'une sauvegarde de gld : chacune par un appel avec --id, donc mêmes règles
// (sessions déjà ouvertes épargnées, -n respecté).
func matinResume(ctx context.Context, o matinOpts, name string, out io.Writer) error {
	saves, _ := loadTabs()
	if len(saves) == 0 {
		if b, err := os.ReadFile(filepath.Join(cfgDir(), "cc-web-tabs.json")); err == nil { // ancien nom, avant la première migration
			saves, _ = readTabs(b)
		}
	}
	if len(saves) == 0 {
		return terr("srv.matin.aucuneSauvegarde", "A1", tabsFile())
	}
	var save *TabSave
	for i := range saves {
		if name == "" || saves[i].Name == name {
			save = &saves[i]
			break
		}
	}
	if save == nil {
		return terr("srv.matin.sauvegardeIntrouvable", "A1", name)
	}
	label := L("cli.matin.automatic")
	if save.Name != "" {
		label = save.Name
	}
	fmt.Fprintln(out, L("cli.matin.backup", "A1", label, "A2", strings.Replace(cut(save.At, 16), "T", " ", 1), "A3", len(save.Sessions)))
	for _, s := range save.Sessions {
		fmt.Fprintln(out)
		one := o
		one.id, one.project, one.prompt = s.ID, s.Project, ""
		if err := matinRun(ctx, one, out); err != nil {
			fmt.Fprintln(out, "gld-matin :", err)
		}
	}
	return nil
}

func matinRun(ctx context.Context, o matinOpts, out io.Writer) error {
	if o.launch == "list" {
		fmt.Fprintln(out, L("cli.matin.launchers"))
		for _, l := range launchers {
			if launcherAvailable(l) {
				fmt.Fprintln(out, "  "+l)
			}
		}
		return nil
	}
	launcher := o.launch
	if launcher == "" {
		launcher = detectLauncher()
	}

	// Nouvelle session avec un prompt (ex. fusion de mémoires depuis gld).
	if o.prompt != "" && o.id == "" {
		dir, err := absDir(o.project)
		if err != nil {
			return err
		}
		if o.dry {
			fmt.Fprintln(out, L("cli.matin.wouldOpenNew", "A1", dir))
			return nil
		}
		if launcher == "" {
			return terr("srv.matin.aucunTerminalSupporteDetecte")
		}
		if err := openTab(launcher, "claude", tabSpec{Cwd: dir, Args: claudeTabArgs(o.args, "", o.prompt), Env: o.env}); err != nil {
			return err
		}
		fmt.Fprintln(out, L("cli.matin.openedNew", "A1", dir))
		return nil
	}

	if o.id != "" {
		o.age, o.min = 36500, 0
	}
	p, err := resolveProject(o.project)
	if err != nil {
		return err
	}
	if launcher == "" && !o.dry {
		return terr("srv.matin.aucunTerminalSupporteDetecte2")
	}
	fmt.Fprintln(out, L("cli.matin.project", "A1", p.Path))
	if !o.dry {
		fmt.Fprintln(out, L("cli.matin.tabs", "A1", launcher))
	}
	fmt.Fprintln(out)

	live := liveSessions() // inconnu (nil) : on ouvre quand même, comme avant
	excl := map[string]bool{}
	for _, e := range strings.Split(o.exclure, ":") {
		if e != "" {
			excl[e] = true
		}
	}
	files := listTranscripts(p.Transcripts, false, false)
	recent := files[:0:0]
	for _, t := range files {
		if younger(t.Info, o.age) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		fmt.Fprintln(out, L("cli.matin.noRecent", "A1", o.age))
		return nil
	}

	seen := map[string]bool{}
	n := 0
	opened := false // la session demandée est déjà ouverte dans un terminal
	for _, t := range recent {
		if o.id != "" && t.S.ID != o.id || t.Info.Size() < o.min || o.id == "" && t.S.autoReview() {
			continue
		}
		title, mark := t.label()
		if o.id != "" && title == "" {
			title = short(t.S.ID) // une session demandée par son id s'ouvre même sans titre
		}
		if mark == "~" && o.nommees && o.id == "" || title == "" {
			continue
		}
		if o.id == "" { // premier vu = plus récent : les homonymes plus anciens sont ignorés
			if seen[title] {
				continue
			}
			seen[title] = true
		}
		if excl[title] {
			continue
		}
		if live[t.S.ID] {
			fmt.Fprintf(out, "  %s %s %s %s\n", pad(L("cli.matin.stateOpen"), 12), mark, pad(cut(title, 45), 45), short(t.S.ID))
			opened = true
			continue
		}
		if o.dry {
			fmt.Fprintf(out, "  %s %s %s %s  (%s)\n", pad(L("cli.matin.stateWould"), 12), mark, pad(cut(title, 45), 45), short(t.S.ID), mdate(t.Info))
		} else {
			fmt.Fprintf(out, "  %s %s %s %s\n", pad(L("cli.matin.stateOpening"), 12), mark, pad(cut(title, 45), 45), short(t.S.ID))
			if err := openTab(launcher, title, tabSpec{Cwd: p.Path, Args: claudeTabArgs(o.args, t.S.ID, o.prompt)}); err != nil {
				fmt.Fprintln(out, "   ", L("srv.common.failed", "A1", err.Error()))
			}
			select {
			case <-time.After(o.pause):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		n++
	}
	fmt.Fprintln(out)
	if o.id != "" && n == 0 { // une session précise : ne rien ouvrir est un échec à dire clairement, pas « 0 onglet »
		if opened {
			return terr("srv.matin.cetteSessionEstDeja")
		}
		return terr("srv.matin.sessionIntrouvableDansCe", "A1", short(o.id))
	}
	if o.dry {
		fmt.Fprintln(out, L("cli.matin.wouldReopen", "A1", n))
	} else {
		fmt.Fprintln(out, L("cli.matin.launched", "A1", n))
	}
	return nil
}

// absDir : un dossier existant, en chemin absolu ; le dossier courant si target est vide.
func absDir(target string) (string, error) {
	if target == "" {
		target, _ = os.Getwd()
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", terr("srv.matin.projetIntrouvable")
	}
	return abs, nil
}
