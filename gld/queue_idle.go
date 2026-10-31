package main

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	idleScreenTailLines = 14
	idleScreenSettle    = 20 * time.Second
)

var (
	idlePrompt = regexp.MustCompile(`^❯\s*$`)

	idleSeenMu sync.Mutex
	idleSeenAt = map[string]time.Time{}
)

// screenIdle : le prompt est vide et rien ne tourne (pas de « esc to interrupt »).
func screenIdle(screen string) bool {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	if len(lines) > idleScreenTailLines {
		lines = lines[len(lines)-idleScreenTailLines:]
	}
	hasEmptyPrompt := false
	for _, line := range lines {
		if idlePrompt.MatchString(line) {
			hasEmptyPrompt = true
		}
	}
	return hasEmptyPrompt && !askBusy.MatchString(strings.Join(lines, "\n"))
}

// idleByScreen rattrape un statut « busy » resté à tort (tâches de fond terminées mais listées) : l'écran doit
// rester au repos idleScreenSettle de suite avant que la file parte.
func idleByScreen(sessionID string, now time.Time) bool {
	screen, err := screenFn(sessionID)
	idleSeenMu.Lock()
	defer idleSeenMu.Unlock()
	if err != nil || !screenIdle(screen) {
		delete(idleSeenAt, sessionID)
		return false
	}
	firstSeen, seen := idleSeenAt[sessionID]
	if !seen {
		idleSeenAt[sessionID] = now
		return false
	}
	return now.Sub(firstSeen) >= idleScreenSettle
}
