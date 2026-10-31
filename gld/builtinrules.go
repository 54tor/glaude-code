package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Règles livrées avec gld : copiées dans <config Claude>/gld-rules à chaque démarrage (lecture seule dans l'onglet Mémoire) et
// transmises en prompt système aux seules sessions que gld lance (format de sortie, questions en TUI que asking.go détecte,
// fichiers longs). Elles n'entrent plus dans rules/, que Claude Code charge dans toutes les sessions, gld ou non.
//
//go:embed rules/*.md
var builtinRules embed.FS

// builtinRetired : noms des règles que gld écrivait autrefois dans rules/. Le fichier est supprimé, sauf s'il ne porte plus
// la marque de gld (l'utilisateur l'a repris à son compte).
var builtinRetired = []string{"gld-askuser.md", "gld-long-output.md", "gld-output-format.md", "gld-todo-notes.md"}

const (
	builtinMark         = "<!-- Managed by gld:"
	builtinRulesBodyMax = 1 << 10
	claudeHelpTimeout   = 15 * time.Second
)

func builtinDir() string        { return filepath.Join(cfgDir(), "gld-rules") }
func builtinPromptFile() string { return filepath.Join(builtinDir(), "all.prompt") }
func builtinFlagFile() string   { return filepath.Join(cfgDir(), "gld-web-rules.json") }

// builtinOn : les règles gld sont-elles transmises aux sessions ? Oui tant que l'utilisateur ne les a pas coupées.
func builtinOn() bool {
	var flagFile struct{ Off bool }
	if content, err := readTrusted(builtinFlagFile()); err == nil && json.Unmarshal(content, &flagFile) == nil {
		return !flagFile.Off
	}
	return true
}

func setBuiltinOn(enabled bool) error {
	content, _ := json.Marshal(map[string]bool{"off": !enabled})
	return rWriteAtomic(cfgDir(), builtinFlagFile(), content, 0o600)
}

// claudeHelp : l'aide de claude (remplaçable dans les tests).
var claudeHelp = func() string {
	commandContext, cancel := context.WithTimeout(context.Background(), claudeHelpTimeout)
	defer cancel()
	helpOutput, _ := withPath(exec.CommandContext(commandContext, "claude", "--help")).Output()
	return string(helpOutput)
}

var (
	promptFlagOnce sync.Once
	promptFlagName string
)

// promptFlag : l'option de claude qui ajoute du texte au prompt système, selon sa version ("" si elle n'en a aucune).
func promptFlag() string {
	promptFlagOnce.Do(func() {
		helpText := claudeHelp()
		switch {
		case strings.Contains(helpText, "--append-system-prompt-file"):
			promptFlagName = "--append-system-prompt-file"
		case strings.Contains(helpText, "--append-system-prompt"):
			promptFlagName = "--append-system-prompt"
		}
		if promptFlagName == "" && helpText != "" {
			journal("règles gld : cette version de claude n'a pas d'option de prompt système, règles non transmises")
		}
	})
	return promptFlagName
}

// builtinPromptArgs : l'option claude qui transmet les règles gld, vide si elles sont coupées, pas encore écrites ou
// si claude ne sait pas les recevoir (un option inconnue empêcherait de lancer la session).
func builtinPromptArgs() []string {
	if !builtinOn() {
		return nil
	}
	promptContent, err := os.ReadFile(builtinPromptFile())
	if err != nil {
		return nil
	}
	switch promptFlag() {
	case "--append-system-prompt-file":
		return []string{"--append-system-prompt-file", builtinPromptFile()}
	case "--append-system-prompt":
		return []string{"--append-system-prompt", string(promptContent)}
	}
	return nil
}

// ruleBody : le texte d'une règle embarquée sans son front matter ni sa marque.
func ruleBody(content []byte) string {
	text := string(content)
	if rest, hasFrontMatter := strings.CutPrefix(text, "---\n"); hasFrontMatter {
		if _, after, found := strings.Cut(rest, "\n---\n"); found {
			text = after
		}
	}
	var lines []string
	for _, ruleLine := range strings.Split(text, "\n") {
		if !strings.HasPrefix(ruleLine, builtinMark) {
			lines = append(lines, ruleLine)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// installBuiltinRules écrit les règles embarquées si elles manquent ou diffèrent et retire les anciennes copies de rules/ ;
// renvoie les noms touchés (préfixe - : supprimée).
func installBuiltinRules() ([]string, error) {
	entries, err := builtinRules.ReadDir("rules")
	if err != nil {
		return nil, err
	}
	var touched []string
	for _, retiredName := range builtinRetired {
		retiredPath := filepath.Join(cfgDir(), "rules", retiredName)
		if content, readErr := readTrusted(retiredPath); readErr == nil && bytes.Contains(content, []byte(builtinMark)) {
			if removeErr := rRemove(cfgDir(), retiredPath); removeErr != nil {
				return touched, removeErr
			}
			touched = append(touched, "-"+retiredName)
		}
	}
	rulesDir := builtinDir()
	var promptParts []string
	writeIfChanged := func(path string, want []byte) (bool, error) {
		if current, readErr := readTrusted(path); readErr == nil && bytes.Equal(current, want) {
			return false, nil
		}
		if mkdirErr := rMkdirAll(cfgDir(), rulesDir, 0o755); mkdirErr != nil {
			return false, mkdirErr
		}
		return true, rWriteAtomic(cfgDir(), path, want, 0o644)
	}
	for _, entry := range entries {
		want, readErr := builtinRules.ReadFile("rules/" + entry.Name())
		if readErr != nil {
			return touched, readErr
		}
		promptParts = append(promptParts, ruleBody(want))
		changed, writeErr := writeIfChanged(filepath.Join(rulesDir, entry.Name()), want)
		if writeErr != nil {
			return touched, writeErr
		}
		if changed {
			touched = append(touched, entry.Name())
		}
	}
	_, err = writeIfChanged(builtinPromptFile(), []byte(strings.Join(promptParts, "\n\n")+"\n"))
	return touched, err
}

func routesBuiltinRules(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/builtin-rules", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, http.StatusOK, map[string]any{"enabled": builtinOn()})
	})
	mux.HandleFunc("PUT /v1/builtin-rules", func(responseWriter http.ResponseWriter, request *http.Request) {
		var payload struct{ Enabled bool }
		if !decodeBody(responseWriter, request, &payload, builtinRulesBodyMax) {
			return
		}
		if err := setBuiltinOn(payload.Enabled); err != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, http.StatusOK, map[string]any{"enabled": payload.Enabled})
	})
}
