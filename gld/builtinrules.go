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

const builtinMark = "<!-- Managed by gld:"

func builtinDir() string        { return filepath.Join(cfgDir(), "gld-rules") }
func builtinPromptFile() string { return filepath.Join(builtinDir(), "all.prompt") }
func builtinFlagFile() string   { return filepath.Join(cfgDir(), "gld-web-rules.json") }

// builtinOn : les règles gld sont-elles transmises aux sessions ? Oui tant que l'utilisateur ne les a pas coupées.
func builtinOn() bool {
	var v struct{ Off bool }
	if b, err := readTrusted(builtinFlagFile()); err == nil && json.Unmarshal(b, &v) == nil {
		return !v.Off
	}
	return true
}

func setBuiltinOn(on bool) error {
	b, _ := json.Marshal(map[string]bool{"off": !on})
	return rWriteAtomic(cfgDir(), builtinFlagFile(), b, 0o600)
}

// claudeHelp : l'aide de claude (remplaçable dans les tests).
var claudeHelp = func() string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, _ := withPath(exec.CommandContext(ctx, "claude", "--help")).Output()
	return string(out)
}

var (
	promptFlagOnce sync.Once
	promptFlagName string
)

// promptFlag : l'option de claude qui ajoute du texte au prompt système, selon sa version ("" si elle n'en a aucune).
func promptFlag() string {
	promptFlagOnce.Do(func() {
		h := claudeHelp()
		switch {
		case strings.Contains(h, "--append-system-prompt-file"):
			promptFlagName = "--append-system-prompt-file"
		case strings.Contains(h, "--append-system-prompt"):
			promptFlagName = "--append-system-prompt"
		}
		if promptFlagName == "" && h != "" {
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
	b, err := os.ReadFile(builtinPromptFile())
	if err != nil {
		return nil
	}
	switch promptFlag() {
	case "--append-system-prompt-file":
		return []string{"--append-system-prompt-file", builtinPromptFile()}
	case "--append-system-prompt":
		return []string{"--append-system-prompt", string(b)}
	}
	return nil
}

// ruleBody : le texte d'une règle embarquée sans son front matter ni sa marque.
func ruleBody(b []byte) string {
	s := string(b)
	if rest, ok := strings.CutPrefix(s, "---\n"); ok {
		if _, after, found := strings.Cut(rest, "\n---\n"); found {
			s = after
		}
	}
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if !strings.HasPrefix(l, builtinMark) {
			out = append(out, l)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// installBuiltinRules écrit les règles embarquées si elles manquent ou diffèrent et retire les anciennes copies de rules/ ;
// renvoie les noms touchés (préfixe - : supprimée).
func installBuiltinRules() ([]string, error) {
	ents, err := builtinRules.ReadDir("rules")
	if err != nil {
		return nil, err
	}
	var done []string
	for _, n := range builtinRetired {
		dst := filepath.Join(cfgDir(), "rules", n)
		if cur, err := readTrusted(dst); err == nil && bytes.Contains(cur, []byte(builtinMark)) {
			if err := rRemove(cfgDir(), dst); err != nil {
				return done, err
			}
			done = append(done, "-"+n)
		}
	}
	dir := builtinDir()
	var prompt []string
	write := func(dst string, want []byte) (bool, error) {
		if cur, err := readTrusted(dst); err == nil && bytes.Equal(cur, want) {
			return false, nil
		}
		if err := rMkdirAll(cfgDir(), dir, 0o755); err != nil {
			return false, err
		}
		return true, rWriteAtomic(cfgDir(), dst, want, 0o644)
	}
	for _, e := range ents {
		want, err := builtinRules.ReadFile("rules/" + e.Name())
		if err != nil {
			return done, err
		}
		prompt = append(prompt, ruleBody(want))
		if changed, err := write(filepath.Join(dir, e.Name()), want); err != nil {
			return done, err
		} else if changed {
			done = append(done, e.Name())
		}
	}
	_, err = write(builtinPromptFile(), []byte(strings.Join(prompt, "\n\n")+"\n"))
	return done, err
}

func routesBuiltinRules(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/builtin-rules", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"enabled": builtinOn()})
	})
	mux.HandleFunc("PUT /v1/builtin-rules", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Enabled bool }
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		if err := setBuiltinOn(req.Enabled); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"enabled": req.Enabled})
	})
}
