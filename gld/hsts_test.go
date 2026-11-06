package main

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

// L'en-tête ne part que vers le nom de --public-url en https, jamais vers une IP, localhost ou en HTTP clair.
func TestSetHSTSSeulementPourLAdressePubliqueHTTPS(t *testing.T) {
	casDeTest := []struct {
		nom, adressePublique, hoteDeLaRequete string
		enHTTPS, attendu                      bool
	}{
		{"nom public en https", "https://gld.example.com", "gld.example.com:8799", true, true},
		{"autre nom", "https://gld.example.com", "autre.example.com", true, false},
		{"requête en clair", "https://gld.example.com", "gld.example.com", false, false},
		{"adresse publique en http", "http://gld.example.com", "gld.example.com", true, false},
		{"adresse IP", "https://192.0.2.10:8799", "192.0.2.10:8799", true, false},
		{"localhost", "https://localhost:8799", "localhost:8799", true, false},
		{"sans adresse publique", "", "gld.example.com", true, false},
	}
	for _, casCourant := range casDeTest {
		t.Run(casCourant.nom, func(t *testing.T) {
			t.Setenv("GLD_PUBLIC_URL", casCourant.adressePublique)
			requete := httptest.NewRequest("GET", "/", nil)
			requete.Host = casCourant.hoteDeLaRequete
			if casCourant.enHTTPS {
				requete.TLS = &tls.ConnectionState{}
			}
			enregistreur := httptest.NewRecorder()
			setHSTS(enregistreur, requete)
			valeurObtenue := enregistreur.Header().Get("Strict-Transport-Security") != ""
			if valeurObtenue != casCourant.attendu {
				t.Fatalf("HSTS posé = %v, attendu %v", valeurObtenue, casCourant.attendu)
			}
		})
	}
}
