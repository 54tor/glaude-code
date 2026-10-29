package main

// Aide sur une règle de permission : Haiku dit en clair ce qu'elle autorise, le risque et la catégorie qui lui convient.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

func permHelpSystem() string { return L("prompt.permHelpSystem", "A1", replyLang()) }

var (
	permHelpMu    sync.Mutex
	permHelpCache = map[string]string{}
)

// permHelpRun interroge Haiku (remplaçable dans les tests).
var permHelpRun = func(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := helperCmd(ctx, "haiku", permHelpSystem())
	cmd.Stdin = strings.NewReader(prompt)
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

// permHelp : l'explication d'une règle, gardée en mémoire (une règle ne change pas de sens).
func permHelp(kind, rule string) (string, error) {
	rule = strings.TrimSpace(rule)
	if rule == "" || len(rule) > 300 || strings.ContainsAny(rule, "\n\r") {
		return "", terr("srv.permhelp.regleInvalide")
	}
	if kind != "allow" && kind != "ask" && kind != "deny" {
		kind = ""
	}
	key := replyLang() + "\x00" + kind + "\x00" + rule
	permHelpMu.Lock()
	t, ok := permHelpCache[key]
	permHelpMu.Unlock()
	if ok {
		return t, nil
	}
	prompt := L("prompt.rule", "A1", rule)
	t, err := permHelpRun(prompt)
	if err != nil {
		return "", err
	}
	if t = strings.TrimSpace(t); t == "" {
		return "", terr("srv.permhelp.explicationVide")
	}
	t = cut(t, 900)
	permHelpMu.Lock()
	if len(permHelpCache) > 500 {
		clear(permHelpCache)
	}
	permHelpCache[key] = t
	permHelpMu.Unlock()
	return t, nil
}

func routesPermHelp(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/config/permissions/explanations", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Kind, Rule string }
		if !decodeBody(w, r, &req, 4<<10) {
			return
		}
		t, err := permHelp(req.Kind, req.Rule)
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, "unavailable", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"out": t})
	})
}
