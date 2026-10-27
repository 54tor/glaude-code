package main

import "testing"

func TestBuiltinCatalogue(t *testing.T) {
	c := builtinCatalog()
	if len(c.Agents) != 21 || len(c.Teams) != 11 {
		t.Fatalf("%d agents, %d teams", len(c.Agents), len(c.Teams))
	}
	ids := map[string]bool{}
	leads := 0
	for _, a := range c.Agents {
		ids[a.ID] = true
		if a.Access == "lead" {
			leads++
			if a.ID != "atlas" {
				t.Errorf("lead inattendu : %s", a.ID)
			}
		}
		if !a.Builtin || a.Prompt == "" || a.Name == "" || a.Role == "" {
			t.Errorf("agent incomplet : %s", a.ID)
		}
	}
	if leads != 1 {
		t.Fatalf("%d leads", leads)
	}
	for _, tm := range c.Teams {
		if !tm.Builtin || len(tm.Members) == 0 {
			t.Errorf("team incomplète : %s", tm.ID)
		}
		for _, m := range tm.Members {
			if !ids[m] || m == "atlas" {
				t.Errorf("team %s : membre %q invalide", tm.ID, m)
			}
		}
	}
}

func agentIDs(as []Agent) []string {
	o := []string{}
	for _, a := range as {
		o = append(o, a.ID)
	}
	return o
}

func TestComposeDedup(t *testing.T) {
	lead, mates, tooMany, err := composeTeams(builtinCatalog(), []string{"feature", "bugfix", "revue"})
	if err != nil || lead.ID != "atlas" {
		t.Fatal(lead.ID, err)
	}
	want := []string{"vauban", "ada", "iris", "hugo", "marius", "eclaireur", "perrine", "cerbere"}
	got := agentIDs(mates)
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
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
	c := builtinCatalog()
	c.Agents = c.Agents[1:] // sans Atlas
	if _, _, _, err := composeTeams(c, []string{"feature"}); !isMsg(err, "srv.teams_catalog.chefAbsent") {
		t.Fatalf("err = %v", err)
	}
}
