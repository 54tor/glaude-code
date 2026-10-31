// Notification locale (notify-send) quand une session se met à attendre et qu'aucun onglet ne regarde :
// avec un onglet ouvert, c'est l'interface qui notifie (API Notification du navigateur).
package main

import (
	"os/exec"
	"sync/atomic"
	"time"
)

// Un GET /v1/state servi depuis moins de trois intervalles de surveillance signale un onglet ouvert.
const openTabSilentTicks = 3

var lastAPI atomic.Int64 // dernier GET /v1/state servi (UnixNano) : un onglet ouvert le redemande toutes les 15 s

// newWaiting : ids présents dans currentWaiting et absents de previousWaiting.
func newWaiting(previousWaiting map[string]bool, currentWaiting []string) (freshIDs []string) {
	for _, sessionID := range currentWaiting {
		if !previousWaiting[sessionID] {
			freshIDs = append(freshIDs, sessionID)
		}
	}
	return
}

var notifySend = func(title, body string) { _ = exec.Command("notify-send", "-a", "gld", title, body).Run() }

// watchWaiting : toutes les every, notifie les sessions qui viennent de passer à « attend » si aucun onglet n'est ouvert.
func watchWaiting(every time.Duration) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	previousWaiting := map[string]bool{}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-ticker.C:
		}
		currentWaiting := waiting()
		fresh := newWaiting(previousWaiting, currentWaiting)
		previousWaiting = map[string]bool{}
		for _, sessionID := range currentWaiting {
			previousWaiting[sessionID] = true
		}
		isTabWatching := time.Since(time.Unix(0, lastAPI.Load())) < openTabSilentTicks*every
		if len(fresh) == 0 || isTabWatching {
			continue
		}
		titles := map[string]string{}
		for _, group := range sessions() {
			for _, session := range group.Sessions {
				titles[session.ID] = session.Title
			}
		}
		for _, sessionID := range fresh {
			title := titles[sessionID]
			if title == "" {
				title = sessionID
			}
			notifySend("Claude attend", title)
		}
	}
}
