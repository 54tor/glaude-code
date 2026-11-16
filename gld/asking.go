package main

// Sessions qui attendent une réponse dans leur terminal (question à choix, demande d'autorisation) : repérées sur l'écran tmux de
// chaque session ouverte, avec la même signature que l'interface (pied d'aide du dialogue + options numérotées ou curseur).

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	askOpt    = regexp.MustCompile(`^\s*(?:[❯>›]\s*)?[1-9][.)]\s+\S`)
	askCursor = regexp.MustCompile(`^\s*[❯>›]\s+[1-9][.)]\s+\S`)
	askBusy   = regexp.MustCompile(`(?i)esc to interrupt`)

	askMu    sync.Mutex
	askAt    time.Time
	askCache []string
)

const (
	askScreenTailLines   = 32
	askDialogFootLines   = 8
	askMinimumOptions    = 2
	askCacheLifetime     = 3 * time.Second
	askMaxPanesInspected = 40
)

// screenAsks : l'écran montre un dialogue à choix (question, autorisation). Détection par la structure seule, sans phrase du
// dialogue (elles changent avec la langue) : un curseur « ❯ » posé sur une option numérotée, au moins deux options, et aucun
// prompt de saisie vide en bas (le dialogue le remplace).
func screenAsks(screen string) bool {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	if len(lines) > askScreenTailLines {
		lines = lines[len(lines)-askScreenTailLines:]
	}
	if hasEmptyPrompt(lines) {
		return false
	}
	optionCount, hasCursor := 0, false
	for _, line := range lines {
		if askOpt.MatchString(line) {
			optionCount++
		}
		hasCursor = hasCursor || askCursor.MatchString(line)
	}
	return hasCursor && optionCount >= askMinimumOptions
}

// hasEmptyPrompt : un dialogue remplace la zone de saisie ; un prompt « ❯ » vide en bas d'écran prouve que la session est au repos,
// même si le dernier message cite « do you want to » et une liste numérotée.
func hasEmptyPrompt(lines []string) bool {
	firstLineChecked := max(0, len(lines)-askDialogFootLines)
	for _, line := range lines[firstLineChecked:] {
		if idlePrompt.MatchString(line) {
			return true
		}
	}
	return false
}

// askingSessions : les ids des sessions ouvertes dans tmux qui attendent une réponse. Résultat gardé 3 s : la liste des sessions
// est relue souvent et chaque session coûte un capture-pane.
func askingSessions() []string {
	askingIDs, _ := askingSessionsKnown()
	return askingIDs
}

// askingSessionsKnown : known faux si tmux n'a pas pu lister les panneaux ; cet échec n'est pas gardé en cache.
var askingSessionsKnown = func() (askingIDs []string, known bool) {
	askMu.Lock()
	defer askMu.Unlock()
	if askCache != nil && time.Since(askAt) < askCacheLifetime {
		return askCache, true
	}
	askingIDs = []string{}
	panes, panesErr := sendable()
	if panesErr != nil {
		return askingIDs, false
	}
	inspectedPanes := 0
	for sessionID, paneTarget := range panes {
		inspectedPanes++
		if inspectedPanes > askMaxPanesInspected {
			break
		}
		screen, captureErr := tmuxRun("", "capture-pane", "-p", "-t", paneTarget)
		if captureErr == nil && screenAsks(screen) {
			askingIDs = append(askingIDs, sessionID)
		}
	}
	sort.Strings(askingIDs)
	askCache, askAt = askingIDs, time.Now()
	return askingIDs, true
}
