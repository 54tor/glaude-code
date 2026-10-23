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

// trustKeys : les touches pour répondre au dialogue tel qu'il est affiché. Les versions récentes n'ont plus de chiffres et le curseur
// (❯) part sur « No, exit » : on le déplace sur la réponse voulue puis on valide. nil si l'écran ne montre pas les deux réponses.
func trustKeys(screen string, yes bool) []string {
	var opts []string
	cursor := -1
	for _, l := range strings.Split(screen, "\n") {
		if m := trustOpt.FindStringSubmatch(l); m != nil {
			if m[1] != "" {
				cursor = len(opts)
			}
			opts = append(opts, strings.ToLower(m[2]))
		}
	}
	want := -1
	for i, o := range opts {
		if strings.HasPrefix(o, "yes") == yes {
			want = i
			break
		}
	}
	if cursor < 0 || want < 0 || len(opts) < 2 {
		return nil
	}
	var keys []string
	for i := cursor; i != want; {
		if want > cursor {
			keys = append(keys, "Down")
			i++
		} else {
			keys = append(keys, "Up")
			i--
		}
	}
	return append(keys, "Enter")
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
			keys := trustKeys(p.screen, req.Yes)
			if keys == nil {
				continue
			}
			ok := true
			for _, k := range keys {
				if _, err := tmuxRun("", "send-keys", "-t", p.pane, k); err != nil {
					ok = false
					break
				}
			}
			if ok {
				n++
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
