package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func requetePushPour(utilisateur int64, methode, chemin, corps string) *http.Request {
	requete := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
	if utilisateur != 0 {
		garde := &gateInfo{p: &Principal{User: &User{ID: utilisateur}}}
		requete = requete.WithContext(context.WithValue(requete.Context(), gateKey{}, garde))
	}
	return requete
}

func appelerRoutesPush(t *testing.T, utilisateur int64, methode, chemin, corps string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	routesPush(mux)
	reponse := httptest.NewRecorder()
	mux.ServeHTTP(reponse, requetePushPour(utilisateur, methode, chemin, corps))
	return reponse
}

const corpsAbonnementValide = `{"endpoint":"https://push.example.com/a","keys":{"p256dh":"cle","auth":"secret"},"asking":true,"waiting":false,"lang":"en"}`

func TestPushRoutesCleVapidEstServie(t *testing.T) {
	isolerDossierDeConfigurationPush(t)

	reponse := appelerRoutesPush(t, 0, "GET", "/v1/push/key", "")

	clePublique, _, _ := pushVapidKeys()
	if reponse.Code != 200 || !strings.Contains(reponse.Body.String(), clePublique) {
		t.Fatalf("%d %s", reponse.Code, reponse.Body.String())
	}
}

func TestPushRoutesAbonneEtLitLesPreferences(t *testing.T) {
	isolerDossierDeConfigurationPush(t)

	reponseAbonnement := appelerRoutesPush(t, 7, "PUT", "/v1/push/subscriptions", corpsAbonnementValide)
	reponseLecture := appelerRoutesPush(t, 7, "GET", "/v1/push/subscriptions?endpoint=https://push.example.com/a", "")

	if reponseAbonnement.Code != 204 {
		t.Fatalf("abonnement : %d %s", reponseAbonnement.Code, reponseAbonnement.Body.String())
	}
	corpsAttendu := `"subscribed":true`
	if !strings.Contains(reponseLecture.Body.String(), corpsAttendu) || !strings.Contains(reponseLecture.Body.String(), `"asking":true`) || !strings.Contains(reponseLecture.Body.String(), `"waiting":false`) {
		t.Fatal(reponseLecture.Body.String())
	}
	abonnement, _, _ := pushSubscriptionFind("https://push.example.com/a")
	if abonnement.UserID != 7 || abonnement.Lang != "en" {
		t.Fatalf("abonnement stocké : %+v", abonnement)
	}
}

func TestPushRoutesRefuseUnEndpointNonHttpsOuSansCles(t *testing.T) {
	isolerDossierDeConfigurationPush(t)
	corpsInvalides := []string{
		`{"endpoint":"http://push.example.com/a","keys":{"p256dh":"cle","auth":"secret"}}`,
		`{"endpoint":"https://push.example.com/a","keys":{"p256dh":"","auth":"secret"}}`,
		`{"endpoint":"https://push.example.com/a"}`,
		`{"endpoint":"https://` + strings.Repeat("a", 2100) + `","keys":{"p256dh":"cle","auth":"secret"}}`,
	}
	for _, corpsInvalide := range corpsInvalides {
		reponse := appelerRoutesPush(t, 1, "PUT", "/v1/push/subscriptions", corpsInvalide)
		if reponse.Code != 400 {
			t.Fatalf("corps accepté (%d) : %.60s", reponse.Code, corpsInvalide)
		}
	}
}

func TestPushRoutesUnUtilisateurNeVoitNiNeSupprimePasLAbonnementDUnAutre(t *testing.T) {
	isolerDossierDeConfigurationPush(t)
	appelerRoutesPush(t, 7, "PUT", "/v1/push/subscriptions", corpsAbonnementValide)

	lectureParUnAutre := appelerRoutesPush(t, 8, "GET", "/v1/push/subscriptions?endpoint=https://push.example.com/a", "")
	appelerRoutesPush(t, 8, "DELETE", "/v1/push/subscriptions?endpoint=https://push.example.com/a", "")

	if !strings.Contains(lectureParUnAutre.Body.String(), `"subscribed":false`) {
		t.Fatal(lectureParUnAutre.Body.String())
	}
	if _, encoreLa, _ := pushSubscriptionFind("https://push.example.com/a"); !encoreLa {
		t.Fatal("l'abonnement d'un autre utilisateur a été supprimé")
	}
}

func TestPushRoutesDesabonneRetireLAbonnement(t *testing.T) {
	isolerDossierDeConfigurationPush(t)
	appelerRoutesPush(t, 7, "PUT", "/v1/push/subscriptions", corpsAbonnementValide)

	reponse := appelerRoutesPush(t, 7, "DELETE", "/v1/push/subscriptions?endpoint=https://push.example.com/a", "")

	if _, encoreLa, _ := pushSubscriptionFind("https://push.example.com/a"); reponse.Code != 204 || encoreLa {
		t.Fatalf("%d, encore présent=%v", reponse.Code, encoreLa)
	}
}

func TestPushRoutesNeSontJamaisRelayeesAUnHomeEnfant(t *testing.T) {
	use(t.TempDir())
	if erreur := saveHomes([]Home{{Name: "Pro", Dir: t.TempDir()}}); erreur != nil {
		t.Fatal(erreur)
	}
	demarrages := 0
	demarrageInitial := homeStart
	t.Cleanup(func() { homeStart = demarrageInitial })
	homeStart = func(Home) (*homeProc, error) {
		demarrages++
		return nil, errors.New("pas de démarrage en test")
	}
	prochainHandlerAppele := false
	handler := withHomes(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { prochainHandlerAppele = true }))
	requete := httptest.NewRequest("GET", "/v1/push/key", nil)
	requete.AddCookie(&http.Cookie{Name: homeCookie, Value: "Pro"})

	handler.ServeHTTP(httptest.NewRecorder(), requete)

	if demarrages != 0 || !prochainHandlerAppele {
		t.Fatalf("démarrages=%d, servie par la passerelle=%v", demarrages, prochainHandlerAppele)
	}
}
