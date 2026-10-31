package main

import (
	"strings"
	"testing"
)

func TestBuiltinCatalogue(t *testing.T) {
	catalog := builtinCatalog()
	if len(catalog.Agents) != 21 || len(catalog.Teams) != 11 {
		t.Fatalf("%d agents, %d teams", len(catalog.Agents), len(catalog.Teams))
	}
	ids := map[string]bool{}
	leads := 0
	for _, agent := range catalog.Agents {
		ids[agent.ID] = true
		if agent.Access == "lead" {
			leads++
			if agent.ID != "chef" {
				t.Errorf("lead inattendu : %s", agent.ID)
			}
		}
		if !agent.Builtin || agent.Prompt == "" || agent.Name == "" || agent.Role == "" {
			t.Errorf("agent incomplet : %s", agent.ID)
		}
	}
	if leads != 1 {
		t.Fatalf("%d leads", leads)
	}
	wantAgents := [][3]string{
		{"chef", "Chef de projet", "lead"}, {"architecte", "Architecte", "read"}, {"backend", "Développement backend", "write"},
		{"frontend", "Développement frontend", "write"}, {"testeur", "Testeur", "write"}, {"relecteur", "Relecteur", "read"},
		{"performance", "Performance", "read"}, {"secu-auth", "Sécurité authentification", "read"}, {"secu-injection", "Sécurité injections", "read"},
		{"secu-secrets", "Sécurité secrets", "read"}, {"secu-deps", "Sécurité dépendances", "read"}, {"enqueteur-1", "Enquêteur 1", "read"},
		{"enqueteur-2", "Enquêteur 2", "read"}, {"enqueteur-3", "Enquêteur 3", "read"}, {"explorateur", "Explorateur", "read"},
		{"refactoring", "Refactoring", "write"}, {"redacteur", "Rédaction", "write"}, {"ux", "UX et accessibilité", "read"},
		{"qa", "QA navigateur", "read"}, {"donnees", "Données et migrations", "read"}, {"devops", "DevOps", "write"},
	}
	for index, values := range wantAgents {
		agent := catalog.Agents[index]
		if agent.ID != values[0] || agent.Name != values[1] || agent.Access != values[2] {
			t.Errorf("agent %d : %s/%s/%s, attendu %v", index, agent.ID, agent.Name, agent.Access, values)
		}
	}
	wantTeams := map[string]string{
		"feature": "architecte backend frontend testeur relecteur", "bugfix": "explorateur backend testeur",
		"enquete": "enqueteur-1 enqueteur-2 enqueteur-3", "audit": "secu-auth secu-injection secu-secrets secu-deps",
		"revue": "relecteur performance secu-auth", "refacto": "explorateur refactoring testeur relecteur",
		"doc": "explorateur redacteur", "ux": "ux frontend qa", "migration": "donnees backend testeur",
		"prod": "devops testeur redacteur", "decouverte": "explorateur architecte redacteur",
	}
	for _, tm := range catalog.Teams {
		if strings.Join(tm.Members, " ") != wantTeams[tm.ID] {
			t.Errorf("team %s : %v", tm.ID, tm.Members)
		}
	}
	for _, tm := range catalog.Teams {
		if !tm.Builtin || len(tm.Members) == 0 {
			t.Errorf("team incomplète : %s", tm.ID)
		}
		for _, message := range tm.Members {
			if !ids[message] || message == "chef" {
				t.Errorf("team %s : membre %q invalide", tm.ID, message)
			}
		}
	}
}

func agentIDs(as []Agent) []string {
	values := []string{}
	for _, agent := range as {
		values = append(values, agent.ID)
	}
	return values
}

func TestComposeDedup(t *testing.T) {
	lead, mates, tooMany, err := composeTeams(builtinCatalog(), []string{"feature", "bugfix", "revue"})
	if err != nil || lead.ID != "chef" {
		t.Fatal(lead.ID, err)
	}
	want := []string{"architecte", "backend", "frontend", "testeur", "relecteur", "explorateur", "performance", "secu-auth"}
	got := agentIDs(mates)
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%v", got)
		}
	}
	if !tooMany {
		t.Error("tooMany attendu")
	}
}

func TestComposeTooMany(t *testing.T) {
	_, _, tooMany, err := composeTeams(builtinCatalog(), []string{"feature", "audit"})
	if err != nil || !tooMany {
		t.Fatal(tooMany, err)
	}
	_, _, tooMany, _ = composeTeams(builtinCatalog(), []string{"feature"})
	if tooMany {
		t.Error("5 coéquipiers ne dépassent pas")
	}
}

func TestComposeUnknownTeam(t *testing.T) {
	lead, mates, tooMany, err := composeTeams(builtinCatalog(), []string{"feature", "nope"})
	if err == nil || lead.ID != "" || mates != nil || tooMany {
		t.Fatal(lead, mates, tooMany, err)
	}
}

func TestComposeEmpty(t *testing.T) {
	lead, mates, tooMany, err := composeTeams(builtinCatalog(), nil)
	if err == nil || lead.ID != "" || mates != nil || tooMany {
		t.Fatal(lead, mates, tooMany, err)
	}
}

func TestComposeNoLead(t *testing.T) {
	catalog := builtinCatalog()
	catalog.Agents = catalog.Agents[1:] // sans le chef
	if _, _, _, err := composeTeams(catalog, []string{"feature"}); !isMsg(err, "srv.teams_catalog.chefAbsent") {
		t.Fatalf("err = %v", err)
	}
}
