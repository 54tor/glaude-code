package main

// Les noms qui désignent l'opérateur en dehors de son compte système : identité git, compte Claude, nom de la machine.
// Ils sont masqués dans ce qu'un invité reçoit, au même titre que le compte.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	identityTermMinLength = 4
	identityCacheTTL      = 5 * time.Minute
	identityGitTimeout    = 3 * time.Second
)

var (
	identityMu        sync.Mutex
	identityCached    []string
	identityCachedAt  time.Time
	identityLoadTerms = loadIdentityTerms
)

// hostIdentityTerms renvoie les termes d'identité de l'hôte, relus au plus toutes les identityCacheTTL.
func hostIdentityTerms() []string {
	identityMu.Lock()
	defer identityMu.Unlock()
	if identityCached != nil && time.Since(identityCachedAt) < identityCacheTTL {
		return identityCached
	}
	identityCached, identityCachedAt = identityLoadTerms(), time.Now()
	return identityCached
}

func loadIdentityTerms() []string {
	rawTerms := append(gitIdentityValues(), claudeAccountValues()...)
	if hostName, err := os.Hostname(); err == nil {
		rawTerms = append(rawTerms, hostName)
	}
	return expandIdentityTerms(rawTerms)
}

// expandIdentityTerms ajoute à chaque valeur ses mots (nom complet) et la partie locale d'une adresse, sans doublon, du plus long au plus court.
func expandIdentityTerms(rawTerms []string) []string {
	seenTerms := map[string]bool{}
	var terms []string
	addTerm := func(term string) {
		term = strings.TrimSpace(term)
		if len(term) >= identityTermMinLength && !seenTerms[strings.ToLower(term)] {
			seenTerms[strings.ToLower(term)] = true
			terms = append(terms, term)
		}
	}
	for _, rawTerm := range rawTerms {
		addTerm(rawTerm)
		if localPart, _, isAddress := strings.Cut(rawTerm, "@"); isAddress {
			addTerm(localPart)
		}
		for _, word := range strings.Fields(rawTerm) {
			addTerm(word)
		}
	}
	sort.Slice(terms, func(first, second int) bool { return len(terms[first]) > len(terms[second]) })
	return terms
}

func gitIdentityValues() []string {
	commandContext, cancel := context.WithTimeout(context.Background(), identityGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandContext, "git", "config", "--global", "--get-regexp", `^user\.(name|email)$`) // #nosec G204 -- exécutable et arguments fixes
	cmd.Env = append(childEnv(), "GIT_TERMINAL_PROMPT=0")
	applyRunAs(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	var values []string
	for _, configLine := range strings.Split(string(output), "\n") {
		if _, value, found := strings.Cut(configLine, " "); found {
			values = append(values, value)
		}
	}
	return values
}

func claudeAccountValues() []string {
	content, err := readTrusted(claudeJSON())
	if err != nil {
		return nil
	}
	var claudeConfig struct {
		OAuth struct {
			Email string `json:"emailAddress"`
			Name  string `json:"displayName"`
		} `json:"oauthAccount"`
	}
	if json.Unmarshal(content, &claudeConfig) != nil {
		return nil
	}
	return []string{claudeConfig.OAuth.Email, claudeConfig.OAuth.Name}
}
