package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"

	"go.yaml.in/yaml/v3"
)

// Marqueur en tête du corps : seuls les fichiers qui le portent sont réécrits par gld.
const agentMarker = "<!-- gld:managed -->"

var agentIDRe = regexp.MustCompile(`^[a-z0-9-]+$`)

type agentFront struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Model       string `yaml:"model,omitempty"`
	Tools       string `yaml:"tools,omitempty"`
}

// agentFile : le .md de définition lu par Claude Code. « read » n'a que des outils de lecture locaux.
func agentFile(agent Agent) []byte {
	front := agentFront{Name: agent.ID, Description: agent.Role, Model: agent.Model}
	if agent.Access == "read" {
		front.Tools = "Read, Grep, Glob"
	}
	frontYAML, _ := yaml.Marshal(front)
	var document bytes.Buffer
	document.WriteString("---\n")
	document.Write(frontYAML)
	document.WriteString("---\n" + agentMarker + "\n\n" + agent.Prompt + "\n")
	return document.Bytes()
}

// writeAgents écrit <cfgRoot>/agents/<id>.md ; un fichier existant sans marqueur n'est jamais touché.
func writeAgents(cfgRoot string, agents []Agent) (written, conflicts []string, err error) {
	for _, agent := range agents {
		if !agentIDRe.MatchString(agent.ID) {
			return nil, nil, terr("srv.teams_agentfiles.idInvalide", "A1", agent.ID)
		}
	}
	dir := filepath.Join(cfgRoot, "agents")
	// cfgRoot est inscriptible par le compte cible sous GLD_RUN_AS : création, lecture et écriture confinées (rootfs.go).
	if mkdirErr := rMkdirAll(cfgRoot, dir, 0o755); mkdirErr != nil {
		return nil, nil, terr("srv.teams_agentfiles.ecriture", "W1", mkdirErr)
	}
	for _, agent := range agents {
		agentPath := filepath.Join(dir, agent.ID+".md")
		if existing, readErr := readTrustedIn(cfgRoot, agentPath); readErr == nil {
			_, body, _ := frontMatter(string(existing))
			if !bytes.HasPrefix([]byte(body), []byte(agentMarker)) {
				conflicts = append(conflicts, agent.ID)
				continue
			}
		} else if !os.IsNotExist(readErr) {
			return written, conflicts, terr("srv.teams_agentfiles.ecriture", "W1", readErr)
		}
		if fileWriteErr := rWriteAtomic(cfgRoot, agentPath, agentFile(agent), 0o644); fileWriteErr != nil {
			return written, conflicts, terr("srv.teams_agentfiles.ecriture", "W1", fileWriteErr)
		}
		written = append(written, agent.ID)
	}
	return written, conflicts, nil
}
