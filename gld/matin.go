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

const (
	matinDefaultPauseSeconds = 0.7
	matinDefaultAgeDays      = 7
	matinDefaultMinBytes     = 50000
	// matinAnyAgeDays : une session demandée par son id s'ouvre quel que soit son âge.
	matinAnyAgeDays  = 36500
	matinStateWidth  = 12
	matinTitleWidth  = 45
	matinBackupStamp = 16
)

func matinHelp() string {
	return L("cli.matin.help")
}

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

func cliMatin(runContext context.Context, args []string, output io.Writer) error {
	resume, resumeName := false, ""
	var rest []string
	var env map[string]string
	for argIndex := 0; argIndex < len(args); argIndex++ { // --resume prend un nom facultatif
		if args[argIndex] == "--env" { // seul le couple de tabEnvKey est admis ; jamais par --args
			if argIndex+1 >= len(args) || args[argIndex+1] != tabEnvKey+"="+tabEnvVal {
				return terr("srv.matin.envRefuse")
			}
			argIndex++
			env = map[string]string{tabEnvKey: tabEnvVal}
			continue
		}
		if args[argIndex] == "--resume" {
			resume = true
			if argIndex+1 < len(args) && !strings.HasPrefix(args[argIndex+1], "-") {
				argIndex++
				resumeName = args[argIndex]
			}
			continue
		}
		rest = append(rest, args[argIndex])
	}
	parsedFlags, err := parseFlags(rest, []string{"--dry-run", "--nommees"}, []string{"--id", "--args", "--prompt", "--term", "--age", "--min"}, map[string]string{"-n": "--dry-run"})
	if err != nil {
		if err == errHelp {
			return helpText(output, matinHelp())
		}
		return err
	}
	pause := matinDefaultPauseSeconds
	if pauseSeconds, parseErr := strconv.ParseFloat(envStr("", "GLD_MATIN_PAUSE", "CC_MATIN_PAUSE"), 64); parseErr == nil && pauseSeconds >= 0 {
		pause = pauseSeconds
	}
	options := matinOpts{
		dry:     parsedFlags.bools["--dry-run"],
		nommees: parsedFlags.bools["--nommees"],
		id:      parsedFlags.vals["--id"],
		prompt:  parsedFlags.vals["--prompt"],
		launch:  envStr("", "GLD_MATIN_TERM", "CC_MATIN_TERM"),
		age:     envInt(matinDefaultAgeDays, "GLD_MATIN_AGE", "CC_MATIN_AGE"),
		min:     int64(envInt(matinDefaultMinBytes, "GLD_MATIN_MIN", "CC_MATIN_MIN")),
		pause:   time.Duration(pause * float64(time.Second)),
		exclure: envStr("", "GLD_MATIN_EXCLURE", "CC_MATIN_EXCLURE"),
		args:    strings.Fields(envStr("", "GLD_MATIN_ARGS", "CC_MATIN_ARGS")),
		env:     env,
	}
	if len(parsedFlags.pos) > 0 {
		options.project = parsedFlags.pos[len(parsedFlags.pos)-1]
	}
	if term, hasTerm := parsedFlags.vals["--term"]; hasTerm {
		options.launch = term
	}
	if extraArgs, hasArgs := parsedFlags.vals["--args"]; hasArgs {
		options.args = strings.Fields(extraArgs)
	}
	options.age = parsedFlags.int("--age", options.age)
	options.min = int64(parsedFlags.int("--min", int(options.min)))

	if resume {
		return matinResume(runContext, options, resumeName, output)
	}
	return matinRun(runContext, options, output)
}

// matinResume rouvre les sessions d'une sauvegarde de gld : chacune par un appel avec --id, donc mêmes règles
// (sessions déjà ouvertes épargnées, -n respecté).
func matinResume(runContext context.Context, options matinOpts, name string, output io.Writer) error {
	saves, _ := loadTabs()
	if len(saves) == 0 {
		if content, readErr := os.ReadFile(filepath.Join(cfgDir(), "cc-web-tabs.json")); readErr == nil { // ancien nom, avant la première migration
			saves, _ = readTabs(content)
		}
	}
	if len(saves) == 0 {
		return terr("srv.matin.aucuneSauvegarde", "A1", tabsFile())
	}
	var save *TabSave
	for saveIndex := range saves {
		if name == "" || saves[saveIndex].Name == name {
			save = &saves[saveIndex]
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
	fmt.Fprintln(output, L("cli.matin.backup", "A1", label, "A2", strings.Replace(cut(save.At, matinBackupStamp), "T", " ", 1), "A3", len(save.Sessions)))
	for _, session := range save.Sessions {
		fmt.Fprintln(output)
		one := options
		one.id, one.project, one.prompt = session.ID, session.Project, ""
		if session.Busy { // coupée en plein tour : sans message, Claude affiche « Interrupted » et attend
			one.prompt = L("cli.matin.continuePrompt")
		}
		if runErr := matinRun(runContext, one, output); runErr != nil {
			fmt.Fprintln(output, "gld-matin :", runErr)
		}
	}
	return nil
}

func matinRun(runContext context.Context, options matinOpts, output io.Writer) error {
	if options.launch == "list" {
		fmt.Fprintln(output, L("cli.matin.launchers"))
		for _, launcherName := range launchers {
			if launcherAvailable(launcherName) {
				fmt.Fprintln(output, "  "+launcherName)
			}
		}
		return nil
	}
	launcher := options.launch
	if launcher == "" {
		launcher = detectLauncher()
	}

	// Nouvelle session avec un prompt (ex. fusion de mémoires depuis gld).
	if options.prompt != "" && options.id == "" {
		dir, dirErr := absDir(options.project)
		if dirErr != nil {
			return dirErr
		}
		if options.dry {
			fmt.Fprintln(output, L("cli.matin.wouldOpenNew", "A1", dir))
			return nil
		}
		if launcher == "" {
			return terr("srv.matin.aucunTerminalSupporteDetecte")
		}
		if openErr := openTab(launcher, "claude", tabSpec{Cwd: dir, Args: claudeTabArgs(options.args, "", options.prompt), Env: options.env}); openErr != nil {
			return openErr
		}
		fmt.Fprintln(output, L("cli.matin.openedNew", "A1", dir))
		return nil
	}

	if options.id != "" {
		options.age, options.min = matinAnyAgeDays, 0
	}
	resolved, projectErr := resolveProject(options.project)
	if projectErr != nil {
		return projectErr
	}
	if launcher == "" && !options.dry {
		return terr("srv.matin.aucunTerminalSupporteDetecte2")
	}
	fmt.Fprintln(output, L("cli.matin.project", "A1", resolved.Path))
	if !options.dry {
		fmt.Fprintln(output, L("cli.matin.tabs", "A1", launcher))
	}
	fmt.Fprintln(output)

	live := liveSessions() // inconnu (nil) : on ouvre quand même, comme avant
	excludedTitles := map[string]bool{}
	for _, excludedTitle := range strings.Split(options.exclure, ":") {
		if excludedTitle != "" {
			excludedTitles[excludedTitle] = true
		}
	}
	files := listTranscripts(resolved.Transcripts, false, false)
	recent := files[:0:0]
	for _, candidate := range files {
		if younger(candidate.Info, options.age) {
			recent = append(recent, candidate)
		}
	}
	if len(recent) == 0 {
		fmt.Fprintln(output, L("cli.matin.noRecent", "A1", options.age))
		return nil
	}

	seen := map[string]bool{}
	openedCount := 0
	opened := false // la session demandée est déjà ouverte dans un terminal
	for _, candidate := range recent {
		isOtherSession := options.id != "" && candidate.S.ID != options.id
		isTooSmall := candidate.Info.Size() < options.min
		// ni revues automatiques ni sessions d'équipiers : elles appartiennent à leur session parente
		isChildSession := options.id == "" && (candidate.S.autoReview() || candidate.S.team != "")
		if isOtherSession || isTooSmall || isChildSession {
			continue
		}
		title, mark := candidate.label()
		if options.id != "" && title == "" {
			title = short(candidate.S.ID) // une session demandée par son id s'ouvre même sans titre
		}
		skipsUnnamed := mark == "~" && options.nommees && options.id == ""
		if skipsUnnamed || title == "" {
			continue
		}
		if options.id == "" { // premier vu = plus récent : les homonymes plus anciens sont ignorés
			if seen[title] {
				continue
			}
			seen[title] = true
		}
		if excludedTitles[title] {
			continue
		}
		if live[candidate.S.ID] {
			fmt.Fprintf(output, "  %s %s %s %s\n", pad(L("cli.matin.stateOpen"), matinStateWidth), mark, pad(cut(title, matinTitleWidth), matinTitleWidth), short(candidate.S.ID))
			opened = true
			continue
		}
		if options.dry {
			fmt.Fprintf(output, "  %s %s %s %s  (%s)\n", pad(L("cli.matin.stateWould"), matinStateWidth), mark, pad(cut(title, matinTitleWidth), matinTitleWidth), short(candidate.S.ID), mdate(candidate.Info))
		} else {
			fmt.Fprintf(output, "  %s %s %s %s\n", pad(L("cli.matin.stateOpening"), matinStateWidth), mark, pad(cut(title, matinTitleWidth), matinTitleWidth), short(candidate.S.ID))
			if openErr := openTab(launcher, title, tabSpec{Cwd: resolved.Path, Args: claudeTabArgs(options.args, candidate.S.ID, options.prompt)}); openErr != nil {
				fmt.Fprintln(output, "   ", L("srv.common.failed", "A1", openErr.Error()))
			}
			select {
			case <-time.After(options.pause):
			case <-runContext.Done():
				return runContext.Err()
			}
		}
		openedCount++
	}
	fmt.Fprintln(output)
	if options.id != "" && openedCount == 0 { // une session précise : ne rien ouvrir est un échec à dire clairement, pas « 0 onglet »
		if opened {
			return terr("srv.matin.cetteSessionEstDeja")
		}
		return terr("srv.matin.sessionIntrouvableDansCe", "A1", short(options.id))
	}
	if options.dry {
		fmt.Fprintln(output, L("cli.matin.wouldReopen", "A1", openedCount))
	} else {
		fmt.Fprintln(output, L("cli.matin.launched", "A1", openedCount))
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
	if fileInfo, statErr := os.Stat(abs); statErr != nil || !fileInfo.IsDir() {
		return "", terr("srv.matin.projetIntrouvable")
	}
	return abs, nil
}
