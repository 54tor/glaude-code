package main

// Ce que chaque projet configure en propre : ses fichiers de réglages (projet et local), ses règles de permission, ses hooks,
// ses serveurs MCP, ses règles (.claude/rules), ses skills et sa documentation. Lecture seule : un clic mène à l'édition.

import (
	"path/filepath"
	"sort"
	"strings"
)

const projectRuleMaxBytes = 1 << 20

type ProjRow struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

type ProjFile struct {
	ID    string    `json:"id"`
	Label string    `json:"label"` // settings.json (versionné avec le projet) ou settings.local.json (à vous seul)
	Rows  []ProjRow `json:"rows"`
}

type ProjRule struct {
	Rule  string `json:"rule"`
	Scope string `json:"scope"` // p : réglages du projet ; l : réglages locaux
}

type ProjDocRule struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths,omitempty"` // règle scopée : motifs du champ paths
	Warn  string   `json:"warn,omitempty"`
}

type ProjectCfg struct {
	Dir      string                `json:"dir"`
	Project  string                `json:"project"`
	Files    []ProjFile            `json:"files"`
	Perms    map[string][]ProjRule `json:"perms"`
	Hooks    []HookInfo            `json:"hooks"`
	MCP      []McpServer           `json:"mcp"`
	Rules    []ProjDocRule         `json:"rules"`
	Skills   []string              `json:"skills"`
	Docs     []string              `json:"docs"`
	Specific int                   `json:"specific"` // nombre d'éléments propres au projet : 0 = rien de spécifique
}

// projectRules : les règles Markdown de .claude/rules d'un projet (lecture seule), avec leur portée.
func projectRules(cwd string) []ProjDocRule {
	var rules []ProjDocRule
	entries, _ := readDirTrusted(filepath.Join(cwd, ".claude", "rules"))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		rule := ProjDocRule{Name: entry.Name()}
		content, readErr := readTrusted(filepath.Join(cwd, ".claude", "rules", entry.Name()))
		isReadable := readErr == nil && len(content) < projectRuleMaxBytes
		if isReadable {
			if meta, parseErr := parseMeta(string(content)); parseErr != nil {
				rule.Warn = parseErr.Error()
			} else if _, hasPaths := meta["paths"]; hasPaths {
				globs, globErr := globsOf(meta)
				if globErr != nil {
					rule.Warn = globErr.Error()
				}
				rule.Paths = globs
			}
		}
		rules = append(rules, rule)
	}
	return rules
}

func projectConfigs() []ProjectCfg { return projectConfigsFor(false) }

// projectConfigsFor : projectConfigs, avec le masquage strict pour un appelant sans config_write.
func projectConfigsFor(strict bool) []ProjectCfg {
	_, raw := allConfigsFor(strict)
	hooks := hooksFor(raw, strict)
	servers := mcpServersFor(strict)
	skills := listSkills()
	_, memProjects, _ := memList()
	docs := map[string][]string{}
	for _, memProject := range memProjects {
		for _, memFile := range memProject.Files {
			if strings.HasPrefix(memFile.Key, "p/") {
				docs[memProject.Dir] = append(docs[memProject.Dir], memFile.Name)
			}
		}
	}
	configs := []ProjectCfg{}
	for _, liveProject := range liveProjects() {
		projectConfig := ProjectCfg{Dir: liveProject.Dir, Project: liveProject.Project, Perms: map[string][]ProjRule{}, Files: []ProjFile{}, Hooks: []HookInfo{}, MCP: []McpServer{}, Skills: []string{}, Docs: docs[liveProject.Dir]}
		for _, scope := range []string{"p", "l"} {
			fileID := scope + "/" + liveProject.Dir
			doc, found := raw[fileID]
			if !found {
				continue
			}
			label := "settings.json"
			if scope == "l" {
				label = "settings.local.json"
			}
			flat := map[string][]string{}
			flatten("", maskSettings(doc, strict), 0, flat)
			projectFile := ProjFile{ID: fileID, Label: label, Rows: []ProjRow{}}
			for path, values := range flat {
				projectFile.Rows = append(projectFile.Rows, ProjRow{path, strings.Join(values, " · ")})
			}
			sort.Slice(projectFile.Rows, func(left, right int) bool {
				return projectFile.Rows[left].Path < projectFile.Rows[right].Path
			})
			projectConfig.Files = append(projectConfig.Files, projectFile)
			for _, kind := range append(append([]string{}, permKinds...), "additionalDirectories") {
				ruleValues, _ := lookup(doc, "permissions", kind)
				for _, rule := range strList(ruleValues) {
					projectConfig.Perms[kind] = append(projectConfig.Perms[kind], ProjRule{rule, scope})
				}
			}
		}
		for _, hook := range hooks {
			belongsToProject := hook.File == "p/"+liveProject.Dir || hook.File == "l/"+liveProject.Dir
			if belongsToProject {
				projectConfig.Hooks = append(projectConfig.Hooks, hook)
			}
		}
		for _, server := range servers {
			isProjectScope := server.Scope == "project" || server.Scope == "local"
			if server.Project == liveProject.Project && isProjectScope {
				projectConfig.MCP = append(projectConfig.MCP, server)
			}
		}
		for _, skill := range skills {
			if skill.Scope == "project" && skill.Project == liveProject.Project {
				projectConfig.Skills = append(projectConfig.Skills, skill.Name)
			}
		}
		projectConfig.Rules = projectRules(liveProject.Project)
		if projectConfig.Rules == nil {
			projectConfig.Rules = []ProjDocRule{}
		}
		if projectConfig.Docs == nil {
			projectConfig.Docs = []string{}
		}
		specificCount := len(projectConfig.Hooks) + len(projectConfig.MCP) + len(projectConfig.Skills) + len(projectConfig.Rules)
		for _, projectFile := range projectConfig.Files {
			specificCount += len(projectFile.Rows)
		}
		projectConfig.Specific = specificCount
		configs = append(configs, projectConfig)
	}
	sort.Slice(configs, func(left, right int) bool {
		leftHasSpecific := configs[left].Specific > 0
		rightHasSpecific := configs[right].Specific > 0
		if leftHasSpecific != rightHasSpecific {
			return leftHasSpecific
		}
		return configs[left].Project < configs[right].Project
	})
	return configs
}
