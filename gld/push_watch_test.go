package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestPushWatchEventsFor(t *testing.T) {
	titres := map[string]string{"s1": "Premier", "s2": "Second"}
	casDeTest := []struct {
		nom         string
		precedent   pushHomeState
		courant     pushHomeState
		premierTour bool
		attendu     []pushEvent
	}{
		{
			nom:         "le premier passage ne notifie rien",
			courant:     pushHomeState{Waiting: []string{"s1"}, Asking: []string{"s2"}, Titles: titres},
			premierTour: true,
			attendu:     nil,
		},
		{
			nom:     "une nouvelle question notifie asking",
			courant: pushHomeState{Waiting: []string{"s1"}, Asking: []string{"s1"}, Titles: titres},
			attendu: []pushEvent{{HomeID: "h", SessionID: "s1", Title: "Premier", Kind: "asking"}},
		},
		{
			nom:     "une nouvelle attente notifie waiting",
			courant: pushHomeState{Waiting: []string{"s2"}, Titles: titres},
			attendu: []pushEvent{{HomeID: "h", SessionID: "s2", Title: "Second", Kind: "waiting"}},
		},
		{
			nom:     "la question prime sur l'attente d'une même session",
			courant: pushHomeState{Waiting: []string{"s1"}, Asking: []string{"s1"}, Titles: titres},
			attendu: []pushEvent{{HomeID: "h", SessionID: "s1", Title: "Premier", Kind: "asking"}},
		},
		{
			nom:       "rien de nouveau ne notifie rien",
			precedent: pushHomeState{Waiting: []string{"s1"}, Asking: []string{"s1"}},
			courant:   pushHomeState{Waiting: []string{"s1"}, Asking: []string{"s1"}, Titles: titres},
			attendu:   nil,
		},
	}
	for _, casCourant := range casDeTest {
		t.Run(casCourant.nom, func(t *testing.T) {
			evenementsObtenus := pushEventsFor("h", casCourant.precedent, casCourant.courant, casCourant.premierTour)
			if !reflect.DeepEqual(evenementsObtenus, casCourant.attendu) {
				t.Fatalf("obtenu %+v, attendu %+v", evenementsObtenus, casCourant.attendu)
			}
		})
	}
}

func TestPushStateRefuseSansJetonEnfant(t *testing.T) {
	jetonInitial := childToken
	t.Cleanup(func() { childToken = jetonInitial })
	childToken = "jeton-enfant"
	mux := http.NewServeMux()
	routesPushState(mux)

	reponseSansJeton := httptest.NewRecorder()
	mux.ServeHTTP(reponseSansJeton, httptest.NewRequest("GET", "/v1/push-state", nil))
	requeteAvecJeton := httptest.NewRequest("GET", "/v1/push-state", nil)
	requeteAvecJeton.Header.Set(childHeader, "jeton-enfant")
	reponseAvecJeton := httptest.NewRecorder()
	mux.ServeHTTP(reponseAvecJeton, requeteAvecJeton)

	if reponseSansJeton.Code != http.StatusForbidden {
		t.Fatalf("sans jeton : %d au lieu de 403", reponseSansJeton.Code)
	}
	if reponseAvecJeton.Code != http.StatusOK {
		t.Fatalf("avec jeton : %d au lieu de 200", reponseAvecJeton.Code)
	}
}

func TestPushWatchNotifieSeulementLesNouveautesApresLePremierPassage(t *testing.T) {
	envois := installerTransportPushDeTest(t, nil)
	_ = pushSubscriptionUpsert(pushSubscription{Endpoint: "https://push.example.com/a", Waiting: true})
	var verrou sync.Mutex
	numeroDePassage := 0
	sourceInitiale := pushStatesSource
	t.Cleanup(func() { pushStatesSource = sourceInitiale })
	pushStatesSource = func() map[string]pushHomeState {
		verrou.Lock()
		defer verrou.Unlock()
		numeroDePassage++
		if numeroDePassage == 1 {
			return map[string]pushHomeState{mainHomeID: {Waiting: []string{"s1"}}}
		}
		return map[string]pushHomeState{mainHomeID: {Waiting: []string{"s1", "s2"}}}
	}

	annuler := withHomeCtx(t)
	termine := runLoop(func() { watchPush(5 * time.Millisecond) })
	delaiMaximal := time.Now().Add(loopWait)
	for len(*envois) == 0 && time.Now().Before(delaiMaximal) {
		time.Sleep(5 * time.Millisecond)
	}
	annuler()
	expectReturn(t, termine, "watchPush")

	if len(*envois) != 1 {
		t.Fatalf("%d envois au lieu de 1 (s1 vu au premier passage, seul s2 est nouveau)", len(*envois))
	}
}
