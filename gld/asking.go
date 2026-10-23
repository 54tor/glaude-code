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

// screenAsks : l'écran montre un dialogue qui attend une réponse (et la session n'est pas en pleine réflexion).
func screenAsks(screen string) bool {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	if len(lines) > 32 {
		lines = lines[len(lines)-32:]
	}
	tail := strings.Join(lines, "\n")
	foot := tail
	if len(lines) > 8 {
		foot = strings.Join(lines[len(lines)-8:], "\n")
	}
	if askBusy.MatchString(foot) || !askFoot.MatchString(foot) {
		return false
	}
	n := 0
	for _, l := range lines {
		if askOpt.MatchString(l) {
			n++
		}
	}
	return n >= 2
}

// askingSessions : les ids des sessions ouvertes dans tmux qui attendent une réponse. Résultat gardé 3 s : la liste des sessions
// est relue souvent et chaque session coûte un capture-pane.
func askingSessions() []string {
	askMu.Lock()
	defer askMu.Unlock()
	if askCache != nil && time.Since(askAt) < 3*time.Second {
		return askCache
	}
	out := []string{}
	if panes, err := sendable(); err == nil {
		n := 0
		for id, p := range panes {
			if n++; n > 40 {
				break
			}
			if scr, err := tmuxRun("", "capture-pane", "-p", "-t", p); err == nil && screenAsks(scr) {
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	askCache, askAt = out, time.Now()
	return out
}
