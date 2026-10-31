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

const (
	permRuleMaxLength        = 300
	permExplanationMaxLength = 900
	permHelpCacheMax         = 500
)

var (
	permHelpMu    sync.Mutex
	permHelpCache = map[string]string{}
)

// permHelpRun interroge Haiku (remplaçable dans les tests).
var permHelpRun = func(prompt string) (string, error) {
	helperContext, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := helperCmd(helperContext, "haiku", permHelpSystem())
	cmd.Stdin = strings.NewReader(prompt)
	output, err := helperOutput(helperContext, cmd)
	var reply struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(output, &reply) != nil {
		if err == nil {
			err = terr("srv.memwrite.reponseIllisible")
		}
		return "", err
	}
	if reply.IsError {
		return "", errors.New(cut(reply.Result, 300))
	}
	return reply.Result, nil
}

// permHelp : l'explication d'une règle, gardée en mémoire (une règle ne change pas de sens).
func permHelp(kind, rule string) (string, error) {
	rule = strings.TrimSpace(rule)
	if rule == "" || len(rule) > permRuleMaxLength || strings.ContainsAny(rule, "\n\r") {
		return "", terr("srv.permhelp.regleInvalide")
	}
	if kind != "allow" && kind != "ask" && kind != "deny" {
		kind = ""
	}
	key := replyLang() + "\x00" + kind + "\x00" + rule
	permHelpMu.Lock()
	explanation, isCached := permHelpCache[key]
	permHelpMu.Unlock()
	if isCached {
		return explanation, nil
	}
	prompt := L("prompt.rule", "A1", rule)
	explanation, err := permHelpRun(prompt)
	if err != nil {
		return "", err
	}
	if explanation = strings.TrimSpace(explanation); explanation == "" {
		return "", terr("srv.permhelp.explicationVide")
	}
	explanation = cut(explanation, permExplanationMaxLength)
	permHelpMu.Lock()
	if len(permHelpCache) > permHelpCacheMax {
		clear(permHelpCache)
	}
	permHelpCache[key] = explanation
	permHelpMu.Unlock()
	return explanation, nil
}

func routesPermHelp(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/config/permissions/explanations", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Kind, Rule string }
		if !decodeBody(responseWriter, request, &body, 4<<10) {
			return
		}
		explanation, err := permHelp(body.Kind, body.Rule)
		if err != nil {
			writeErr(responseWriter, http.StatusServiceUnavailable, "unavailable", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"out": explanation})
	})
}
