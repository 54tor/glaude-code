package main

// Décompte réel des tokens d'un fichier de mémoire : le texte est envoyé tel quel à Haiku (sans outils, sans réglages,
// consigne de répondre « ok ») et le serveur dit combien de tokens d'entrée il a reçus, moins le coût fixe de l'appel,
// mesuré une fois sur un texte d'un token. Le tokenizer est celui du modèle, pas une approximation ; le résultat est
// juste à un ou deux tokens près. Les comptes sont gardés par empreinte du texte : un fichier inchangé ne se recompte pas.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const tokenSystem = "Réponds uniquement : ok"

type tokenStore struct {
	Model  string         `json:"model"`  // modèle qui a produit les comptes (un autre modèle peut compter autrement)
	Base   int            `json:"base"`   // coût fixe d'un appel, déjà retiré des comptes
	Counts map[string]int `json:"counts"` // empreinte du texte -> tokens
	// Somme des octets et des tokens des textes déjà comptés : leur rapport calibre l'estimation des textes pas encore comptés
	// (du français accentué fait environ 2,3 octets par token, de l'anglais 4).
	SumBytes int `json:"sumBytes"`
	SumToks  int `json:"sumToks"`
}

var (
	tokMu     sync.Mutex
	tokLoaded *tokenStore
)

func tokenFile() string { return filepath.Join(cfgDir(), "gld-web-tokens.json") }

func tokens() *tokenStore {
	if tokLoaded == nil {
		tokLoaded = &tokenStore{Counts: map[string]int{}}
		if b, err := os.ReadFile(tokenFile()); err == nil {
			var s tokenStore
			if json.Unmarshal(b, &s) == nil && s.Counts != nil {
				tokLoaded = &s
			}
		}
	}
	return tokLoaded
}

func saveTokens() {
	b, _ := json.Marshal(tokLoaded)
	tmp := tokenFile() + ".tmp"
	if writeSynced(tmp, b, 0o600) == nil {
		os.Rename(tmp, tokenFile())
	}
}

func textHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// estimateTokens : estimation du nombre de tokens d'un texte de n octets, d'après les comptes réels déjà faits
// (3 octets par token tant qu'on n'en a pas assez).
func estimateTokens(n int) int {
	tokMu.Lock()
	defer tokMu.Unlock()
	s := tokens()
	if s.SumToks >= 400 {
		return max((n*s.SumToks+s.SumBytes/2)/s.SumBytes, 1)
	}
	return max((n+2)/3, 1)
}

// knownTokens : le compte réel d'un texte, s'il a déjà été fait.
func knownTokens(text string) (int, bool) {
	tokMu.Lock()
	defer tokMu.Unlock()
	n, ok := tokens().Counts[textHash(text)]
	return n, ok
}

// tokenRun envoie text à Haiku et renvoie les tokens d'entrée comptés par le serveur, le modèle et le coût (remplaçable dans les tests).
var tokenRun = func(text string) (int, string, float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := helperCmd(ctx, "haiku", tokenSystem)
	cmd.Stdin = strings.NewReader(text)
	out, err := helperOutput(ctx, cmd)
	var r struct {
		IsError    bool                       `json:"is_error"`
		Result     string                     `json:"result"`
		Cost       float64                    `json:"total_cost_usd"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	var raw struct {
		Usage struct {
			In     int `json:"input_tokens"`
			Create int `json:"cache_creation_input_tokens"`
			Read   int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(out, &r) != nil || json.Unmarshal(out, &raw) != nil {
		if err == nil {
			err = terr("srv.memwrite.reponseIllisible")
		}
		return 0, "", 0, err
	}
	if r.IsError {
		return 0, "", r.Cost, errors.New(cut(r.Result, 300))
	}
	model := ""
	for k := range r.ModelUsage {
		model = k
	}
	return raw.Usage.In + raw.Usage.Create + raw.Usage.Read, model, r.Cost, nil
}

// countTokens renvoie les tokens de text (depuis le cache, sinon par un appel), le coût de l'appel et si le compte était connu.
func countTokens(text string) (n int, cost float64, cached bool, err error) {
	if n, ok := knownTokens(text); ok {
		return n, 0, true, nil
	}
	if strings.TrimSpace(text) == "" {
		return 0, 0, true, nil
	}
	tokMu.Lock()
	defer tokMu.Unlock() // un seul appel à la fois : le coût fixe et le cache restent cohérents
	s := tokens()
	total, model, c, err := tokenRun(text)
	if err != nil {
		return 0, c, false, err
	}
	cost += c
	if s.Model != model || s.Base == 0 { // nouveau modèle : ses comptes anciens ne valent plus, on remesure le coût fixe
		one, _, c2, err := tokenRun("x")
		cost += c2
		if err != nil {
			return 0, cost, false, err
		}
		s.Model, s.Base, s.Counts, s.SumBytes, s.SumToks = model, one-1, map[string]int{}, 0, 0
	}
	n = max(total-s.Base, 1)
	if _, seen := s.Counts[textHash(text)]; !seen {
		s.SumBytes += len(text)
		s.SumToks += n
	}
	s.Counts[textHash(text)] = n
	saveTokens()
	return n, cost, false, nil
}

// loadedText : le texte d'un fichier tel que Claude le charge (les 200 premières lignes seulement pour un index MEMORY.md).
func loadedText(key string) (string, error) {
	text, err := memRead(key)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(key, "/MEMORY.md") {
		if lines := strings.SplitAfter(text, "\n"); len(lines) > indexLines {
			text = strings.Join(lines[:indexLines], "")
		}
	}
	return text, nil
}
