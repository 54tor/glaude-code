// Notification locale (notify-send) quand une session se met à attendre et qu'aucun onglet ne regarde :
// avec un onglet ouvert, c'est l'interface qui notifie (API Notification du navigateur).
package main

import (
	"os/exec"
	"sync/atomic"
	"time"
)

var lastAPI atomic.Int64 // dernier GET /v1/state servi (UnixNano) : un onglet ouvert le redemande toutes les 15 s

// newWaiting : ids présents dans cur et absents de prev.
func newWaiting(prev map[string]bool, cur []string) (out []string) {
	for _, id := range cur {
		if !prev[id] {
			out = append(out, id)
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
	prev := map[string]bool{}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-t.C:
		}
		cur := waiting()
		fresh := newWaiting(prev, cur)
		prev = map[string]bool{}
		for _, id := range cur {
			prev[id] = true
		}
		if len(fresh) == 0 || time.Since(time.Unix(0, lastAPI.Load())) < 3*every {
			continue
		}
		titles := map[string]string{}
		for _, g := range sessions() {
			for _, s := range g.Sessions {
				titles[s.ID] = s.Title
			}
		}
		for _, id := range fresh {
			t := titles[id]
			if t == "" {
				t = id
			}
			notifySend("Claude attend", t)
		}
	}
}
