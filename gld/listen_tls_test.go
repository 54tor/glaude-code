package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTLSPair(t *testing.T) {
	for _, record := range []struct {
		cert, key string
		on, bad   bool
	}{{"", "", false, false}, {"c.pem", "k.pem", true, false}, {"c.pem", "", false, true}, {"", "k.pem", false, true}} {
		on, err := tlsPair(record.cert, record.key)
		if on != record.on || (err != nil) != record.bad {
			t.Fatalf("tlsPair(%q, %q) = %v, %v", record.cert, record.key, on, err)
		}
	}
}

func TestURLsForScheme(t *testing.T) {
	for _, rawURL := range urlsFor("https", "192.0.2.1", "8799", "") {
		if !strings.HasPrefix(rawURL, "https://192.0.2.1:8799/") {
			t.Fatalf("adresse : %q", rawURL)
		}
	}
}

// Le cookie du jeton ne doit pas repartir en HTTP clair quand la page est servie en HTTPS (TLS direct ou proxy de confiance).
func TestTokenCookieSecure(t *testing.T) {
	handler := withToken("0123456789abcdef0123", http.NotFoundHandler())
	for _, record := range []struct {
		tls  bool
		want bool
	}{{false, false}, {true, true}} {
		request := httptest.NewRequest("GET", "/?token=0123456789abcdef0123", nil)
		if record.tls {
			request.TLS = &tls.ConnectionState{}
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		ck := recorder.Result().Cookies()
		if len(ck) != 1 || ck[0].Secure != record.want {
			t.Fatalf("TLS %v : cookies %v, Secure attendu %v", record.tls, ck, record.want)
		}
	}
}

// Le navigateur charge le manifeste (et iOS l'icône d'écran d'accueil) sans cookie : ces fichiers publics passent sans jeton,
// le reste non.
func TestPublicAssetsWithoutToken(t *testing.T) {
	handler := withToken("0123456789abcdef0123", http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) { responseWriter.WriteHeader(200) }))
	for path, want := range map[string]int{
		"/site.webmanifest": 200, "/icons/apple-touch-icon.png": 200, "/favicon.ico": 200, "/apple-touch-icon.png": 200, "/apple-touch-icon-precomposed.png": 200, "/web-app-manifest-512x512.png": 200,
		"/v1/state": 401, "/x": 401, "/icons/../v1/state": 401, "/sw.js": 401, "/assets/app.js": 401,
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != want {
			t.Errorf("%s : %d, attendu %d", path, recorder.Code, want)
		}
	}
	localRecorder := httptest.NewRecorder()
	handler.ServeHTTP(localRecorder, httptest.NewRequest("HEAD", "/site.webmanifest", nil)) // iOS sonde le manifeste en HEAD
	if localRecorder.Code != 200 {
		t.Errorf("HEAD sur le manifeste : %d", localRecorder.Code)
	}
	localRecorder = httptest.NewRecorder()
	handler.ServeHTTP(localRecorder, httptest.NewRequest("POST", "/site.webmanifest", nil))
	if localRecorder.Code != 401 {
		t.Errorf("POST sur un fichier public : %d", localRecorder.Code)
	}
	// iOS lit la racine sans Accept text/html pour l'icône d'écran d'accueil : il lui faut la page du jeton, pas un 401.
	localRecorder = httptest.NewRecorder()
	handler.ServeHTTP(localRecorder, httptest.NewRequest("GET", "/", nil))
	if localRecorder.Code != 200 || !strings.Contains(localRecorder.Body.String(), `rel="apple-touch-icon"`) {
		t.Errorf("GET / sans jeton : %d, page du jeton attendue", localRecorder.Code)
	}
}
