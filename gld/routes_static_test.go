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
	for _, m := range seen(t, regexp.MustCompile(`(?:pathname === |pathname\.startsWith\(|isSectionOrBelow\()"(/[^"]*)"`).FindAllStringSubmatch(string(src), -1)) {
		p := m[1]
		if p[len(p)-1] == '/' {
			p += "x"
		}
		_, pat := mux.Handler(httptest.NewRequest("GET", p, nil))
		if pat == "" || pat == "GET /{$}" {
			t.Errorf("%s : pas servi par l'interface", p)
		}
	}
}

func seen(t *testing.T, m [][]string) [][]string {
	if len(m) < 10 {
		t.Fatalf("route.ts : %d chemins reconnus, l'expression ne suit plus le fichier", len(m))
	}
	return m
}

// Un dossier de ressources ne se liste pas ; ses fichiers restent servis.
func TestStaticNoDirListing(t *testing.T) {
	mux := http.NewServeMux()
	routesStatic(mux)
	get := func(p string) int {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		return w.Code
	}
	for _, p := range []string{"/assets/", "/icons/"} {
		if c := get(p); c != http.StatusNotFound {
			t.Errorf("%s : %d", p, c)
		}
	}
	if c := get("/icons/icon-192.png"); c != http.StatusOK {
		t.Errorf("fichier d'icône : %d", c)
	}
}
