package main

// Dialogue « faire confiance à ce dossier ? » de Claude Code : une session lancée dans un dossier neuf attend la réponse avant de
// lire le prompt, dans un terminal que personne ne regarde. On repère les panneaux tmux qui l'affichent pour que l'interface pose la
// question, et on n'envoie la réponse (1 ou 2) qu'à un panneau dont l'écran porte bien ce dialogue, dans le dossier demandé.

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

var trustRe = regexp.MustCompile(`(?i)do you trust the files in this folder|is this a project you created or one you trust|yes, i trust this folder`)

type trustPane struct{ pane, dir, screen string }

// trustPanes : les panneaux (au plus 40 examinés) dont l'écran montre le dialogue de confiance, avec leur dossier courant. Seuls
// ceux où tourne Claude Code (claude, ou node pour une installation npm) comptent : un cat ou un éditeur qui affiche ce texte non.
func trustPanes() []trustPane {
	out, err := tmuxRun("", "list-panes", "-a", "-F", "#{pane_id}\t#{pane_current_command}\t#{pane_current_path}")
	if err != nil {
		return nil
	}
	var res []trustPane
	for i, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.SplitN(l, "\t", 3)
		if i >= 40 || len(f) != 3 || !paneRe.MatchString(f[0]) || (f[1] != "claude" && f[1] != "node") || !filepath.IsAbs(f[2]) {
			continue
		}
		id, dir := f[0], f[2]
		if scr, err := tmuxRun("", "capture-pane", "-p", "-t", id); err == nil && trustRe.MatchString(scr) {
			res = append(res, trustPane{id, filepath.Clean(dir), scr})
		}
	}
	return res
}

var trustOpt = regexp.MustCompile(`^\s*(❯|>)?\s*(?:\d+[.)]\s*)?((?:Yes|No)\b.*)$`)

// trustOptions : les deux réponses du dialogue, lues dans le bloc qui suit la question (et nulle part ailleurs sur l'écran), et la
// position du curseur (❯). ok est faux si le bloc n'a pas exactement une réponse « Yes » et une réponse « No ».
func trustOptions(screen string) (opts []string, cursor int, ok bool) {
	lines := strings.Split(screen, "\n")
	q := -1
	for i, l := range lines {
		if trustRe.MatchString(l) {
			q = i
			break
		}
	}
	if q < 0 {
		return nil, -1, false
	}
	cursor = -1
	for _, l := range lines[q+1:] {
		if m := trustOpt.FindStringSubmatch(l); m != nil {
			if m[1] != "" {
				cursor = len(opts)
			}
			opts = append(opts, strings.ToLower(m[2]))
		} else if len(opts) > 0 && strings.TrimSpace(l) != "" {
			break // le bloc d'options est contigu : une autre ligne le termine
		}
	}
	if len(opts) != 2 || cursor < 0 || strings.HasPrefix(opts[0], "yes") == strings.HasPrefix(opts[1], "yes") {
		return nil, -1, false
	}
	return opts, cursor, true
}

// trustKeys : les déplacements du curseur pour atteindre la réponse voulue. Les versions récentes n'ont plus de chiffres et le curseur
// part sur « No, exit ». faux si l'écran n'est pas sans ambiguïté le dialogue ; la validation (Entrée) est envoyée à part, après
// avoir revérifié l'écran.
func trustKeys(screen string, yes bool) ([]string, bool) {
	opts, cursor, ok := trustOptions(screen)
	if !ok {
		return nil, false
	}
	want := 0
	if strings.HasPrefix(opts[1], "yes") == yes {
		want = 1
	}
	var keys []string
	for ; cursor < want; cursor++ {
		keys = append(keys, "Down")
	}
	for ; cursor > want; cursor-- {
		keys = append(keys, "Up")
	}
	return keys, true
}

// trustOnWant : le curseur est bien sur la réponse voulue.
func trustOnWant(screen string, yes bool) bool {
	opts, cursor, ok := trustOptions(screen)
	return ok && strings.HasPrefix(opts[cursor], "yes") == yes
}

func routesTrust(mux *http.ServeMux) {
	mux.HandleFunc("GET /trust-pending", func(w http.ResponseWriter, r *http.Request) {
		dirs := []string{}
		for _, p := range trustPanes() {
			dirs = append(dirs, p.dir)
		}
		writeJSON(w, 200, map[string]any{"dirs": dirs})
	})
	mux.HandleFunc("POST /trust-answer", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Dir string
			Yes bool
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil || !filepath.IsAbs(req.Dir) {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
			return
		}
		n := 0
		for _, p := range trustPanes() {
			if p.dir != filepath.Clean(req.Dir) {
				continue
			}
			moves, valid := trustKeys(p.screen, req.Yes)
			if !valid {
				continue
			}
			ok := true
			for _, k := range moves {
				if _, err := tmuxRun("", "send-keys", "-t", p.pane, k); err != nil {
					ok = false
					break
				}
			}
			// On ne valide qu'après avoir revu l'écran : le curseur doit être sur la réponse demandée.
			if scr, err := tmuxRun("", "capture-pane", "-p", "-t", p.pane); ok && err == nil && trustOnWant(scr, req.Yes) {
				if _, err := tmuxRun("", "send-keys", "-t", p.pane, "Enter"); err == nil {
					n++
				}
			}
		}
		journal("trust-answer %s oui=%v : %d panneau(x)", req.Dir, req.Yes, n)
		if n == 0 {
			writeJSON(w, 200, map[string]any{"ok": false, "out": "plus aucune question de confiance à ce dossier"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Réponse envoyée"})
	})
}
