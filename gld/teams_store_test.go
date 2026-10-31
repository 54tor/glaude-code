package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEditBuiltinMakesCopy(t *testing.T) {
	root := t.TempDir()
	agent, err := saveAgent(root, Agent{ID: "backend", Name: "Backend bis", Role: "r", Access: "write", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if agent.ID == "backend" || agent.Builtin || agent.Base != "backend" || len(agent.ID) != 10 || agent.ID[:2] != "x-" {
		t.Fatalf("copie = %+v", agent)
	}
	catalog, _ := loadCatalog(root)
	var orig Agent
	for _, agent := range builtinCatalog().Agents {
		if agent.ID == "backend" {
			orig = agent
		}
	}
	found := false
	for _, localAgent := range catalog.Agents {
		if localAgent.ID == "backend" && localAgent != orig {
			t.Fatal("fourni modifié")
		}
		found = found || localAgent.ID == agent.ID
	}
	if !found {
		t.Fatal("copie absente")
	}
	agent.Role = "autre"
	anotherAgent, err := saveAgent(root, agent)
	if err != nil || anotherAgent.ID != agent.ID || anotherAgent.Role != "autre" {
		t.Fatalf("maj en place: %+v %v", anotherAgent, err)
	}
}

func TestRemoveBuiltinRefused(t *testing.T) {
	root := t.TempDir()
	if !isMsg(removeAgent(root, "backend"), "srv.teams_store.builtinRefuse") || !isMsg(removeTeam(root, "feature"), "srv.teams_store.builtinRefuse") {
		t.Fatal("fourni supprimable")
	}
	if !isMsg(removeAgent(root, "x-nope"), "srv.teams_store.idInconnu") {
		t.Fatal("id absent accepté")
	}
}

func TestRemoveAgentUsedByTeam(t *testing.T) {
	root := t.TempDir()
	agent, _ := saveAgent(root, Agent{Name: "Zed", Role: "r", Access: "read"})
	tm, err := saveTeam(root, Team{Name: "Mon équipe", Members: []string{agent.ID, "backend"}})
	if err != nil {
		t.Fatal(err)
	}
	err = removeAgent(root, agent.ID)
	if !isMsg(err, "srv.teams_store.agentUtilise") {
		t.Fatalf("err = %v", err)
	}
	if err := removeTeam(root, tm.ID); err != nil {
		t.Fatal(err)
	}
	if err := removeAgent(root, agent.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogSurvivesUpdate(t *testing.T) {
	root := t.TempDir()
	agent, _ := saveAgent(root, Agent{Name: "Zed", Role: "r", Access: "read"})
	catalog, err := loadCatalog(root)
	if err != nil || len(catalog.Agents) != len(builtinCatalog().Agents)+1 || catalog.Agents[len(catalog.Agents)-1].ID != agent.ID {
		t.Fatalf("catalogue: %v", err)
	}
	if catalog.Agents[0].ID != "chef" {
		t.Fatal("fournis d'abord")
	}
}

func TestStoreConcurrent(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for _, name := range []string{"A1", "B2", "C3", "D4", "E5", "F6"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := saveAgent(root, Agent{Name: name, Role: "r", Access: "read"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	catalog, _ := loadCatalog(root)
	if len(catalog.Agents) != len(builtinCatalog().Agents)+6 {
		t.Fatalf("agents = %d", len(catalog.Agents))
	}
}

func TestSaveAgentRejectsBadAccess(t *testing.T) {
	root := t.TempDir()
	for _, acc := range []string{"lead", "", "admin", "READ"} {
		if _, err := saveAgent(root, Agent{Name: "Q", Role: "r", Access: acc}); !isMsg(err, "srv.teams_store.accesInvalide") {
			t.Fatalf("%q: %v", acc, err)
		}
	}
	if _, err := saveAgent(root, Agent{Name: "DÉVELOPPEMENT BACKEND", Role: "r", Access: "read"}); !isMsg(err, "srv.teams_store.doublon") {
		t.Fatalf("doublon: %v", err)
	}
	if _, err := saveAgent(root, Agent{Name: "", Role: "r", Access: "read"}); !isMsg(err, "srv.teams_store.nomVide") {
		t.Fatal("nom vide")
	}
	if _, err := saveAgent(root, Agent{ID: "x-inconnu", Name: "N", Role: "r", Access: "read"}); !isMsg(err, "srv.teams_store.idInconnu") {
		t.Fatal("id inconnu")
	}
}

func TestSaveTeamRejectsUnknownMember(t *testing.T) {
	root := t.TempDir()
	cases := map[string]Team{
		"srv.teams_store.membreInconnu": {Name: "T", Members: []string{"nope"}},
		"srv.teams_store.membreVide":    {Name: "T"},
		"srv.teams_store.membreDoublon": {Name: "T", Members: []string{"backend", "backend"}},
		"srv.teams_store.nomVide":       {Members: []string{"backend"}},
	}
	for key, tt := range cases {
		if _, err := saveTeam(root, tt); !isMsg(err, key) {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if _, err := saveTeam(root, Team{Name: "T", Members: []string{"chef"}}); !isMsg(err, "srv.teams_store.membreInconnu") {
		t.Fatal("chef accepté comme membre")
	}
	team, err := saveTeam(root, Team{ID: "feature", Name: "F2", Members: []string{"backend"}})
	if err != nil || team.Builtin || team.Base != "feature" {
		t.Fatalf("copie team: %+v %v", team, err)
	}
}

func TestStoreCorruptFileKept(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gld-teams.json")
	os.WriteFile(path, []byte("{pas du json"), 0o600)
	if _, err := loadCatalog(root); !isMsg(err, "srv.teams_store.fichierIllisible") {
		t.Fatalf("load: %v", err)
	}
	if _, err := saveAgent(root, Agent{Name: "Z", Role: "r", Access: "read"}); err == nil {
		t.Fatal("écrasement")
	}
	if data, _ := os.ReadFile(path); string(data) != "{pas du json" {
		t.Fatal("fichier modifié")
	}
}

func TestSaveAgentRejectsBadModel(t *testing.T) {
	root := t.TempDir()
	for _, message := range []string{"a b", "x\ny: z", strings.Repeat("a", 65), "é"} {
		if _, err := saveAgent(root, Agent{Name: "Q", Role: "r", Access: "read", Model: message}); !isMsg(err, "srv.teams_store.modeleInvalide") {
			t.Fatalf("%q: %v", message, err)
		}
	}
	if _, err := saveAgent(root, Agent{Name: "Q", Role: "r", Access: "read", Model: "claude-sonnet-4.5"}); err != nil {
		t.Fatal(err)
	}
}
