package main

import (
	"net/http/httptest"
	"testing"
)

// Un home enfant n'a pas de garde : il ne connaît les droits de l'appelant que par l'en-tête de sa passerelle, et seulement avec le jeton d'enfant.
func TestCallerHasChildHeader(t *testing.T) {
	old := childToken
	childToken = "tok-enfant"
	t.Cleanup(func() { childToken = old })
	mk := func(tok string, rights *string) bool {
		r := httptest.NewRequest("GET", "/v1/config/mcp", nil)
		if tok != "" {
			r.Header.Set(childHeader, tok)
		}
		if rights != nil {
			r.Header.Set(rightsHeader, *rights)
		}
		return callerHas(r, RightConfigWrite)
	}
	none, fs, cfg := "", "fs_read,mem_read", "mem_read,config_write"
	for name, c := range map[string]struct {
		tok    string
		rights *string
		want   bool
	}{
		"droits vides":      {"tok-enfant", &none, false},
		"sans config_write": {"tok-enfant", &fs, false},
		"avec config_write": {"tok-enfant", &cfg, true},
		"passerelle en mode ouvert (pas d'en-tête)": {"tok-enfant", nil, true},
		"en-tête sans jeton d'enfant":               {"", &cfg, false},
	} {
		if got := mk(c.tok, c.rights); got != c.want {
			t.Errorf("%s : got %v, want %v", name, got, c.want)
		}
	}
}
