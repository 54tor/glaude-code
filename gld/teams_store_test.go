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
	a, err := saveAgent(root, Agent{ID: "ada", Name: "Ada bis", Role: "r", Access: "write", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "ada" || a.Builtin || a.Base != "ada" || len(a.ID) != 10 || a.ID[:2] != "x-" {
		t.Fatalf("copie = %+v", a)
	}
	c, _ := loadCatalog(root)
	var orig Agent
	for _, x := range builtinCatalog().Agents {
		if x.ID == "ada" {
			orig = x
		}
	}
	found := false
	for _, x := range c.Agents {
		if x.ID == "ada" && x != orig {
			t.Fatal("fourni modifié")
		}
		found = found || x.ID == a.ID
	}
	if !found {
		t.Fatal("copie absente")
	}
	a.Role = "autre"
	b, err := saveAgent(root, a)
	if err != nil || b.ID != a.ID || b.Role != "autre" {
		t.Fatalf("maj en place: %+v %v", b, err)
	}
}

func TestRemoveBuiltinRefused(t *testing.T) {
	root := t.TempDir()
	if !isMsg(removeAgent(root, "ada"), "srv.teams_store.builtinRefuse") || !isMsg(removeTeam(root, "feature"), "srv.teams_store.builtinRefuse") {
		t.Fatal("fourni supprimable")
	}
	if !isMsg(removeAgent(root, "x-nope"), "srv.teams_store.idInconnu") {
		t.Fatal("id absent accepté")
	}
}

func TestRemoveAgentUsedByTeam(t *testing.T) {
	root := t.TempDir()
	a, _ := saveAgent(root, Agent{Name: "Zed", Role: "r", Access: "read"})
	tm, err := saveTeam(root, Team{Name: "Mon équipe", Members: []string{a.ID, "ada"}})
	if err != nil {
		t.Fatal(err)
	}
	err = removeAgent(root, a.ID)
	if !isMsg(err, "srv.teams_store.agentUtilise") {
		t.Fatalf("err = %v", err)
	}
	if err := removeTeam(root, tm.ID); err != nil {
		t.Fatal(err)
	}
	if err := removeAgent(root, a.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogSurvivesUpdate(t *testing.T) {
	root := t.TempDir()
	a, _ := saveAgent(root, Agent{Name: "Zed", Role: "r", Access: "read"})
	c, err := loadCatalog(root)
	if err != nil || len(c.Agents) != len(builtinCatalog().Agents)+1 || c.Agents[len(c.Agents)-1].ID != a.ID {
		t.Fatalf("catalogue: %v", err)
	}
	if c.Agents[0].ID != "atlas" {
		t.Fatal("fournis d'abord")
	}
}

func TestStoreConcurrent(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for _, n := range []string{"A1", "B2", "C3", "D4", "E5", "F6"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := saveAgent(root, Agent{Name: n, Role: "r", Access: "read"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	c, _ := loadCatalog(root)
	if len(c.Agents) != len(builtinCatalog().Agents)+6 {
		t.Fatalf("agents = %d", len(c.Agents))
	}
}

func TestSaveAgentRejectsBadAccess(t *testing.T) {
	root := t.TempDir()
	for _, acc := range []string{"lead", "", "admin", "READ"} {
		if _, err := saveAgent(root, Agent{Name: "Q", Role: "r", Access: acc}); !isMsg(err, "srv.teams_store.accesInvalide") {
			t.Fatalf("%q: %v", acc, err)
		}
	}
	if _, err := saveAgent(root, Agent{Name: "ADA", Role: "r", Access: "read"}); !isMsg(err, "srv.teams_store.doublon") {
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
		"srv.teams_store.membreDoublon": {Name: "T", Members: []string{"ada", "ada"}},
		"srv.teams_store.nomVide":       {Members: []string{"ada"}},
	}
	for k, tt := range cases {
		if _, err := saveTeam(root, tt); !isMsg(err, k) {
			t.Fatalf("%s: %v", k, err)
		}
	}
	if _, err := saveTeam(root, Team{Name: "T", Members: []string{"atlas"}}); !isMsg(err, "srv.teams_store.membreInconnu") {
		t.Fatal("chef accepté comme membre")
	}
	b, err := saveTeam(root, Team{ID: "feature", Name: "F2", Members: []string{"ada"}})
	if err != nil || b.Builtin || b.Base != "feature" {
		t.Fatalf("copie team: %+v %v", b, err)
	}
}

func TestStoreCorruptFileKept(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "gld-teams.json")
	os.WriteFile(p, []byte("{pas du json"), 0o600)
	if _, err := loadCatalog(root); !isMsg(err, "srv.teams_store.fichierIllisible") {
		t.Fatalf("load: %v", err)
	}
	if _, err := saveAgent(root, Agent{Name: "Z", Role: "r", Access: "read"}); err == nil {
		t.Fatal("écrasement")
	}
	if b, _ := os.ReadFile(p); string(b) != "{pas du json" {
		t.Fatal("fichier modifié")
	}
}

func TestSaveAgentRejectsBadModel(t *testing.T) {
	root := t.TempDir()
	for _, m := range []string{"a b", "x\ny: z", strings.Repeat("a", 65), "é"} {
		if _, err := saveAgent(root, Agent{Name: "Q", Role: "r", Access: "read", Model: m}); !isMsg(err, "srv.teams_store.modeleInvalide") {
			t.Fatalf("%q: %v", m, err)
		}
	}
	if _, err := saveAgent(root, Agent{Name: "Q", Role: "r", Access: "read", Model: "claude-sonnet-4.5"}); err != nil {
		t.Fatal(err)
	}
}
