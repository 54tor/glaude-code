package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
)

// Chaque chemin que le routeur de l'interface reconnaît (ui/src/lib/route.ts) doit renvoyer la page : sinon un rechargement ou un
// lien direct tombe sur un 404.
func TestPageRoutesMatchUI(t *testing.T) {
	src, err := os.ReadFile("ui/src/lib/route.ts")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	routesStatic(mux)
	for _, values := range seen(t, regexp.MustCompile(`(?:pathname === |pathname\.startsWith\(|isSectionOrBelow\()"(/[^"]*)"`).FindAllStringSubmatch(string(src), -1)) {
		path := values[1]
		if path[len(path)-1] == '/' {
			path += "x"
		}
		_, pat := mux.Handler(httptest.NewRequest("GET", path, nil))
		if pat == "" || pat == "GET /{$}" {
			t.Errorf("%s : pas servi par l'interface", path)
		}
	}
}

func seen(t *testing.T, valuesList [][]string) [][]string {
	if len(valuesList) < 10 {
		t.Fatalf("route.ts : %d chemins reconnus, l'expression ne suit plus le fichier", len(valuesList))
	}
	return valuesList
}

// Un dossier de ressources ne se liste pas ; ses fichiers restent servis.
func TestStaticNoDirListing(t *testing.T) {
	mux := http.NewServeMux()
	routesStatic(mux)
	get := func(path string) int {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		return recorder.Code
	}
	for _, path := range []string{"/assets/", "/icons/"} {
		if counter := get(path); counter != http.StatusNotFound {
			t.Errorf("%s : %d", path, counter)
		}
	}
	if counter := get("/icons/icon-192.png"); counter != http.StatusOK {
		t.Errorf("fichier d'icône : %d", counter)
	}
}
