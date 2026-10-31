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
	var catalog Catalog
	content, err := readTrustedIn(cfgRoot, teamsPath(cfgRoot))
	if errors.Is(err, fs.ErrNotExist) {
		return catalog, nil
	}
	if err != nil {
		return catalog, terr("srv.teams_store.fichierIllisible", "A1", err.Error())
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return catalog, nil
	}
	if decodeErr := json.Unmarshal(content, &catalog); decodeErr != nil {
		return Catalog{}, terr("srv.teams_store.fichierIllisible", "A1", decodeErr.Error())
	}
	return catalog, nil
}

func persoSave(cfgRoot string, catalog Catalog) error {
	content, _ := json.MarshalIndent(catalog, "", "  ")
	return rWriteAtomic(cfgRoot, teamsPath(cfgRoot), content, 0o600)
}

func mergeCatalog(personal Catalog) Catalog {
	merged := builtinCatalog()
	merged.Agents = append(merged.Agents, personal.Agents...)
	merged.Teams = append(merged.Teams, personal.Teams...)
	return merged
}

func loadCatalog(cfgRoot string) (Catalog, error) {
	teamsMu.Lock()
	defer teamsMu.Unlock()
	personal, err := persoLoad(cfgRoot)
	if err != nil {
		return Catalog{}, err
	}
	return mergeCatalog(personal), nil
}

func newPersoID() string {
	var randomBytes [4]byte
	rand.Read(randomBytes[:])
	return "x-" + hex.EncodeToString(randomBytes[:])
}

var modelRe = regexp.MustCompile(`^[A-Za-z0-9._-]{0,64}$`)

func saveAgent(cfgRoot string, agent Agent) (Agent, error) {
	teamsMu.Lock()
	defer teamsMu.Unlock()
	personal, err := persoLoad(cfgRoot)
	if err != nil {
		return Agent{}, err
	}
	all := mergeCatalog(personal)
	agent.Name, agent.Role = strings.TrimSpace(agent.Name), strings.TrimSpace(agent.Role)
	switch {
	case agent.Name == "":
		return Agent{}, terr("srv.teams_store.nomVide")
	case agent.Role == "":
		return Agent{}, terr("srv.teams_store.roleVide")
	case agent.Access != "read" && agent.Access != "write":
		return Agent{}, terr("srv.teams_store.accesInvalide", "A1", agent.Access)
	case !modelRe.MatchString(agent.Model):
		return Agent{}, terr("srv.teams_store.modeleInvalide")
	case len(agent.Prompt) > maxPromptBytes:
		return Agent{}, terr("srv.teams_store.promptTropLong")
	}
	existingIndex, base := -1, ""
	if agent.ID != "" {
		for _, candidate := range all.Agents {
			if candidate.ID == agent.ID && candidate.Builtin {
				base = candidate.ID
			}
		}
		if base == "" {
			for agentIndex, candidate := range personal.Agents {
				if candidate.ID == agent.ID {
					existingIndex = agentIndex
				}
			}
			if existingIndex < 0 {
				return Agent{}, terr("srv.teams_store.idInconnu", "A1", agent.ID)
			}
			base = personal.Agents[existingIndex].Base
		}
	}
	if existingIndex < 0 {
		if len(personal.Agents) >= maxPersoAgents {
			return Agent{}, terr("srv.teams_store.tropDAgents")
		}
		agent.ID = newPersoID()
	}
	for _, candidate := range all.Agents {
		if candidate.ID != agent.ID && strings.EqualFold(candidate.Name, agent.Name) {
			return Agent{}, terr("srv.teams_store.doublon", "A1", agent.Name)
		}
	}
	agent.Builtin, agent.Base = false, base
	if existingIndex < 0 {
		personal.Agents = append(personal.Agents, agent)
	} else {
		personal.Agents[existingIndex] = agent
	}
	return agent, persoSave(cfgRoot, personal)
}

func saveTeam(cfgRoot string, team Team) (Team, error) {
	teamsMu.Lock()
	defer teamsMu.Unlock()
	personal, err := persoLoad(cfgRoot)
	if err != nil {
		return Team{}, err
	}
	all := mergeCatalog(personal)
	team.Name = strings.TrimSpace(team.Name)
	if team.Name == "" {
		return Team{}, terr("srv.teams_store.nomVide")
	}
	if len(team.Members) == 0 {
		return Team{}, terr("srv.teams_store.membreVide")
	}
	isEligible := map[string]bool{}
	for _, agent := range all.Agents {
		isEligible[agent.ID] = agent.Access != "lead" // le chef est implicite, jamais membre
	}
	seen := map[string]bool{}
	for _, memberID := range team.Members {
		if !isEligible[memberID] {
			return Team{}, terr("srv.teams_store.membreInconnu", "A1", memberID)
		}
		if seen[memberID] {
			return Team{}, terr("srv.teams_store.membreDoublon", "A1", memberID)
		}
		seen[memberID] = true
	}
	existingIndex, base := -1, ""
	if team.ID != "" {
		for _, candidate := range all.Teams {
			if candidate.ID == team.ID && candidate.Builtin {
				base = candidate.ID
			}
		}
		if base == "" {
			for teamIndex, candidate := range personal.Teams {
				if candidate.ID == team.ID {
					existingIndex = teamIndex
				}
			}
			if existingIndex < 0 {
				return Team{}, terr("srv.teams_store.idInconnu", "A1", team.ID)
			}
			base = personal.Teams[existingIndex].Base
		}
	}
	if existingIndex < 0 {
		if len(personal.Teams) >= maxPersoTeams {
			return Team{}, terr("srv.teams_store.tropDeTeams")
		}
		team.ID = newPersoID()
	}
	team.Builtin, team.Base = false, base
	if existingIndex < 0 {
		personal.Teams = append(personal.Teams, team)
	} else {
		personal.Teams[existingIndex] = team
	}
	return team, persoSave(cfgRoot, personal)
}

func removeAgent(cfgRoot, id string) error {
	teamsMu.Lock()
	defer teamsMu.Unlock()
	personal, err := persoLoad(cfgRoot)
	if err != nil {
		return err
	}
	for _, agent := range builtinCatalog().Agents {
		if agent.ID == id {
			return terr("srv.teams_store.builtinRefuse", "A1", id)
		}
	}
	for _, team := range mergeCatalog(personal).Teams {
		for _, memberID := range team.Members {
			if memberID == id {
				return terr("srv.teams_store.agentUtilise", "A1", id, "A2", team.Name)
			}
		}
	}
	for agentIndex, agent := range personal.Agents {
		if agent.ID == id {
			personal.Agents = append(personal.Agents[:agentIndex], personal.Agents[agentIndex+1:]...)
			return persoSave(cfgRoot, personal)
		}
	}
	return terr("srv.teams_store.idInconnu", "A1", id)
}

func removeTeam(cfgRoot, id string) error {
	teamsMu.Lock()
	defer teamsMu.Unlock()
	personal, err := persoLoad(cfgRoot)
	if err != nil {
		return err
	}
	for _, team := range builtinCatalog().Teams {
		if team.ID == id {
			return terr("srv.teams_store.builtinRefuse", "A1", id)
		}
	}
	for teamIndex, team := range personal.Teams {
		if team.ID == id {
			personal.Teams = append(personal.Teams[:teamIndex], personal.Teams[teamIndex+1:]...)
			return persoSave(cfgRoot, personal)
		}
	}
	return terr("srv.teams_store.idInconnu", "A1", id)
}
