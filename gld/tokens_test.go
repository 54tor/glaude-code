package main

import (
	"errors"
	"testing"
)

func TestCountTokens(t *testing.T) {
	use(t.TempDir())
	tokMu.Lock()
	tokLoaded = nil
	tokMu.Unlock()
	appels := 0
	old := tokenRun
	defer func() { tokenRun = old }()
	// faux modèle : 540 tokens de coût fixe, un token tous les 4 caractères
	tokenRun = func(text string) (int, string, float64, error) {
		appels++
		if text == "boom" {
			return 0, "", 0, errors.New("échec")
		}
		return 540 + (len(text)+3)/4, "claude-haiku-test", 0.0001, nil
	}
	n, cost, cached, err := countTokens("abcdefgh") // 2 tokens
	if err != nil || n != 2 || cached || cost <= 0 {
		t.Fatal(n, cost, cached, err)
	}
	if appels != 2 { // le texte, puis la mesure du coût fixe
		t.Fatal("appels :", appels)
	}
	// deuxième demande : depuis le cache, sans appel
	if n, _, cached, err := countTokens("abcdefgh"); err != nil || n != 2 || !cached || appels != 2 {
		t.Fatal(n, cached, err, appels)
	}
	// un autre texte : un seul appel, le coût fixe est connu
	if n, _, _, err := countTokens("abcdefghijklmnop"); err != nil || n != 4 || appels != 3 {
		t.Fatal(n, err, appels)
	}
	if k, ok := knownTokens("abcdefgh"); !ok || k != 2 {
		t.Fatal(k, ok)
	}
	if _, _, _, err := countTokens("boom"); err == nil {
		t.Fatal("erreur d'appel avalée")
	}
	// un autre modèle invalide les comptes anciens
	tokenRun = func(text string) (int, string, float64, error) { return 10 + (len(text)+3)/4, "claude-autre", 0, nil }
	if n, _, _, err := countTokens("nouveau texte ici"); err != nil || n != 5 {
		t.Fatal(n, err)
	}
	if _, ok := knownTokens("abcdefgh"); ok {
		t.Fatal("compte d'un autre modèle conservé")
	}
	// estimation : 3 octets par token au départ, puis le rapport observé une fois assez de textes comptés
	tokMu.Lock()
	tokLoaded = &tokenStore{Counts: map[string]int{}}
	tokMu.Unlock()
	if e := estimateTokens(300); e != 100 {
		t.Fatal("estimation par défaut :", e)
	}
	tokMu.Lock()
	tokLoaded.SumBytes, tokLoaded.SumToks = 1000, 500 // 2 octets par token
	tokMu.Unlock()
	if e := estimateTokens(300); e != 150 {
		t.Fatal("estimation calibrée :", e)
	}
}
