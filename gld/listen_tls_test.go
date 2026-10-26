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
