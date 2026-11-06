package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

var verrouEnvoisPush sync.Mutex

func nombreEnvoisPush(envois *[]envoiPushEnregistre) int {
	verrouEnvoisPush.Lock()
	defer verrouEnvoisPush.Unlock()
	return len(*envois)
}

type envoiPushEnregistre struct {
	endpoint string
	corps    []byte
}

func installerTransportPushDeTest(t *testing.T, statutParEndpoint map[string]int) *[]envoiPushEnregistre {
	t.Helper()
	isolerDossierDeConfigurationPush(t)
	envois := &[]envoiPushEnregistre{}
	transportInitial, droitsInitiaux := pushTransport, pushMayRead
	t.Cleanup(func() { pushTransport, pushMayRead = transportInitial, droitsInitiaux })
	pushTransport = func(abonnement pushSubscription, corps []byte) (int, error) {
		verrouEnvoisPush.Lock()
		*envois = append(*envois, envoiPushEnregistre{abonnement.Endpoint, corps})
		verrouEnvoisPush.Unlock()
		if statut, defini := statutParEndpoint[abonnement.Endpoint]; defini {
			return statut, nil
		}
		return 201, nil
	}
	pushMayRead = func(int64, string) bool { return true }
	return envois
}

func TestPushSendEnvoieSeulementAuxAbonnementsDontLaPreferenceEstActive(t *testing.T) {
	envois := installerTransportPushDeTest(t, nil)
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/a", Asking: true})

	pushDispatch(pushEvent{HomeID: mainHomeID, SessionID: "s1", Kind: "waiting"})
	nombreApresWaiting := len(*envois)
	pushDispatch(pushEvent{HomeID: mainHomeID, SessionID: "s1", Kind: "asking"})

	if nombreApresWaiting != 0 || len(*envois) != 1 {
		t.Fatalf("envois après waiting=%d, au total=%d", nombreApresWaiting, len(*envois))
	}
}

func TestPushSendNeNotifiePasUnUtilisateurSansDroitSessionsRead(t *testing.T) {
	envois := installerTransportPushDeTest(t, nil)
	pushMayRead = func(int64, string) bool { return false }
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/a", Asking: true})

	pushDispatch(pushEvent{HomeID: mainHomeID, SessionID: "s1", Kind: "asking"})

	if len(*envois) != 0 {
		t.Fatalf("%d envois malgré l'absence de droit", len(*envois))
	}
}

func TestPushSendSupprimeLAbonnementExpireEtContinue(t *testing.T) {
	envois := installerTransportPushDeTest(t, map[string]int{"https://push.example.com/a": 410})
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/a", Asking: true})
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/b", Asking: true})

	pushDispatch(pushEvent{HomeID: mainHomeID, SessionID: "s1", Kind: "asking"})

	_, trouveA, _ := pushSubscriptionFind("https://push.example.com/a")
	_, trouveB, _ := pushSubscriptionFind("https://push.example.com/b")
	if trouveA || !trouveB || len(*envois) != 2 {
		t.Fatalf("A trouvé=%v B trouvé=%v envois=%d", trouveA, trouveB, len(*envois))
	}
}

func TestPushSendUneErreurDEnvoiNArretePasLesAutres(t *testing.T) {
	envois := installerTransportPushDeTest(t, nil)
	appelsAvantErreur := 0
	pushTransport = func(abonnement pushSubscription, corps []byte) (int, error) {
		appelsAvantErreur++
		if appelsAvantErreur == 1 {
			return 0, errors.New("réseau coupé")
		}
		verrouEnvoisPush.Lock()
		*envois = append(*envois, envoiPushEnregistre{abonnement.Endpoint, corps})
		verrouEnvoisPush.Unlock()
		return 201, nil
	}
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/a", Asking: true})
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/b", Asking: true})

	pushDispatch(pushEvent{HomeID: mainHomeID, SessionID: "s1", Kind: "asking"})

	if len(*envois) != 1 {
		t.Fatalf("%d envois réussis au lieu de 1", len(*envois))
	}
}

func TestPushSendCorpsContientTitreLocaliseTagEtUrl(t *testing.T) {
	envois := installerTransportPushDeTest(t, nil)
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/a", Lang: "fr", Asking: true})

	pushDispatch(pushEvent{HomeID: mainHomeID, SessionID: "s 1", Title: "Mon titre", Kind: "asking"})

	var corpsDecode struct{ Title, Body, Tag, URL string }
	if erreur := json.Unmarshal((*envois)[0].corps, &corpsDecode); erreur != nil {
		t.Fatal(erreur)
	}
	titreAttendu := tr("fr", "srv.push.asking", nil)
	if corpsDecode.Title != titreAttendu || corpsDecode.Body != "Mon titre" || corpsDecode.Tag != "s 1" || corpsDecode.URL != "/?s=s%201" {
		t.Fatalf("corps inattendu : %+v", corpsDecode)
	}
}

func TestPushSendCorpsRepliSurLIdentifiantQuandLeTitreEstVide(t *testing.T) {
	envois := installerTransportPushDeTest(t, nil)
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/a", Waiting: true})

	pushDispatch(pushEvent{HomeID: mainHomeID, SessionID: "abc", Kind: "waiting"})

	var corpsDecode struct{ Body string }
	_ = json.Unmarshal((*envois)[0].corps, &corpsDecode)
	if corpsDecode.Body != "abc" {
		t.Fatalf("body = %q", corpsDecode.Body)
	}
}

func TestPushSendUserMayReadAutoriseToutSansComptes(t *testing.T) {
	fonctionInitiale := authStateFn
	t.Cleanup(func() { authStateFn = fonctionInitiale })
	authStateFn = func() (*authStore, bool, error) { return nil, false, nil }

	if !pushUserMayRead(0, mainHomeID) {
		t.Fatal("sans comptes, tout home est permis")
	}
}

func TestPushSendUserMayReadRefuseSiLaBaseDesComptesEstEnErreur(t *testing.T) {
	fonctionInitiale := authStateFn
	t.Cleanup(func() { authStateFn = fonctionInitiale })
	authStateFn = func() (*authStore, bool, error) { return nil, false, errors.New("base illisible") }

	if pushUserMayRead(1, mainHomeID) {
		t.Fatal("une base inutilisable ne doit jamais ouvrir l'envoi")
	}
}

func TestPushSendNeSuitJamaisUneRedirection(t *testing.T) {
	cibleInterne := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("la redirection a été suivie")
	}))
	defer cibleInterne.Close()
	redirecteur := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		http.Redirect(responseWriter, request, cibleInterne.URL, http.StatusFound)
	}))
	defer redirecteur.Close()

	reponse, erreur := pushHTTPClient.Get(redirecteur.URL)

	if erreur != nil {
		t.Fatal(erreur)
	}
	reponse.Body.Close()
	if reponse.StatusCode != http.StatusFound {
		t.Fatalf("statut %d au lieu de 302", reponse.StatusCode)
	}
}

func TestPushSendStatusFailedIgnoreLesSuccesEtLesAbonnementsExpires(t *testing.T) {
	casDeTest := map[int]bool{201: false, 200: false, 404: false, 410: false, 400: true, 401: true, 403: true, 413: true, 429: true, 302: true, 500: true}
	for statut, attendu := range casDeTest {
		if pushStatusFailed(statut) != attendu {
			t.Errorf("statut %d : obtenu %v, attendu %v", statut, !attendu, attendu)
		}
	}
}

func TestPushSendTitreLocaliseEnAnglaisPourUneLangueAutre(t *testing.T) {
	envois := installerTransportPushDeTest(t, nil)
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/a", Lang: "other", Asking: true})

	pushDispatch(pushEvent{HomeID: mainHomeID, SessionID: "s1", Kind: "asking"})

	var corpsDecode struct{ Title string }
	_ = json.Unmarshal((*envois)[0].corps, &corpsDecode)
	if corpsDecode.Title != tr("en", "srv.push.asking", nil) {
		t.Fatalf("titre = %q", corpsDecode.Title)
	}
}

// Un compte désactivé ne reçoit plus de notification, même si ses droits sur le home sont restés en place :
// la désactivation ne supprime pas ses abonnements.
func TestPushSendUserMayReadRefuseUnCompteDesactive(t *testing.T) {
	magasinDesComptes := testStore(t)
	identifiantCompte, ajoutErr := magasinDesComptes.AddUser("alice", "un mot de passe assez long", false)
	if ajoutErr != nil {
		t.Fatal(ajoutErr)
	}
	if grantErr := magasinDesComptes.Grant(identifiantCompte, mainHomeID, RightSessionsRead); grantErr != nil {
		t.Fatal(grantErr)
	}
	fonctionInitiale := authStateFn
	t.Cleanup(func() { authStateFn = fonctionInitiale })
	authStateFn = func() (*authStore, bool, error) { return magasinDesComptes, true, nil }

	if !pushUserMayRead(identifiantCompte, mainHomeID) {
		t.Fatal("un compte actif avec sessions_read doit recevoir l'envoi")
	}
	if disableErr := magasinDesComptes.SetDisabled(identifiantCompte, true); disableErr != nil {
		t.Fatal(disableErr)
	}
	if pushUserMayRead(identifiantCompte, mainHomeID) {
		t.Fatal("un compte désactivé ne doit plus recevoir d'envoi")
	}
}
