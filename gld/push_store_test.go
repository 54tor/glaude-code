package main

import (
	"fmt"
	"os"
	"testing"
)

func isolerDossierDeConfigurationPush(t *testing.T) {
	t.Helper()
	racineInitiale := root
	t.Cleanup(func() { root = racineInitiale })
	root = t.TempDir() + "/projects"
}

func TestPushStoreGenereEtReutiliseLesClesVapid(t *testing.T) {
	isolerDossierDeConfigurationPush(t)

	clePubliquePremiere, clePriveePremiere, erreurPremiere := pushVapidKeys()
	clePubliqueSeconde, clePriveeSeconde, erreurSeconde := pushVapidKeys()

	if erreurPremiere != nil || erreurSeconde != nil {
		t.Fatalf("erreurs inattendues : %v / %v", erreurPremiere, erreurSeconde)
	}
	if clePubliquePremiere == "" || clePriveePremiere == "" {
		t.Fatal("clés vides")
	}
	if clePubliquePremiere != clePubliqueSeconde || clePriveePremiere != clePriveeSeconde {
		t.Fatal("les clés doivent être stables d'un appel à l'autre")
	}
	informationsFichier, erreurStat := os.Stat(pushFile())
	if erreurStat != nil {
		t.Fatal(erreurStat)
	}
	if modeObtenu := informationsFichier.Mode().Perm(); modeObtenu != 0o600 {
		t.Fatalf("mode du fichier : %o au lieu de 600", modeObtenu)
	}
}

func TestPushStoreUpsertRemplaceUnAbonnementDeMemeEndpoint(t *testing.T) {
	isolerDossierDeConfigurationPush(t)
	endpoint := "https://push.example.com/a"

	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: endpoint, Asking: true})
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: endpoint, Waiting: true})

	abonnements, erreur := pushSubscriptionsLoad()
	if erreur != nil {
		t.Fatal(erreur)
	}
	if len(abonnements) != 1 {
		t.Fatalf("%d abonnements au lieu de 1", len(abonnements))
	}
	if abonnements[0].Asking || !abonnements[0].Waiting {
		t.Fatalf("le second upsert doit gagner : %+v", abonnements[0])
	}
}

func TestPushStoreDeleteRetireSeulementLEndpointVise(t *testing.T) {
	isolerDossierDeConfigurationPush(t)
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/a"})
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/b"})

	if erreur := pushSubscriptionDelete("https://push.example.com/a"); erreur != nil {
		t.Fatal(erreur)
	}

	_, trouveA, _ := pushSubscriptionFind("https://push.example.com/a")
	_, trouveB, _ := pushSubscriptionFind("https://push.example.com/b")
	if trouveA || !trouveB {
		t.Fatalf("A trouvé=%v, B trouvé=%v", trouveA, trouveB)
	}
}

func TestPushStoreRefuseDEcraserUnFichierIllisible(t *testing.T) {
	isolerDossierDeConfigurationPush(t)
	contenuCorrompu := []byte("{pas du json")
	if erreur := os.WriteFile(pushFile(), contenuCorrompu, 0o600); erreur != nil {
		t.Fatal(erreur)
	}

	_, erreurLecture := pushSubscriptionsLoad()
	erreurEcriture := pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/a"})

	if erreurLecture == nil || erreurEcriture == nil {
		t.Fatalf("erreurs attendues, obtenu : %v / %v", erreurLecture, erreurEcriture)
	}
	contenuApres, _ := os.ReadFile(pushFile())
	if string(contenuApres) != string(contenuCorrompu) {
		t.Fatalf("le fichier a été modifié : %q", contenuApres)
	}
}

func TestPushStoreRefuseAuDelaDuPlafond(t *testing.T) {
	isolerDossierDeConfigurationPush(t)
	for numeroAbonnement := 0; numeroAbonnement < pushSubscriptionsMax; numeroAbonnement++ {
		erreur := pushSubscriptionUpsert(pushSubscription{Endpoint: fmt.Sprintf("https://push.example.com/%d", numeroAbonnement), UserID: int64(numeroAbonnement)})
		if erreur != nil {
			t.Fatal(erreur)
		}
	}

	erreurAuDela := pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/de-trop", UserID: pushSubscriptionsMax + 1})

	if erreurAuDela == nil {
		t.Fatal("le plafond n'est pas appliqué")
	}
}

func TestPushStoreNeRegenerePasLesClesQuandLeFichierEstIllisible(t *testing.T) {
	isolerDossierDeConfigurationPush(t)
	if erreur := os.MkdirAll(pushFile(), 0o700); erreur != nil {
		t.Fatal(erreur)
	}

	_, _, erreurCles := pushVapidKeys()

	if erreurCles == nil {
		t.Fatal("un fichier présent mais illisible ne doit pas être traité comme absent")
	}
	informations, erreurStat := os.Stat(pushFile())
	if erreurStat != nil || !informations.IsDir() {
		t.Fatal("le chemin illisible a été remplacé")
	}
}

// Un compte ne peut pas occuper toute la table : au-delà de pushSubscriptionsPerUser, ses nouveaux abonnements sont refusés,
// ceux d'un autre compte restent acceptés.
func TestPushStoreRefuseAuDelaDuPlafondParCompte(t *testing.T) {
	isolerDossierDeConfigurationPush(t)
	const compteRempli, autreCompte = 7, 8
	for numeroAbonnement := 0; numeroAbonnement < pushSubscriptionsPerUser; numeroAbonnement++ {
		if erreur := pushSubscriptionUpsert(pushSubscription{Endpoint: fmt.Sprintf("https://push.example.com/%d", numeroAbonnement), UserID: compteRempli}); erreur != nil {
			t.Fatal(erreur)
		}
	}
	if erreur := pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/de-trop", UserID: compteRempli}); erreur == nil {
		t.Fatal("le plafond par compte n'est pas appliqué")
	}
	if erreur := pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/autre", UserID: autreCompte}); erreur != nil {
		t.Fatalf("un autre compte est bloqué : %v", erreur)
	}
}

// Les abonnements dont la session créatrice est fermée ne comptent plus dans le plafond : ils sont retirés au prochain abonnement
// du même compte.
func TestPushStoreRetireLesAbonnementsDesSessionsFermees(t *testing.T) {
	isolerDossierDeConfigurationPush(t)
	fonctionInitiale := pushSessionLive
	t.Cleanup(func() { pushSessionLive = fonctionInitiale })
	pushSessionLive = func(string) bool { return true }
	for numeroAbonnement := 0; numeroAbonnement < pushSubscriptionsPerUser; numeroAbonnement++ {
		if erreur := pushSubscriptionUpsert(pushSubscription{Endpoint: fmt.Sprintf("https://push.example.com/%d", numeroAbonnement), UserID: 7, SessionID: "session-fermee"}); erreur != nil {
			t.Fatal(erreur)
		}
	}
	pushSessionLive = func(sessionID string) bool { return sessionID != "session-fermee" }
	if erreur := pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/nouveau", UserID: 7, SessionID: "session-ouverte"}); erreur != nil {
		t.Fatalf("abonnement refusé alors que les anciens appartiennent à une session fermée : %v", erreur)
	}
	abonnementsRestants, _ := pushSubscriptionsLoad()
	if len(abonnementsRestants) != 1 || abonnementsRestants[0].SessionID != "session-ouverte" {
		t.Fatalf("abonnements restants : %+v", abonnementsRestants)
	}
}
