package main

import (
	"strings"
	"testing"
)

// Le terminal redessine avec retard après la touche Bas : l'ancien écran est encore servi pendant deux relevés. La réponse « oui » doit
// attendre que le curseur arrive sur « Yes », puis valider.
func TestTrustRedessinLent(t *testing.T) {
	handler, _, _ := web(t)
	old := tmuxRun
	t.Cleanup(func() { tmuxRun = old })
	const before = "Is this a project you created or one you trust?\n ❯ No, exit\n   Yes, I trust this folder\n"
	const after = "Is this a project you created or one you trust?\n   No, exit\n ❯ Yes, I trust this folder\n"
	var sent []string
	moved, stale := false, 0
	tmuxRun = func(_ string, values ...string) (string, error) {
		switch values[0] {
		case "list-panes":
			return "%1\tclaude\t/home/u/neuf\n", nil
		case "capture-pane":
			if moved {
				if stale < 2 {
					stale++
					return before, nil
				}
				return after, nil
			}
			return before, nil
		case "send-keys":
			sent = append(sent, values[3])
			if values[3] == "Down" {
				moved = true
			}
		}
		return "", nil
	}
	v1ok(t, handler, "POST", "/v1/trust/answers", `{"dir":"/home/u/neuf","yes":true}`, "")
	if got := strings.Join(sent, " "); got != "Down Enter" {
		t.Fatalf("touches envoyées : %q", got)
	}
}
