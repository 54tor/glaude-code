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
func agentFile(a Agent) []byte {
	f := agentFront{Name: a.ID, Description: a.Role, Model: a.Model}
	if a.Access == "read" {
		f.Tools = "Read, Grep, Glob"
	}
	y, _ := yaml.Marshal(f)
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(y)
	b.WriteString("---\n" + agentMarker + "\n\n" + a.Prompt + "\n")
	return b.Bytes()
}

// writeAgents écrit <cfgRoot>/agents/<id>.md ; un fichier existant sans marqueur n'est jamais touché.
func writeAgents(cfgRoot string, as []Agent) (written, conflicts []string, err error) {
	for _, a := range as {
		if !agentIDRe.MatchString(a.ID) {
			return nil, nil, terr("srv.teams_agentfiles.idInvalide", "A1", a.ID)
		}
	}
	dir := filepath.Join(cfgRoot, "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, terr("srv.teams_agentfiles.ecriture", "W1", err)
	}
	for _, a := range as {
		p := filepath.Join(dir, a.ID+".md")
		if old, e := os.ReadFile(p); e == nil {
			_, body, _ := frontMatter(string(old))
			if !bytes.HasPrefix([]byte(body), []byte(agentMarker)) {
				conflicts = append(conflicts, a.ID)
				continue
			}
		} else if !os.IsNotExist(e) {
			return written, conflicts, terr("srv.teams_agentfiles.ecriture", "W1", e)
		}
		if e := writeAtomic(dir, p, agentFile(a)); e != nil {
			return written, conflicts, terr("srv.teams_agentfiles.ecriture", "W1", e)
		}
		written = append(written, a.ID)
	}
	return written, conflicts, nil
}

func writeAtomic(dir, dst string, data []byte) error {
	f, err := os.CreateTemp(dir, ".gld-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(f.Name(), dst)
}
