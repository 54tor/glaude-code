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
	askFoot = regexp.MustCompile(`(?i)enter to (select|confirm)|esc to cancel|tab to amend|ready to submit|do you want to|would you like to|proceed\?`)
	askOpt  = regexp.MustCompile(`^\s*(?:[❯>›]\s*)?[1-9][.)]\s+\S|^\s*❯\s+\S`)
	askBusy = regexp.MustCompile(`(?i)esc to interrupt`)

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

// screenAsks : l'écran montre un dialogue qui attend une réponse (et la session n'est pas en pleine réflexion).
func screenAsks(screen string) bool {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	if len(lines) > askScreenTailLines {
		lines = lines[len(lines)-askScreenTailLines:]
	}
	tail := strings.Join(lines, "\n")
	foot := tail
	if len(lines) > askDialogFootLines {
		foot = strings.Join(lines[len(lines)-askDialogFootLines:], "\n")
	}
	if askBusy.MatchString(foot) || !askFoot.MatchString(foot) {
		return false
	}
	optionCount := 0
	for _, line := range lines {
		if askOpt.MatchString(line) {
			optionCount++
		}
	}
	return optionCount >= askMinimumOptions
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
