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
		erreur := pushSubscriptionUpsert(pushSubscription{Endpoint: fmt.Sprintf("https://push.example.com/%d", numeroAbonnement)})
		if erreur != nil {
			t.Fatal(erreur)
		}
	}

	erreurAuDela := pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/de-trop"})

	if erreurAuDela == nil {
		t.Fatal("le plafond n'est pas appliqué")
	}
}
