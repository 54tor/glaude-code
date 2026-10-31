package main

import (
	"bytes"
	"embed"
	"path/filepath"
)

// Règles User-wide livrées avec gld : copiées en gld-*.md dans <config Claude>/rules à chaque démarrage, pour que Claude Code
// les charge dans toutes les sessions (format de sortie, questions en TUI que asking.go détecte, fichiers longs).
//
//go:embed rules/*.md
var builtinRules embed.FS

// builtinRetired : noms des règles livrées autrefois et retirées depuis. Quand on retire une règle de rules/, ajouter son nom ici :
// le fichier est supprimé du home, sauf s'il ne porte plus la marque de gld (l'utilisateur l'a repris à son compte).
var builtinRetired = []string{}

const builtinMark = "<!-- Managed by gld:"

// installBuiltinRules écrit les règles embarquées si elles manquent ou diffèrent, supprime les retirées ; renvoie les noms touchés (préfixe - : supprimée).
func installBuiltinRules() ([]string, error) {
	dir := filepath.Join(cfgDir(), "rules")
	ents, err := builtinRules.ReadDir("rules")
	if err != nil {
		return nil, err
	}
	var done []string
	for _, n := range builtinRetired {
		dst := filepath.Join(dir, n)
		if cur, err := readTrusted(dst); err == nil && bytes.Contains(cur, []byte(builtinMark)) {
			if err := rRemove(cfgDir(), dst); err != nil {
				return done, err
			}
			done = append(done, "-"+n)
		}
	}
	for _, e := range ents {
		want, err := builtinRules.ReadFile("rules/" + e.Name())
		if err != nil {
			return done, err
		}
		dst := filepath.Join(dir, e.Name())
		if cur, err := readTrusted(dst); err == nil && bytes.Equal(cur, want) {
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
