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
	lintMaxKeys  = 6
	lintMaxWhy   = 300
)

func lintSystem() string { return L("prompt.lintSystem", "A1", replyLang()) }

// memLintFiles : les clés à analyser pour une portée (« global », ou le dossier de transcripts d'un projet).
func memLintFiles(scope string) ([]string, error) {
	global, projects, _ := memList()
	var keys []string
	if scope == "global" {
		for _, memoryFile := range global {
			if !memoryFile.Off {
				keys = append(keys, memoryFile.Key)
			}
		}
		return keys, nil
	}
	if !safeDir(scope) {
		return nil, terr("srv.memmeta.projetInvalide")
	}
	for _, memoryProject := range projects {
		if memoryProject.Dir != scope {
			continue
		}
		for _, memoryFile := range memoryProject.Files {
			if !strings.HasSuffix(memoryFile.Key, "/MEMORY.md") {
				keys = append(keys, memoryFile.Key)
			}
		}
	}
	if len(keys) == 0 {
		return nil, terr("srv.memlint.rienAAnalyserDans")
	}
	return keys, nil
}

func memLintPrompt(keys []string) (string, error) {
	var promptText strings.Builder
	for _, key := range keys {
		text, err := memRead(key)
		if err != nil {
			continue
		}
		text = cut(text, lintMaxFile)
		if promptText.Len()+len(text) > lintMaxTotal {
			break
		}
		promptText.WriteString("=== " + key + " ===\n" + text + "\n\n")
	}
	if promptText.Len() == 0 {
		return "", terr("srv.memlint.rienAAnalyser")
	}
	return promptText.String(), nil
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
	answer, cost, err := memDraft("haiku", lintSystem(), prompt)
	if err != nil {
		return nil, cost, err
	}
	var raw []Finding
	if decodeErr := json.Unmarshal([]byte(strings.TrimSpace(answer)), &raw); decodeErr != nil {
		return nil, cost, terr("srv.memlint.reponseDeLAnalyse")
	}
	kept := []Finding{}
	for _, finding := range raw {
		isKnownKind := slices.Contains([]string{"doublon", "contradiction", "périmé"}, finding.Kind)
		hasValidKeyCount := len(finding.Keys) > 0 && len(finding.Keys) <= lintMaxKeys
		if !isKnownKind || !hasValidKeyCount {
			continue
		}
		allKnown := true
		for _, key := range finding.Keys {
			allKnown = allKnown && slices.Contains(keys, key)
		}
		if allKnown {
			finding.Why = cut(strings.TrimSpace(finding.Why), lintMaxWhy)
			kept = append(kept, finding)
		}
	}
	return kept, cost, nil
}
