package main

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"
)

// Règles User-wide livrées avec gld : copiées en gld-*.md dans <config Claude>/rules à chaque démarrage, pour que Claude Code
// les charge dans toutes les sessions (format de sortie, questions en TUI que asking.go détecte, fichiers longs).
//
//go:embed rules/*.md
var builtinRules embed.FS

// installBuiltinRules écrit les règles embarquées si elles manquent ou diffèrent ; renvoie les noms réécrits.
func installBuiltinRules() ([]string, error) {
	dir := filepath.Join(cfgDir(), "rules")
	ents, err := builtinRules.ReadDir("rules")
	if err != nil {
		return nil, err
	}
	var done []string
	for _, e := range ents {
		want, err := builtinRules.ReadFile("rules/" + e.Name())
		if err != nil {
			return done, err
		}
		dst := filepath.Join(dir, e.Name())
		if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, want) {
			continue
		}
		if err := rMkdirAll(cfgDir(), dir, 0o755); err != nil {
			return done, err
		}
		if err := rWriteAtomic(cfgDir(), dst, want, 0o644); err != nil {
			return done, err
		}
		done = append(done, e.Name())
	}
	return done, nil
}
