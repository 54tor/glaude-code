package main

import "testing"

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
