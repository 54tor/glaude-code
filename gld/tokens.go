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
	tokRunMu  sync.Mutex // sérialise les appels à Haiku, sans retenir tokMu (lectures du cache) pendant leurs 90 s
	tokLoaded *tokenStore
)

func tokenFile() string { return filepath.Join(cfgDir(), "gld-web-tokens.json") }

func tokens() *tokenStore {
	if tokLoaded == nil {
		tokLoaded = &tokenStore{Counts: map[string]int{}}
		if storeJSON, err := readTrusted(tokenFile()); err == nil {
			var decodedStore tokenStore
			if json.Unmarshal(storeJSON, &decodedStore) == nil && decodedStore.Counts != nil {
				tokLoaded = &decodedStore
			}
		}
	}
	return tokLoaded
}

func saveTokens() {
	storeJSON, _ := json.Marshal(tokLoaded)
	rWriteAtomic(cfgDir(), tokenFile(), storeJSON, 0o600)
}

func textHash(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])
}

// estimateTokens : estimation du nombre de tokens d'un texte de byteCount octets, d'après les comptes réels déjà faits
// (3 octets par token tant qu'on n'en a pas assez).
func estimateTokens(byteCount int) int {
	tokMu.Lock()
	defer tokMu.Unlock()
	store := tokens()
	if store.SumToks >= 400 {
		return max((byteCount*store.SumToks+store.SumBytes/2)/store.SumBytes, 1)
	}
	return max((byteCount+2)/3, 1)
}

// knownTokens : le compte réel d'un texte, s'il a déjà été fait.
func knownTokens(text string) (int, bool) {
	tokMu.Lock()
	defer tokMu.Unlock()
	tokenCount, found := tokens().Counts[textHash(text)]
	return tokenCount, found
}

// tokenRun envoie text à Haiku et renvoie les tokens d'entrée comptés par le serveur, le modèle et le coût (remplaçable dans les tests).
var tokenRun = func(text string) (int, string, float64, error) {
	callContext, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	helperCommand := helperCmd(callContext, "haiku", tokenSystem)
	helperCommand.Stdin = strings.NewReader(text)
	helperResponse, err := helperOutput(callContext, helperCommand)
	var result struct {
		IsError    bool                       `json:"is_error"`
		Result     string                     `json:"result"`
		Cost       float64                    `json:"total_cost_usd"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	var rawUsage struct {
		Usage struct {
			In     int `json:"input_tokens"`
			Create int `json:"cache_creation_input_tokens"`
			Read   int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(helperResponse, &result) != nil || json.Unmarshal(helperResponse, &rawUsage) != nil {
		if err == nil {
			err = terr("srv.memwrite.reponseIllisible")
		}
		return 0, "", 0, err
	}
	if result.IsError {
		return 0, "", result.Cost, errors.New(cut(result.Result, 300))
	}
	model := ""
	for modelName := range result.ModelUsage {
		model = modelName
	}
	return rawUsage.Usage.In + rawUsage.Usage.Create + rawUsage.Usage.Read, model, result.Cost, nil
}

// countTokens renvoie les tokens de text (depuis le cache, sinon par un appel), le coût de l'appel et si le compte était connu.
func countTokens(text string) (tokenCount int, cost float64, cached bool, err error) {
	if knownCount, found := knownTokens(text); found {
		return knownCount, 0, true, nil
	}
	if strings.TrimSpace(text) == "" {
		return 0, 0, true, nil
	}
	tokRunMu.Lock() // un seul appel à la fois : le coût fixe et le cache restent cohérents, sans bloquer les lectures du cache
	defer tokRunMu.Unlock()
	total, model, callCost, err := tokenRun(text)
	if err != nil {
		return 0, callCost, false, err
	}
	cost += callCost
	tokMu.Lock()
	store := tokens()
	remeasureBase := store.Model != model || store.Base == 0
	tokMu.Unlock()
	var singleTokenTotal int
	if remeasureBase { // nouveau modèle : ses comptes anciens ne valent plus, on remesure le coût fixe
		var baseCallCost float64
		singleTokenTotal, _, baseCallCost, err = tokenRun("x")
		cost += baseCallCost
		if err != nil {
			return 0, cost, false, err
		}
	}
	tokMu.Lock()
	defer tokMu.Unlock()
	if remeasureBase {
		store.Model, store.Base, store.Counts, store.SumBytes, store.SumToks = model, singleTokenTotal-1, map[string]int{}, 0, 0
	}
	tokenCount = max(total-store.Base, 1)
	if _, seen := store.Counts[textHash(text)]; !seen {
		store.SumBytes += len(text)
		store.SumToks += tokenCount
	}
	store.Counts[textHash(text)] = tokenCount
	saveTokens()
	return tokenCount, cost, false, nil
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
