package main

import "testing"

func TestDropCtrlMouseReports(t *testing.T) {
	cases := []struct {
		name           string
		input          string
		valeurAttendue string
	}{
		{"clic simple conservé", "\x1b[<0;10;5M", "\x1b[<0;10;5M"},
		{"ctrl+clic retiré", "\x1b[<16;10;5M", ""},
		{"ctrl+relâché retiré", "\x1b[<16;10;5m", ""},
		{"ctrl+molette retiré", "\x1b[<80;10;5M", ""},
		{"shift+clic conservé", "\x1b[<4;10;5M", "\x1b[<4;10;5M"},
		{"molette conservée", "\x1b[<64;10;5M", "\x1b[<64;10;5M"},
		{"frappes autour conservées", "ab\x1b[<16;1;1Mcd", "abcd"},
		{"texte sans souris", "ls -la\r", "ls -la\r"},
		{"nombre démesuré conservé", "\x1b[<99999999999;1;1M", "\x1b[<99999999999;1;1M"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			valeurObtenue := string(dropCtrlMouseReports([]byte(testCase.input)))
			if valeurObtenue != testCase.valeurAttendue {
				t.Fatalf("valeurObtenue %q, valeurAttendue %q", valeurObtenue, testCase.valeurAttendue)
			}
		})
	}
}
