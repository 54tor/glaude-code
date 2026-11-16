package main

import "testing"

func TestEncodeDirNameMatchesRegexpEncoding(t *testing.T) {
	for _, name := range []string{"Projets", "glaude-code", "a b.c_d", "été 2026", "日本語x", "", "A9z"} {
		valeurAttendue := nonAlnum.ReplaceAllString(name, "-")
		if valeurObtenue := encodeDirName(name); valeurObtenue != valeurAttendue {
			t.Errorf("%q : valeurAttendue %q, valeurObtenue %q", name, valeurAttendue, valeurObtenue)
		}
	}
}
