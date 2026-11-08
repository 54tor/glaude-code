package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

const (
	leakHome       = "/home/bobby"
	leakProjectDir = "/home/bobby/Projets/app"
)

var leakedHostStrings = []string{"bobby", "Jean Dupont", "jean.dupont", "/home/", "Projets/app", "/data/backups"}

func preparerIdentiteHoteDeTest(t *testing.T) {
	t.Setenv("HOME", leakHome)
	previousLoader := identityLoadTerms
	identityLoadTerms = func() []string {
		return expandIdentityTerms([]string{"bobby", "Jean Dupont", "jean.dupont@example.com"})
	}
	identityMu.Lock()
	identityCached = nil
	identityMu.Unlock()
	t.Cleanup(func() {
		identityLoadTerms = previousLoader
		identityMu.Lock()
		identityCached = nil
		identityMu.Unlock()
	})
}

func ajouterLigneHoteAuTranscript(t *testing.T, ligne string) {
	t.Helper()
	cheminTranscript, trouve := transcript("-p", id1)
	if !trouve {
		t.Fatal("transcript introuvable")
	}
	fichierTranscript, err := os.OpenFile(cheminTranscript, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	fichierTranscript.WriteString(ligne)
	fichierTranscript.Close()
}

func verifierAucuneFuiteHote(t *testing.T, requestPath, corps string) {
	t.Helper()
	for _, chaineHote := range leakedHostStrings {
		if strings.Contains(strings.ToLower(corps), strings.ToLower(chaineHote)) {
			t.Errorf("%s : %q visible pour un invité : %s", requestPath, chaineHote, corps)
		}
	}
}

func TestGuestNeVoitAucuneIdentiteDansLesOutilsDepliés(t *testing.T) {
	preparerIdentiteHoteDeTest(t)
	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	cheminTranscript, _ := transcript("-p", id1)
	informationsAvant, err := os.Stat(cheminTranscript)
	if err != nil {
		t.Fatal(err)
	}
	positionDebut := informationsAvant.Size()
	ajouterLigneHoteAuTranscript(t, `{"type":"assistant","cwd":"`+leakProjectDir+`","timestamp":"2026-01-01T00:00:02Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"cat `+leakProjectDir+`/src/a.go /data/backups/db.sql ; whoami # bobby Jean Dupont jean.dupont@example.com"}}]}}`+"\n")
	informationsApres, _ := os.Stat(cheminTranscript)
	cheminOutils := "/v1/sessions/" + id1 + "/conversation/tools?from=" + strconv.FormatInt(positionDebut, 10) + "&until=" + strconv.FormatInt(informationsApres.Size(), 10)

	reponse := gReq{method: "GET", path: cheminOutils, cookie: content}.do(handler)
	if reponse.Code != 200 {
		t.Fatalf("%s : %d %s", cheminOutils, reponse.Code, reponse.Body)
	}
	corps := reponse.Body.String()
	verifierAucuneFuiteHote(t, cheminOutils, corps)
	if !strings.Contains(corps, "…/a.go") {
		t.Errorf("commande d'outil : %q attendu absent de %s", "…/a.go", corps)
	}
}

func TestGuestNeVoitAucuneIdentiteNiChemin(t *testing.T) {
	preparerIdentiteHoteDeTest(t)

	handler := guestWeb(t, nil, modeTok)
	content, _ := guestIn(t, handler)
	transcriptPath, found := transcript("-p", id1)
	if !found {
		t.Fatal("transcript introuvable")
	}
	hostLine := `{"type":"user","cwd":"` + leakProjectDir + `","timestamp":"2026-01-01T00:00:01Z","message":{"content":"` +
		`lis ` + leakProjectDir + `/src/a.go et /data/backups/db.sql ; whoami -> bobby ; auteur Jean Dupont jean.dupont@example.com"}}` + "\n"
	transcriptFile, err := os.OpenFile(transcriptPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	transcriptFile.WriteString(hostLine)
	transcriptFile.Close()

	for _, requestPath := range []string{
		"/v1/sessions/" + id1 + "/conversation",
		"/v1/sessions/" + id1 + "/live",
		"/v1/guest/me",
	} {
		recorder := gReq{method: "GET", path: requestPath, cookie: content}.do(handler)
		if recorder.Code != 200 {
			t.Fatalf("%s : %d", requestPath, recorder.Code)
		}
		for _, leaked := range leakedHostStrings {
			if strings.Contains(strings.ToLower(recorder.Body.String()), strings.ToLower(leaked)) {
				t.Errorf("%s : %q visible pour un invité : %s", requestPath, leaked, recorder.Body.String())
			}
		}
	}
	conversation := gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: content}.do(handler).Body.String()
	for _, expectedPart := range []string{"…/a.go", "…/db.sql"} {
		if !strings.Contains(conversation, expectedPart) {
			t.Errorf("conversation : %q attendu absent de %s", expectedPart, conversation)
		}
	}
}

const secretEnfantDeTest = "secret-enfant-de-test"

// Côté enfant : la passerelle relaie la requête avec X-Gld-Guest ; le masquage de guestServe doit s'y appliquer.
func TestGuestRelayeVersUnEnfantNeVoitAucuneIdentiteNiChemin(t *testing.T) {
	preparerIdentiteHoteDeTest(t)
	handler := guestWeb(t, nil, modeTok)
	cookieInvite, _ := guestIn(t, handler)
	_, secretInvite := guestSplit(cookieInvite)
	ajouterLigneHoteAuTranscript(t, `{"type":"user","cwd":"`+leakProjectDir+`","timestamp":"2026-01-01T00:00:01Z","message":{"content":"`+
		`lis `+leakProjectDir+`/src/a.go et /data/backups/db.sql ; whoami -> bobby ; auteur Jean Dupont jean.dupont@example.com"}}`+"\n")

	ancienJetonEnfant := childToken
	childToken = secretEnfantDeTest
	t.Cleanup(func() { childToken = ancienJetonEnfant })

	cheminConversation := "/v1/sessions/" + id1 + "/conversation"
	requete := httptest.NewRequest("GET", cheminConversation, nil)
	requete.Host = testHost
	requete.Header.Set(childHeader, secretEnfantDeTest)
	requete.Header.Set(guestHeader, secretInvite)
	requete.Header.Set(shareHeader, hID1)
	reponse := httptest.NewRecorder()
	handler.ServeHTTP(reponse, requete)

	if reponse.Code != 200 {
		t.Fatalf("%s : %d %s", cheminConversation, reponse.Code, reponse.Body)
	}
	corps := reponse.Body.String()
	verifierAucuneFuiteHote(t, cheminConversation, corps)
	if !strings.Contains(corps, "…/a.go") {
		t.Errorf("conversation relayée : %q attendu absent de %s", "…/a.go", corps)
	}
}

// Côté passerelle : un cookie « <home>.<secret> » est relayé à l'enfant avec le secret, sans cookie, et une route hors liste blanche est refusée.
func TestGuestRelayPasserelleTransmetSecretSansCookie(t *testing.T) {
	handler := guestWeb(t, nil, modeTok)
	cookieInvite, csrfInvite := guestIn(t, handler)
	_, secretInvite := guestSplit(cookieInvite)
	addHomes(t, "Pro")

	ancienOuvertureDesPartages := guestOpenShares
	guestOpenShares = func(string) (*shareStore, error) { return shareSt, nil }
	t.Cleanup(func() { guestOpenShares = ancienOuvertureDesPartages })

	requetesRecues := make(chan *http.Request, 4)
	serveurEnfant := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		requetesRecues <- request
		responseWriter.Write([]byte("{}"))
	}))
	defer serveurEnfant.Close()
	urlEnfant, _ := url.Parse(serveurEnfant.URL)
	ancienDemarrage := homeStart
	t.Cleanup(func() { homeStart = ancienDemarrage; stopHomes() })
	homeStart = func(home Home) (*homeProc, error) {
		return &homeProc{dir: home.Dir, proxy: childProxy(urlEnfant, secretEnfantDeTest), done: make(chan struct{})}, nil
	}

	cookieRelaye := "Pro" + cookieInvite
	reponse := gReq{method: "GET", path: "/v1/sessions/" + id1 + "/conversation", cookie: cookieRelaye, csrf: csrfInvite}.do(handler)
	if reponse.Code != 200 {
		t.Fatalf("relais : %d %s", reponse.Code, reponse.Body)
	}
	requeteRecue := <-requetesRecues
	if requeteRecue.Header.Get(guestHeader) != secretInvite {
		t.Errorf("secret transmis à l'enfant : %q", requeteRecue.Header.Get(guestHeader))
	}
	if requeteRecue.Header.Get("Cookie") != "" {
		t.Errorf("cookie transmis à l'enfant : %q", requeteRecue.Header.Get("Cookie"))
	}
	if requeteRecue.Header.Get(childHeader) != secretEnfantDeTest {
		t.Errorf("jeton de la passerelle absent : %q", requeteRecue.Header.Get(childHeader))
	}

	reponseHorsListeBlanche := gReq{method: "GET", path: "/v1/state", cookie: cookieRelaye, csrf: csrfInvite}.do(handler)
	if reponseHorsListeBlanche.Code != 403 {
		t.Errorf("route hors liste blanche relayée : %d", reponseHorsListeBlanche.Code)
	}
	select {
	case requeteInattendue := <-requetesRecues:
		t.Errorf("l'enfant a reçu %s", requeteInattendue.URL.Path)
	default:
	}
}
