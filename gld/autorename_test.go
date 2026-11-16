package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func userLine(text string) string {
	return fmt.Sprintf(`{"type":"user","message":{"role":"user","content":%q}}`+"\n", text)
}

func TestCountInputsIgnoresToolResultsAndCountsIncrementally(t *testing.T) {
	use(t.TempDir())
	transcriptPath := filepath.Join(cfgDir(), "projects", "-p", id1+".jsonl")
	toolResult := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}` + "\n"
	put(t, transcriptPath, userLine("un")+toolResult+userLine("deux"), old)
	if valeurObtenue := countInputs(transcriptPath); valeurObtenue != 2 {
		t.Fatalf("valeurAttendue 2, valeurObtenue %d", valeurObtenue)
	}
	put(t, transcriptPath, userLine("un")+toolResult+userLine("deux")+userLine("trois"), old)
	if valeurObtenue := countInputs(transcriptPath); valeurObtenue != 3 {
		t.Fatalf("valeurAttendue 3, valeurObtenue %d", valeurObtenue)
	}
}

func TestReviewAutoTitleDropsSessionRenamedByHand(t *testing.T) {
	use(t.TempDir())
	transcriptPath := filepath.Join(cfgDir(), "projects", "-p", id1+".jsonl")
	put(t, transcriptPath, userLine("un")+`{"type":"custom-title","customTitle":"Titre manuel","sessionId":"`+id1+`"}`+"\n", old)
	updateAutoTitles(func(titles map[string]AutoTitle) { titles[id1] = AutoTitle{Title: "Titre auto", Inputs: 0} })

	reviewAutoTitle(id1, transcriptPath, AutoTitle{Title: "Titre auto"})

	groupsMu.Lock()
	suivi := loadStore().AutoTitles
	groupsMu.Unlock()
	if _, stillTracked := suivi[id1]; stillTracked {
		t.Fatal("un titre posé à la main doit sortir la session du suivi")
	}
}

func TestReviewAutoTitleResendsRenameEveryFiveInputs(t *testing.T) {
	use(t.TempDir())
	transcriptPath := filepath.Join(cfgDir(), "projects", "-p", id1+".jsonl")
	content := `{"type":"custom-title","customTitle":"Titre auto","sessionId":"` + id1 + `"}` + "\n" + strings.Repeat(userLine("msg"), autoRenameEveryInputs)
	put(t, transcriptPath, content, old)
	previousOpenFn, previousSendFn := openFn, sendFn
	sent := make(chan string, 1)
	openFn = func() map[string]bool { return map[string]bool{id1: true} }
	sendFn = func(_, text string) error { sent <- text; return nil }
	t.Cleanup(func() { openFn, sendFn = previousOpenFn, previousSendFn })
	previousWaiting := waiting
	waiting = func() []string { return []string{id1} }
	t.Cleanup(func() { waiting = previousWaiting })

	reviewAutoTitle(id1, transcriptPath, AutoTitle{Title: "Titre auto", Inputs: 0})

	if valeurObtenue := <-sent; valeurObtenue != "/rename" {
		t.Fatalf("valeurAttendue /rename, valeurObtenue %q", valeurObtenue)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) { // l'enregistrement du suivi suit l'envoi : on l'attend avant de rendre le dossier temporaire
		groupsMu.Lock()
		isRecorded := loadStore().AutoTitles[id1].Pending
		groupsMu.Unlock()
		if isRecorded {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("renvoi non enregistré dans le suivi")
}
