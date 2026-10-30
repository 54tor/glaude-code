package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTLSPair(t *testing.T) {
	for _, c := range []struct {
		cert, key string
		on, bad   bool
	}{{"", "", false, false}, {"c.pem", "k.pem", true, false}, {"c.pem", "", false, true}, {"", "k.pem", false, true}} {
		on, err := tlsPair(c.cert, c.key)
		if on != c.on || (err != nil) != c.bad {
			t.Fatalf("tlsPair(%q, %q) = %v, %v", c.cert, c.key, on, err)
		}
	}
}

func TestURLsForScheme(t *testing.T) {
	for _, u := range urlsFor("https", "192.0.2.1", "8799", "") {
		if !strings.HasPrefix(u, "https://192.0.2.1:8799/") {
			t.Fatalf("adresse : %q", u)
		}
	}
}

// Le cookie du jeton ne doit pas repartir en HTTP clair quand la page est servie en HTTPS (TLS direct ou proxy de confiance).
func TestTokenCookieSecure(t *testing.T) {
	h := withToken("0123456789abcdef0123", http.NotFoundHandler())
	for _, c := range []struct {
		tls  bool
		want bool
	}{{false, false}, {true, true}} {
		r := httptest.NewRequest("GET", "/?token=0123456789abcdef0123", nil)
		if c.tls {
			r.TLS = &tls.ConnectionState{}
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		ck := w.Result().Cookies()
		if len(ck) != 1 || ck[0].Secure != c.want {
			t.Fatalf("TLS %v : cookies %v, Secure attendu %v", c.tls, ck, c.want)
		}
	}
}

// Le navigateur charge le manifeste (et iOS l'icône d'écran d'accueil) sans cookie : ces fichiers publics passent sans jeton,
// le reste non.
func TestPublicAssetsWithoutToken(t *testing.T) {
	h := withToken("0123456789abcdef0123", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for path, want := range map[string]int{
		"/manifest.webmanifest": 200, "/icons/apple-touch-icon.png": 200, "/favicon.ico": 200, "/apple-touch-icon.png": 200, "/apple-touch-icon-precomposed.png": 200,
		"/v1/state": 401, "/x": 401, "/icons/../v1/state": 401, "/sw.js": 401, "/assets/app.js": 401,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("%s : %d, attendu %d", path, w.Code, want)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/manifest.webmanifest", nil))
	if w.Code != 401 {
		t.Errorf("POST sur un fichier public : %d", w.Code)
	}
	// iOS lit la racine sans Accept text/html pour l'icône d'écran d'accueil : il lui faut la page du jeton, pas un 401.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `rel="apple-touch-icon"`) {
		t.Errorf("GET / sans jeton : %d, page du jeton attendue", w.Code)
	}
}
