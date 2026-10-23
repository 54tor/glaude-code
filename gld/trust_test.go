package main

import (
	"strings"
	"testing"
)

func TestTrustPanes(t *testing.T) {
	old := tmuxRun
	defer func() { tmuxRun = old }()
	tmuxRun = func(_ string, a ...string) (string, error) {
		switch a[0] {
		case "list-panes":
			return "%1\tclaude\t/home/u/neuf\n%2\tclaude\t/home/u/autre\n%3\tclaude\trelatif\n%4\tless\t/home/u/code\n", nil
		case "capture-pane":
			if a[3] == "%1" || a[3] == "%3" || a[3] == "%4" {
				return " Do you trust the files in this folder?\n 1. Yes, proceed\n 2. No, exit\n", nil
			}
			return "$ ls\n", nil
		}
		return "", nil
	}
	ps := trustPanes()
	if len(ps) != 1 || ps[0].dir != "/home/u/neuf" || ps[0].pane != "%1" {
		t.Fatalf("%+v", ps)
	}
}

func TestTrustKeys(t *testing.T) {
	neuf := " Quick safety check: Is this a project you created or one you trust?\n ❯ No, exit\n   Yes, I trust this folder\n Enter to confirm · Esc to cancel"
	ancien := " Do you trust the files in this folder?\n ❯ 1. Yes, proceed\n   2. No, exit\n"
	for _, c := range []struct {
		scr  string
		yes  bool
		want string
	}{
		{neuf, true, "Down Enter"},
		{neuf, false, "Enter"},
		{ancien, true, "Enter"},
		{ancien, false, "Down Enter"},
		{"rien à voir", true, ""},
	} {
		if got := strings.Join(trustKeys(c.scr, c.yes), " "); got != c.want {
			t.Errorf("yes=%v : %q, attendu %q", c.yes, got, c.want)
		}
	}
}
