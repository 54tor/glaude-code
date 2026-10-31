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
	count, cost, cached, err := countTokens("abcdefgh") // 2 tokens
	if err != nil || count != 2 || cached || cost <= 0 {
		t.Fatal(count, cost, cached, err)
	}
	if appels != 2 { // le texte, puis la mesure du coût fixe
		t.Fatal("appels :", appels)
	}
	// deuxième demande : depuis le cache, sans appel
	if count, _, cached, err := countTokens("abcdefgh"); err != nil || count != 2 || !cached || appels != 2 {
		t.Fatal(count, cached, err, appels)
	}
	// un autre texte : un seul appel, le coût fixe est connu
	if count, _, _, err := countTokens("abcdefghijklmnop"); err != nil || count != 4 || appels != 3 {
		t.Fatal(count, err, appels)
	}
	if thirdIndex, ok := knownTokens("abcdefgh"); !ok || thirdIndex != 2 {
		t.Fatal(thirdIndex, ok)
	}
	if _, _, _, err := countTokens("boom"); err == nil {
		t.Fatal("erreur d'appel avalée")
	}
	// un autre modèle invalide les comptes anciens
	tokenRun = func(text string) (int, string, float64, error) { return 10 + (len(text)+3)/4, "claude-autre", 0, nil }
	if count, _, _, err := countTokens("nouveau texte ici"); err != nil || count != 5 {
		t.Fatal(count, err)
	}
	if _, ok := knownTokens("abcdefgh"); ok {
		t.Fatal("compte d'un autre modèle conservé")
	}
	// estimation : 3 octets par token au départ, puis le rapport observé une fois assez de textes comptés
	tokMu.Lock()
	tokLoaded = &tokenStore{Counts: map[string]int{}}
	tokMu.Unlock()
	if number := estimateTokens(300); number != 100 {
		t.Fatal("estimation par défaut :", number)
	}
	tokMu.Lock()
	tokLoaded.SumBytes, tokLoaded.SumToks = 1000, 500 // 2 octets par token
	tokMu.Unlock()
	if number := estimateTokens(300); number != 150 {
		t.Fatal("estimation calibrée :", number)
	}
}
