package main

import (
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
