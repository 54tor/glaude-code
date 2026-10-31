package main

// Titre automatique d'une session : Haiku résume le début de la conversation en quelques mots.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// pendingTitles : sessions ouvertes dont le titre automatique attend la fin du processus (id -> transcript).
var (
	pendingTitles   = map[string]string{}
	pendingTitlesMu sync.Mutex
)

func deferTitle(id, path string) {
	pendingTitlesMu.Lock()
	pendingTitles[id] = path
	pendingTitlesMu.Unlock()
}

// flushTitles pose le titre des sessions en attente qui ne sont plus ouvertes ; une session sans titre à ce moment-là seulement.
func flushTitles() {
	pendingTitlesMu.Lock()
	todo := make(map[string]string, len(pendingTitles))
	for id, p := range pendingTitles {
		todo[id] = p
	}
	pendingTitlesMu.Unlock()
	if len(todo) == 0 {
		return
	}
	live, err := liveCheck()
	if err != nil {
		return
	}
	for id, p := range todo {
		if live[id] {
			continue
		}
		pendingTitlesMu.Lock()
		delete(pendingTitles, id)
		pendingTitlesMu.Unlock()
		if hasTitle(p) {
			continue
		}
		title, err := autoTitle(p)
		if err == nil {
			err = setTitle(p, id, title)
		}
		journal("auto-title %s « %s » : %v", id, title, errOr(err))
	}
}

func titleLoop(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-t.C:
			flushTitles()
		}
	}
}

func titleSystem() string { return L("prompt.titleSystem", "A1", replyLang()) }

// titleRun demande le titre à Haiku (remplaçable dans les tests).
var titleRun = func(text string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := helperCmd(ctx, "haiku", titleSystem())
	cmd.Stdin = strings.NewReader(text)
	out, err := helperOutput(ctx, cmd)
	var r struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(out, &r) != nil {
		if err == nil {
			err = terr("srv.memwrite.reponseIllisible")
		}
		return "", err
	}
	if r.IsError {
		return "", errors.New(cut(r.Result, 300))
	}
	return r.Result, nil
}

// autoTitle propose un titre d'après les premiers échanges du transcript.
func autoTitle(path string) (string, error) {
	var b strings.Builder
	n := 0
	for _, m := range conv(path, false) {
		if m.T == "" {
			continue
		}
		b.WriteString(m.R + " : " + cut(m.T, 600) + "\n\n")
		if n++; n >= 6 {
			break
		}
	}
	if b.Len() == 0 {
		return "", terr("srv.autotitle.emptyConversation")
	}
	t, err := titleRun(b.String())
	if err != nil {
		return "", err
	}
	t = strings.Trim(spaceRe.ReplaceAllString(t, " "), " \"«».")
	if t == "" {
		return "", terr("srv.autotitle.emptyTitle")
	}
	return cut(t, 80), nil
}
