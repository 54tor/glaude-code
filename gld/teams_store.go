package main

// Agents et teams créés par l'utilisateur, par home, dans <cfgRoot>/gld-teams.json ; fusionnés avec le catalogue fourni.
// Les éléments fournis ne sont jamais écrits : les modifier crée une copie « perso » (ID x-<8 hexa>, Base = fourni).

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	maxPersoAgents = 200
	maxPersoTeams  = 100
	maxPromptBytes = 8000
)

var teamsMu sync.Mutex // un seul verrou de package : lecture-modification-écriture sérialisées

func teamsPath(cfgRoot string) string { return filepath.Join(cfgRoot, "gld-teams.json") }

// persoLoad : le contenu perso ; un fichier absent ou vide est vide, un fichier illisible une erreur (jamais réécrit).
func persoLoad(cfgRoot string) (Catalog, error) {
	var c Catalog
	b, err := readTrustedIn(cfgRoot, teamsPath(cfgRoot))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, terr("srv.teams_store.fichierIllisible", "A1", err.Error())
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Catalog{}, terr("srv.teams_store.fichierIllisible", "A1", err.Error())
	}
	return c, nil
}

func persoSave(cfgRoot string, c Catalog) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	return rWriteAtomic(cfgRoot, teamsPath(cfgRoot), b, 0o600)
}

func mergeCatalog(p Catalog) Catalog {
	c := builtinCatalog()
	c.Agents = append(c.Agents, p.Agents...)
	c.Teams = append(c.Teams, p.Teams...)
	return c
}

func loadCatalog(cfgRoot string) (Catalog, error) {
	teamsMu.Lock()
	defer teamsMu.Unlock()
	p, err := persoLoad(cfgRoot)
	if err != nil {
		return Catalog{}, err
	}
	return mergeCatalog(p), nil
}

func newPersoID() string {
	var b [4]byte
	rand.Read(b[:])
	return "x-" + hex.EncodeToString(b[:])
}

var modelRe = regexp.MustCompile(`^[A-Za-z0-9._-]{0,64}$`)

func saveAgent(cfgRoot string, a Agent) (Agent, error) {
	teamsMu.Lock()
	defer teamsMu.Unlock()
	p, err := persoLoad(cfgRoot)
	if err != nil {
		return Agent{}, err
	}
	all := mergeCatalog(p)
	a.Name, a.Role = strings.TrimSpace(a.Name), strings.TrimSpace(a.Role)
	switch {
	case a.Name == "":
		return Agent{}, terr("srv.teams_store.nomVide")
	case a.Role == "":
		return Agent{}, terr("srv.teams_store.roleVide")
	case a.Access != "read" && a.Access != "write":
		return Agent{}, terr("srv.teams_store.accesInvalide", "A1", a.Access)
	case !modelRe.MatchString(a.Model):
		return Agent{}, terr("srv.teams_store.modeleInvalide")
	case len(a.Prompt) > maxPromptBytes:
		return Agent{}, terr("srv.teams_store.promptTropLong")
	}
	idx, base := -1, ""
	if a.ID != "" {
		for _, x := range all.Agents {
			if x.ID == a.ID && x.Builtin {
				base = x.ID
			}
		}
		if base == "" {
			for i, x := range p.Agents {
				if x.ID == a.ID {
					idx = i
				}
			}
			if idx < 0 {
				return Agent{}, terr("srv.teams_store.idInconnu", "A1", a.ID)
			}
			base = p.Agents[idx].Base
		}
	}
	if idx < 0 {
		if len(p.Agents) >= maxPersoAgents {
			return Agent{}, terr("srv.teams_store.tropDAgents")
		}
		a.ID = newPersoID()
	}
	for _, x := range all.Agents {
		if x.ID != a.ID && strings.EqualFold(x.Name, a.Name) {
			return Agent{}, terr("srv.teams_store.doublon", "A1", a.Name)
		}
	}
	a.Builtin, a.Base = false, base
	if idx < 0 {
		p.Agents = append(p.Agents, a)
	} else {
		p.Agents[idx] = a
	}
	return a, persoSave(cfgRoot, p)
}

func saveTeam(cfgRoot string, t Team) (Team, error) {
	teamsMu.Lock()
	defer teamsMu.Unlock()
	p, err := persoLoad(cfgRoot)
	if err != nil {
		return Team{}, err
	}
	all := mergeCatalog(p)
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return Team{}, terr("srv.teams_store.nomVide")
	}
	if len(t.Members) == 0 {
		return Team{}, terr("srv.teams_store.membreVide")
	}
	ok := map[string]bool{}
	for _, x := range all.Agents {
		ok[x.ID] = x.Access != "lead" // le chef est implicite, jamais membre
	}
	seen := map[string]bool{}
	for _, m := range t.Members {
		if !ok[m] {
			return Team{}, terr("srv.teams_store.membreInconnu", "A1", m)
		}
		if seen[m] {
			return Team{}, terr("srv.teams_store.membreDoublon", "A1", m)
		}
		seen[m] = true
	}
	idx, base := -1, ""
	if t.ID != "" {
		for _, x := range all.Teams {
			if x.ID == t.ID && x.Builtin {
				base = x.ID
			}
		}
		if base == "" {
			for i, x := range p.Teams {
				if x.ID == t.ID {
					idx = i
				}
			}
			if idx < 0 {
				return Team{}, terr("srv.teams_store.idInconnu", "A1", t.ID)
			}
			base = p.Teams[idx].Base
		}
	}
	if idx < 0 {
		if len(p.Teams) >= maxPersoTeams {
			return Team{}, terr("srv.teams_store.tropDeTeams")
		}
		t.ID = newPersoID()
	}
	t.Builtin, t.Base = false, base
	if idx < 0 {
		p.Teams = append(p.Teams, t)
	} else {
		p.Teams[idx] = t
	}
	return t, persoSave(cfgRoot, p)
}

func removeAgent(cfgRoot, id string) error {
	teamsMu.Lock()
	defer teamsMu.Unlock()
	p, err := persoLoad(cfgRoot)
	if err != nil {
		return err
	}
	for _, x := range builtinCatalog().Agents {
		if x.ID == id {
			return terr("srv.teams_store.builtinRefuse", "A1", id)
		}
	}
	for _, t := range mergeCatalog(p).Teams {
		for _, m := range t.Members {
			if m == id {
				return terr("srv.teams_store.agentUtilise", "A1", id, "A2", t.Name)
			}
		}
	}
	for i, x := range p.Agents {
		if x.ID == id {
			p.Agents = append(p.Agents[:i], p.Agents[i+1:]...)
			return persoSave(cfgRoot, p)
		}
	}
	return terr("srv.teams_store.idInconnu", "A1", id)
}

func removeTeam(cfgRoot, id string) error {
	teamsMu.Lock()
	defer teamsMu.Unlock()
	p, err := persoLoad(cfgRoot)
	if err != nil {
		return err
	}
	for _, x := range builtinCatalog().Teams {
		if x.ID == id {
			return terr("srv.teams_store.builtinRefuse", "A1", id)
		}
	}
	for i, x := range p.Teams {
		if x.ID == id {
			p.Teams = append(p.Teams[:i], p.Teams[i+1:]...)
			return persoSave(cfgRoot, p)
		}
	}
	return terr("srv.teams_store.idInconnu", "A1", id)
}
