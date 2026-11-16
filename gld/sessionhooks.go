package main

// Hooks de Claude Code que gld pose dans settings.json pour suivre les sous-agents (sessionevents.go). Même cycle que les
// règles intégrées : installés au démarrage, retirables d'un interrupteur, et seules les entrées de gld sont touchées.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

const (
	gldEventCommand   = `[ -x "$HOME/.local/bin/gld-web" ] && "$HOME/.local/bin/gld-web" _event; exit 0`
	gldEventTimeout   = 5
	sessionHooksBody  = 1 << 10
	settingsFileName  = "settings.json"
	settingsFileModes = 0o600
)

var gldEventHookEvents = []string{"SessionStart", "SessionEnd", "Stop", "SubagentStart", "SubagentStop"}

func sessionHooksFlagFile() string { return filepath.Join(cfgDir(), "gld-web-hooks.json") }

// sessionHooksOn : vrai tant que l'utilisateur n'a pas coupé les hooks.
func sessionHooksOn() bool {
	var flagFile struct{ Off bool }
	if content, err := readTrusted(sessionHooksFlagFile()); err == nil && json.Unmarshal(content, &flagFile) == nil {
		return !flagFile.Off
	}
	return true
}

func setSessionHooksOn(enabled bool) error {
	content, _ := json.Marshal(map[string]bool{"off": !enabled})
	if err := rWriteAtomic(cfgDir(), sessionHooksFlagFile(), content, 0o600); err != nil {
		return err
	}
	_, err := syncSessionHooks(enabled)
	return err
}

func isGldEventHook(hook any) bool {
	hookFields, isObject := hook.(map[string]any)
	return isObject && hookFields["command"] == gldEventCommand
}

// withoutGldEventHooks : les groupes de hooks d'un événement, privés des entrées de gld (un groupe vide disparaît).
func withoutGldEventHooks(groups []any) []any {
	kept := []any{}
	for _, group := range groups {
		groupFields, isObject := group.(map[string]any)
		hookList, hasHooks := groupFields["hooks"].([]any)
		if !isObject || !hasHooks {
			kept = append(kept, group)
			continue
		}
		otherHooks := []any{}
		for _, hook := range hookList {
			if !isGldEventHook(hook) {
				otherHooks = append(otherHooks, hook)
			}
		}
		if len(otherHooks) == 0 {
			continue
		}
		groupFields["hooks"] = otherHooks
		kept = append(kept, groupFields)
	}
	return kept
}

func gldEventGroup() map[string]any {
	return map[string]any{"hooks": []any{map[string]any{"type": "command", "async": true, "timeout": gldEventTimeout, "command": gldEventCommand}}}
}

// syncSessionHooks pose ou retire les hooks de gld dans settings.json ; renvoie vrai si le fichier a changé. Un fichier
// absent est créé à l'installation, un fichier illisible n'est jamais écrasé.
func syncSessionHooks(enabled bool) (bool, error) {
	settingsPath := filepath.Join(cfgDir(), settingsFileName)
	current, readErr := readTrusted(settingsPath)
	settings := map[string]any{}
	switch {
	case readErr == nil:
		decoder := json.NewDecoder(bytes.NewReader(current))
		decoder.UseNumber()
		if err := decoder.Decode(&settings); err != nil || settings == nil {
			return false, errors.New("settings.json illisible : hooks non modifiés")
		}
	case !os.IsNotExist(readErr):
		return false, readErr
	case !enabled:
		return false, nil
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, eventName := range gldEventHookEvents {
		existing, _ := hooks[eventName].([]any)
		updated := withoutGldEventHooks(existing)
		if enabled {
			updated = append(updated, gldEventGroup())
		}
		if len(updated) == 0 {
			delete(hooks, eventName)
		} else {
			hooks[eventName] = updated
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false) // « && » et « < » restent lisibles dans le fichier de l'utilisateur
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(settings); err != nil {
		return false, err
	}
	wanted := encoded.Bytes()
	if readErr == nil && jsonEquivalent(current, wanted) {
		return false, nil
	}
	return true, rWriteAtomic(cfgDir(), settingsPath, wanted, settingsFileModes)
}

func jsonEquivalent(left, right []byte) bool {
	var leftValue, rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && bytes.Equal(mustMarshal(leftValue), mustMarshal(rightValue))
}

func mustMarshal(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func routesSessionHooks(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/session-hooks", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, http.StatusOK, map[string]any{"enabled": sessionHooksOn()})
	})
	mux.HandleFunc("PUT /v1/session-hooks", func(responseWriter http.ResponseWriter, request *http.Request) {
		var payload struct{ Enabled bool }
		if !decodeBody(responseWriter, request, &payload, sessionHooksBody) {
			return
		}
		if err := setSessionHooksOn(payload.Enabled); err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"enabled": payload.Enabled})
	})
}
