package main

import (
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

const slashDescriptionMax = 200

type slashCmd struct {
	Cmd    string `json:"cmd"` // « /nom » ou « /plugin:nom »
	Desc   string `json:"desc"`
	Origin string `json:"origin"` // user, project, plugin
}

// scanCommands : les commandes d'un dossier commands/ (un .md par commande, sous-dossiers ignorés).
func scanCommands(base, prefix, origin string) []slashCmd {
	var commands []slashCmd
	entries, _ := readDirTrusted(base)
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".md")
		if entry.IsDir() || name == entry.Name() || !skillName.MatchString(name) {
			continue
		}
		var skill Skill
		if content := readSmall(filepath.Join(base, entry.Name())); content != nil {
			skill.Dir = name
			describeSkill(&skill, string(content))
		}
		commands = append(commands, slashCmd{"/" + prefix + name, cut(skill.Description, slashDescriptionMax), origin})
	}
	return commands
}

// readSmall : le contenu d'un fichier régulier d'au plus skillMaxBytes (ni lien, ni tube, ni gros fichier), nil sinon.
func readSmall(filePath string) []byte {
	file, err := openTrusted(filePath)
	if err != nil {
		return nil
	}
	defer file.Close()
	if fileInfo, statErr := file.Stat(); statErr != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() > skillMaxBytes {
		return nil
	}
	content, _ := io.ReadAll(io.LimitReader(file, skillMaxBytes+1))
	if len(content) > skillMaxBytes {
		return nil
	}
	return content
}

// slashCommands : les « / » que l'utilisateur a en plus de ceux de Claude Code, personnels, de projet et de plugins.
func slashCommands() []slashCmd {
	commands := scanCommands(filepath.Join(cfgDir(), "commands"), "", "user")
	for _, skill := range listSkills() {
		if skill.Hidden || skill.Scope == "store" {
			continue
		}
		command := "/" + skill.Dir
		if skill.Scope == "plugin" {
			command = "/" + strings.SplitN(skill.Plugin, "@", 2)[0] + ":" + skill.Dir
		}
		commands = append(commands, slashCmd{command, cut(skill.Description, slashDescriptionMax), skill.Scope})
	}
	for _, liveProject := range liveProjects() {
		commands = append(commands, scanCommands(filepath.Join(liveProject.Project, ".claude", "commands"), "", "project")...)
	}
	for _, plugin := range pluginSkillRoots() {
		commands = append(commands, scanCommands(filepath.Join(filepath.Dir(plugin.base), "commands"), strings.SplitN(plugin.id, "@", 2)[0]+":", "plugin")...)
	}
	seen := map[string]bool{}
	unique := commands[:0]
	for _, command := range commands {
		if !seen[command.Cmd] {
			seen[command.Cmd] = true
			unique = append(unique, command)
		}
	}
	sort.Slice(unique, func(left, right int) bool { return unique[left].Cmd < unique[right].Cmd })
	return unique
}

func routesSlash(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/slash-commands", func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSON(responseWriter, 200, map[string]any{"commands": slashCommands()})
	})
}
