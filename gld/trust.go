package main

// Dialogue « faire confiance à ce dossier ? » de Claude Code : une session lancée dans un dossier neuf attend la réponse avant de
// lire le prompt, dans un terminal que personne ne regarde. On repère les panneaux tmux qui l'affichent pour que l'interface pose la
// question, et on n'envoie la réponse (1 ou 2) qu'à un panneau dont l'écran porte bien ce dialogue, dans le dossier demandé.

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var trustRe = regexp.MustCompile(`(?i)do you trust the files in this folder|is this a project you created or one you trust|yes, i trust this folder`)

type trustPane struct{ pane, dir, screen string }

// trustPanes : les panneaux (au plus 40 examinés) dont l'écran montre le dialogue de confiance, avec leur dossier courant. Seuls
// ceux où tourne Claude Code (claude, ou node pour une installation npm) comptent : un cat ou un éditeur qui affiche ce texte non.
func trustPanes() []trustPane {
	paneListing, listErr := tmuxRun("", "list-panes", "-a", "-F", "#{pane_id}\t#{pane_current_command}\t#{pane_current_path}")
	if listErr != nil {
		return nil
	}
	var panes []trustPane
	for paneIndex, paneLine := range strings.Split(strings.TrimSpace(paneListing), "\n") {
		fields := strings.SplitN(paneLine, "\t", 3)
		if paneIndex >= 40 || len(fields) != 3 || !paneRe.MatchString(fields[0]) || (fields[1] != "claude" && fields[1] != "node") || !filepath.IsAbs(fields[2]) {
			continue
		}
		paneID, dir := fields[0], fields[2]
		if screen, captureErr := tmuxRun("", "capture-pane", "-p", "-t", paneID); captureErr == nil && trustRe.MatchString(screen) {
			panes = append(panes, trustPane{paneID, filepath.Clean(dir), screen})
		}
	}
	return panes
}

var trustOpt = regexp.MustCompile(`^\s*(❯|>)?\s*(?:\d+[.)]\s*)?((?:Yes|No)\b.*)$`)

// trustOptions : les deux réponses du dialogue, lues dans le bloc qui suit la question (et nulle part ailleurs sur l'écran), et la
// position du curseur (❯). isDialog est faux si le bloc n'a pas exactement une réponse « Yes » et une réponse « No ».
func trustOptions(screen string) (answers []string, cursor int, isDialog bool) {
	lines := strings.Split(screen, "\n")
	questionIndex := -1
	for lineIndex, screenLine := range lines {
		if trustRe.MatchString(screenLine) {
			questionIndex = lineIndex
			break
		}
	}
	if questionIndex < 0 {
		return nil, -1, false
	}
	cursor = -1
	for _, screenLine := range lines[questionIndex+1:] {
		if match := trustOpt.FindStringSubmatch(screenLine); match != nil {
			if match[1] != "" {
				cursor = len(answers)
			}
			answers = append(answers, strings.ToLower(match[2]))
		} else if len(answers) > 0 && strings.TrimSpace(screenLine) != "" {
			break // le bloc d'options est contigu : une autre ligne le termine
		}
	}
	if len(answers) != 2 || cursor < 0 || strings.HasPrefix(answers[0], "yes") == strings.HasPrefix(answers[1], "yes") {
		return nil, -1, false
	}
	return answers, cursor, true
}

// trustKeys : les déplacements du curseur pour atteindre la réponse voulue. Les versions récentes n'ont plus de chiffres et le curseur
// part sur « No, exit ». faux si l'écran n'est pas sans ambiguïté le dialogue ; la validation (Entrée) est envoyée à part, après
// avoir revérifié l'écran.
func trustKeys(screen string, yes bool) ([]string, bool) {
	answers, cursor, isDialog := trustOptions(screen)
	if !isDialog {
		return nil, false
	}
	wantedIndex := 0
	if strings.HasPrefix(answers[1], "yes") == yes {
		wantedIndex = 1
	}
	var keys []string
	for ; cursor < wantedIndex; cursor++ {
		keys = append(keys, "Down")
	}
	for ; cursor > wantedIndex; cursor-- {
		keys = append(keys, "Up")
	}
	return keys, true
}

// trustOnWant : le curseur est bien sur la réponse voulue.
func trustOnWant(screen string, yes bool) bool {
	answers, cursor, isDialog := trustOptions(screen)
	return isDialog && strings.HasPrefix(answers[cursor], "yes") == yes
}

func routesTrust(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/trust/pending", func(responseWriter http.ResponseWriter, request *http.Request) {
		dirs := []string{}
		for _, pane := range trustPanes() {
			dirs = append(dirs, pane.dir)
		}
		writeJSON(responseWriter, 200, map[string]any{"dirs": dirs})
	})
	mux.HandleFunc("POST /v1/trust/answers", func(responseWriter http.ResponseWriter, request *http.Request) {
		var answer struct {
			Dir string
			Yes bool
		}
		if !decodeBody(responseWriter, request, &answer, 1<<10) {
			return
		}
		if !filepath.IsAbs(answer.Dir) {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		answeredCount := 0
		for _, pane := range trustPanes() {
			if pane.dir != filepath.Clean(answer.Dir) {
				continue
			}
			moves, valid := trustKeys(pane.screen, answer.Yes)
			if !valid {
				continue
			}
			keysSent := true
			for _, key := range moves {
				if _, keyErr := tmuxRun("", "send-keys", "-t", pane.pane, key); keyErr != nil {
					keysSent = false
					break
				}
			}
			// On ne valide qu'après avoir revu l'écran : le curseur doit être sur la réponse demandée.
			// Le terminal redessine après chaque touche : on attend (au plus ~2 s) que le curseur soit en place plutôt que de juger
			// l'écran de l'instant, qui montre encore l'ancienne position.
			for attempt := 0; keysSent && attempt < 20; attempt++ {
				screen, captureErr := tmuxRun("", "capture-pane", "-p", "-t", pane.pane)
				if captureErr == nil && trustOnWant(screen, answer.Yes) {
					if _, enterErr := tmuxRun("", "send-keys", "-t", pane.pane, "Enter"); enterErr == nil {
						answeredCount++
					}
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		journal("trust-answer %s oui=%v : %d panneau(x)", answer.Dir, answer.Yes, answeredCount)
		if answeredCount == 0 {
			writeErr(responseWriter, http.StatusConflict, "conflict", L("srv.trust.plusAucuneQuestionDe"))
			return
		}
		writeJSON(responseWriter, 201, map[string]any{"message": L("srv.trust.reponseEnvoyee")})
	})
}
