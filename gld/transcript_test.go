package main

import (
	"strings"
	"testing"
)

func TestLineMsgsGardeLesReponsesAskUserQuestionOutilsMasques(t *testing.T) {
	ligneResultat := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"User has answered your questions: \"Quel choix ?\"=\"A\"."}]}}`)

	messagesObtenus := lineMsgs(ligneResultat, false)

	if len(messagesObtenus) != 1 || !strings.Contains(messagesObtenus[0].T, "Quel choix ?") {
		t.Fatalf("réponse AskUserQuestion absente outils masqués : %+v", messagesObtenus)
	}
}

func TestLineMsgsGardeLesReponsesAskUserQuestionNouvelleFormulation(t *testing.T) {
	ligneResultat := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"Your questions have been answered: \"Quel choix ?\"=\"A\". You can now continue with these answers."}]}}`)

	messagesObtenus := lineMsgs(ligneResultat, false)

	if len(messagesObtenus) != 1 || !strings.Contains(messagesObtenus[0].T, "Quel choix ?") {
		t.Fatalf("réponse AskUserQuestion (nouvelle formulation) absente outils masqués : %+v", messagesObtenus)
	}
}

func TestLineMsgsAfficheLesReponsesAskUserQuestionLisiblement(t *testing.T) {
	ligneResultat := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"The user answered: \"Quel choix ?\"=\"A\". Read the answers carefully."}]}}`)

	messagesObtenus := lineMsgs(ligneResultat, false)

	valeurAttendue := `[{"q":"Quel choix ?","a":"A"}]`
	if len(messagesObtenus) != 1 || messagesObtenus[0].R != "ask" || messagesObtenus[0].T != valeurAttendue {
		t.Fatalf("obtenu %+v, attendu %q", messagesObtenus, valeurAttendue)
	}
}
