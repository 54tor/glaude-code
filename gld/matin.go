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

const matinHelp = `
gld-matin — rouvre chaque sujet Claude d'un projet dans un onglet de terminal.

  gld-matin                      onglets pour le projet courant
  gld-matin -n                   dry-run : affiche, n'ouvre rien
  gld-matin ~/Projets/autre      cible un autre projet
  gld-matin --term tmux          force un lanceur (défaut : auto-détecté)
  gld-matin --term list          liste les lanceurs disponibles ici
  gld-matin --age 40 --min 0     élargit la fenêtre (jours) et le seuil (octets)
  gld-matin --nommees            ignore les sessions sans nom explicite
  gld-matin --id <uuid> [projet] rouvre cette seule session (sans filtre d'âge ni de taille)
  gld-matin --args "..."         options ajoutées à claude (défaut : aucune, ou $GLD_MATIN_ARGS)
  gld-matin --resume [nom]       rouvre les sessions de la dernière sauvegarde de gld
                                 (ou de la sauvegarde manuelle de ce nom), ex. après un redémarrage
  gld-matin --prompt "..." [projet]  ouvre une NOUVELLE session claude avec ce prompt
                                 (avec --id : reprend cette session en lui envoyant ce message)

Un sujet = une session Claude, désignée par son nom, sinon (marqué « ~ ») par son premier prompt.
`

type matinOpts struct {
	dry, nommees                bool
	id, project, prompt, launch string
	args                        []string
	age                         int
	min                         int64
	pause                       time.Duration
	exclure                     string
}

func cliMatin(ctx context.Context, args []string, out io.Writer) error {
	resume, resumeName := false, ""
	var rest []string
	for i := 0; i < len(args); i++ { // --resume prend un nom facultatif
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
			return helpText(out, matinHelp)
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
	saves := loadTabs()
	if len(saves) == 0 {
		if b, err := os.ReadFile(filepath.Join(cfgDir(), "cc-web-tabs.json")); err == nil { // ancien nom, avant la première migration
			saves = readTabs(b)
		}
	}
	if len(saves) == 0 {
		return fmt.Errorf("aucune sauvegarde (%s)", tabsFile())
	}
	var save *TabSave
	for i := range saves {
		if name == "" || saves[i].Name == name {
			save = &saves[i]
			break
		}
	}
	if save == nil {
		return fmt.Errorf("sauvegarde introuvable : %s", name)
	}
	label := "automatique"
	if save.Name != "" {
		label = save.Name
	}
	fmt.Fprintf(out, "sauvegarde : %s du %s, %d session(s)\n", label, strings.Replace(trunc(save.At, 16), "T", " ", 1), len(save.Sessions))
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
		fmt.Fprintln(out, "lanceurs disponibles ici :")
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
			fmt.Fprintf(out, "ouvrirait une nouvelle session dans %s\n", dir)
			return nil
		}
		if launcher == "" {
			return fmt.Errorf("aucun terminal supporté détecté (--term list)")
		}
		if err := openTab(launcher, "claude", tabSpec{Cwd: dir, Args: claudeTabArgs(o.args, "", o.prompt)}); err != nil {
			return err
		}
		fmt.Fprintf(out, "1 onglet lancé : nouvelle session dans %s.\n", dir)
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
		return fmt.Errorf("aucun terminal supporté détecté. --term list pour voir,\n          ou --term <nom> pour en forcer un")
	}
	fmt.Fprintf(out, "projet : %s\n", p.Path)
	if !o.dry {
		fmt.Fprintf(out, "onglets: %s\n", launcher)
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
		fmt.Fprintf(out, "aucune session de moins de %d jours.\n", o.age)
		return nil
	}

	seen := map[string]bool{}
	n := 0
	for _, t := range recent {
		if o.id != "" && t.S.ID != o.id || t.Info.Size() < o.min {
			continue
		}
		title, mark := t.label()
		if mark == "~" && o.nommees || title == "" {
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
			fmt.Fprintf(out, "  déjà ouverte %s %s %s\n", mark, pad(trunc(title, 45), 45), short(t.S.ID))
			continue
		}
		if o.dry {
			fmt.Fprintf(out, "  ouvrirait    %s %s %s  (%s)\n", mark, pad(trunc(title, 45), 45), short(t.S.ID), mdate(t.Info))
		} else {
			fmt.Fprintf(out, "  ouvre        %s %s %s\n", mark, pad(trunc(title, 45), 45), short(t.S.ID))
			if err := openTab(launcher, title, tabSpec{Cwd: p.Path, Args: claudeTabArgs(o.args, t.S.ID, o.prompt)}); err != nil {
				fmt.Fprintf(out, "    échec : %v\n", err)
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
	if o.dry {
		fmt.Fprintf(out, "%d sujet(s) seraient rouverts. Lancez sans -n pour ouvrir.\n", n)
	} else {
		fmt.Fprintf(out, "%d onglet(s) lancé(s).\n", n)
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
		return "", fmt.Errorf("projet introuvable")
	}
	return abs, nil
}
