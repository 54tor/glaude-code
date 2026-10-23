package main

// Analyse des règles et de la mémoire par Haiku : doublons, contradictions, entrées périmées. Rien n'est modifié :
// le résultat est une liste de constats que l'utilisateur ouvre et traite lui-même.

import (
	"encoding/json"
	"slices"
	"strings"
)

type Finding struct {
	Kind string   `json:"kind"` // doublon, contradiction ou périmé
	Keys []string `json:"keys"`
	Why  string   `json:"why"`
}

const (
	lintMaxFile  = 6000  // caractères gardés par fichier
	lintMaxTotal = 80000 // caractères envoyés au total
)

func lintSystem() string { return L("prompt.lintSystem", "A1", replyLang()) }

// memLintFiles : les clés à analyser pour une portée (« global », ou le dossier de transcripts d'un projet).
func memLintFiles(scope string) ([]string, error) {
	global, projects, _ := memList()
	var keys []string
	if scope == "global" {
		for _, f := range global {
			if !f.Off {
				keys = append(keys, f.Key)
			}
		}
		return keys, nil
	}
	if !safeDir(scope) {
		return nil, terr("srv.memmeta.projetInvalide")
	}
	for _, p := range projects {
		if p.Dir != scope {
			continue
		}
		for _, f := range p.Files {
			if !strings.HasSuffix(f.Key, "/MEMORY.md") {
				keys = append(keys, f.Key)
			}
		}
	}
	if len(keys) == 0 {
		return nil, terr("srv.memlint.rienAAnalyserDans")
	}
	return keys, nil
}

func memLintPrompt(keys []string) (string, error) {
	var b strings.Builder
	for _, k := range keys {
		text, err := memRead(k)
		if err != nil {
			continue
		}
		text = cut(text, lintMaxFile)
		if b.Len()+len(text) > lintMaxTotal {
			break
		}
		b.WriteString("=== " + k + " ===\n" + text + "\n\n")
	}
	if b.Len() == 0 {
		return "", terr("srv.memlint.rienAAnalyser")
	}
	return b.String(), nil
}

// memLint demande l'analyse à Haiku et ne garde que les constats bien formés, portant sur des fichiers qu'on lui a donnés.
func memLint(scope string) ([]Finding, float64, error) {
	keys, err := memLintFiles(scope)
	if err != nil {
		return nil, 0, err
	}
	prompt, err := memLintPrompt(keys)
	if err != nil {
		return nil, 0, err
	}
	out, cost, err := memDraft("haiku", lintSystem(), prompt)
	if err != nil {
		return nil, cost, err
	}
	var raw []Finding
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); err != nil {
		return nil, cost, terr("srv.memlint.reponseDeLAnalyse")
	}
	res := []Finding{}
	for _, f := range raw {
		if !slices.Contains([]string{"doublon", "contradiction", "périmé"}, f.Kind) || len(f.Keys) == 0 || len(f.Keys) > 6 {
			continue
		}
		ok := true
		for _, k := range f.Keys {
			ok = ok && slices.Contains(keys, k)
		}
		if ok {
			f.Why = cut(strings.TrimSpace(f.Why), 300)
			res = append(res, f)
		}
	}
	return res, cost, nil
}
