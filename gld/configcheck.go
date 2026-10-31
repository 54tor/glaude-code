package main

// Lecture de la configuration : précédence des réglages, santé, hooks, serveurs MCP. Rien n'est modifié ici.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// --- précédence --------------------------------------------------------------------------------------

// EffRow : un réglage et ses valeurs par niveau. Les niveaux, du moins au plus prioritaire : u, p, l.
type EffRow struct {
	Path   string            `json:"path"`
	Values map[string]string `json:"values"` // niveau -> valeur (texte abrégé)
	Winner string            `json:"winner"` // niveau dont la valeur s'applique (pour une liste : tous s'ajoutent)
	Merged bool              `json:"merged,omitempty"`
}

const (
	summaryMaxLen         = 120
	hookCommandPreviewLen = 70
	mcpArgsPreviewLen     = 80
	mcpListTimeout        = 90 * time.Second
)

var levelRank = map[string]int{"u": 0, "p": 1, "l": 2}

// shellWordRe : les mots d'une ligne de commande de hook, guillemets simples ou doubles respectés.
var shellWordRe = regexp.MustCompile(`"([^"]*)"|'([^']*)'|(\S+)`)

func level(id string) string { return id[:1] }

// flatten met un document à plat : « permissions.allow[] » pour les éléments d'une liste, un chemin par feuille.
func flatten(prefix string, value any, depth int, paths map[string][]string) {
	switch typedValue := value.(type) {
	case *omap:
		if depth >= 2 || prefix == "hooks" {
			paths[prefix] = append(paths[prefix], summarize(value))
			return
		}
		for _, key := range typedValue.keys {
			childPath := key
			if prefix != "" {
				childPath = prefix + "." + key
			}
			flatten(childPath, typedValue.m[key], depth+1, paths)
		}
	case []any:
		for _, element := range typedValue {
			paths[prefix+"[]"] = append(paths[prefix+"[]"], summarize(element))
		}
	default:
		paths[prefix] = append(paths[prefix], summarize(value))
	}
}

func summarize(value any) string {
	switch typedValue := value.(type) {
	case string:
		return cut(typedValue, summaryMaxLen)
	case *omap:
		return "{" + L("srv.configcheck.keys", "A1", len(typedValue.keys)) + "}"
	case []any:
		return fmt.Sprintf("[%d]", len(typedValue))
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// effective : les réglages vus d'un projet (dir vide : utilisateur seul). Une liste s'additionne d'un niveau à l'autre ;
// pour le reste, le niveau le plus prioritaire l'emporte.
func effective(raw map[string]any, dir string) []EffRow { return effectiveFor(raw, dir, false) }

// effectiveFor : effective, avec le masquage strict pour un appelant sans config_write.
func effectiveFor(raw map[string]any, dir string, strict bool) []EffRow {
	fileIDs := []string{"u"}
	if dir != "" {
		fileIDs = append(fileIDs, "p/"+dir, "l/"+dir)
	}
	type cell struct{ vals map[string][]string }
	cells := map[string]*cell{}
	for _, fileID := range fileIDs {
		doc, found := raw[fileID]
		if !found {
			continue
		}
		flat := map[string][]string{}
		flatten("", maskSettings(doc, strict), 0, flat)
		for path, levelValues := range flat {
			if cells[path] == nil {
				cells[path] = &cell{map[string][]string{}}
			}
			cells[path].vals[level(fileID)] = levelValues
		}
	}
	var rows []EffRow
	for path, pathCell := range cells {
		row := EffRow{Path: path, Values: map[string]string{}, Merged: strings.HasSuffix(path, "[]")}
		bestRank := -1
		for fileLevel, levelValues := range pathCell.vals {
			row.Values[fileLevel] = strings.Join(levelValues, " · ")
			if levelRank[fileLevel] > bestRank {
				bestRank, row.Winner = levelRank[fileLevel], fileLevel
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(left, right int) bool {
		return rows[left].Path < rows[right].Path
	})
	return rows
}

// --- hooks -------------------------------------------------------------------------------------------

// hooksFor : hooksOf, commandes masquées pour un appelant sans config_write.
func hooksFor(raw map[string]any, strict bool) []HookInfo {
	hooks := hooksOf(raw)
	if strict {
		for hookIndex := range hooks {
			if hooks[hookIndex].Command != "" {
				hooks[hookIndex].Command = maskText
			}
		}
	}
	return hooks
}

type HookInfo struct {
	Scope   string `json:"scope"` // niveau : u, p, l
	File    string `json:"file"`  // identifiant du fichier
	Event   string `json:"event"`
	Matcher string `json:"matcher,omitempty"`
	Type    string `json:"type"`
	Command string `json:"command"`
	Missing string `json:"missing,omitempty"` // ce qui manque : fichier ou programme introuvable
}

var shellBuiltins = map[string]bool{"[": true, "test": true, "echo": true, "exit": true, "cd": true, "true": true, "false": true, ":": true, "export": true, "set": true, "if": true, "then": true, "fi": true, "printf": true, "exec": true}

// missingPart : la première chose que la commande d'un hook référence et qui n'existe pas (un fichier, ou un programme
// en position de commande). Les variables autres que $HOME ne sont pas résolues : on ne devine pas.
func missingPart(command string) string {
	home, _ := os.UserHomeDir()
	expandHome := func(word string) string {
		word = strings.NewReplacer("${HOME}", home, "$HOME", home).Replace(word)
		if strings.HasPrefix(word, "~/") {
			word = filepath.Join(home, word[2:])
		}
		return word
	}
	tokens := shellWordRe.FindAllStringSubmatch(command, -1)
	atCommand := true // le mot suivant est un nom de programme (début, ou après && || ; |)
	for _, tokenMatch := range tokens {
		word := tokenMatch[1] + tokenMatch[2] + tokenMatch[3]
		quoted := tokenMatch[3] == ""
		isOperator := !quoted && (word == "&&" || word == "||" || word == ";" || word == "|")
		word = strings.TrimRight(word, ";")
		if isOperator {
			atCommand = true
			continue
		}
		endsCommand := !quoted && strings.HasSuffix(tokenMatch[3], ";")
		isOption := strings.HasPrefix(word, "-")
		hasSpecialCharacter := strings.Contains(word, "://") || strings.ContainsAny(word, "=<>*?`")
		isTestBracket := word == "]" || word == "["
		isSkipped := word == "" || isOption || hasSpecialCharacter || isTestBracket
		word = expandHome(word)
		isCheckable := !isSkipped && !strings.Contains(word, "$")
		if isCheckable {
			switch {
			case strings.HasPrefix(word, "/"): // un chemin relatif dépend du dossier où le hook se lance, inconnu ici
				if _, err := os.Stat(word); err != nil {
					return word
				}
			case atCommand && !shellBuiltins[word] && !strings.Contains(word, "/"):
				if _, err := exec.LookPath(word); err != nil {
					return word
				}
			}
		}
		atCommand = endsCommand
	}
	return ""
}

func hooksOf(raw map[string]any) []HookInfo {
	var hooks []HookInfo
	fileIDs := make([]string, 0, len(raw))
	for fileID := range raw {
		fileIDs = append(fileIDs, fileID)
	}
	sort.Slice(fileIDs, func(left, right int) bool {
		if leftRank, rightRank := levelRank[level(fileIDs[left])], levelRank[level(fileIDs[right])]; leftRank != rightRank {
			return leftRank < rightRank
		}
		return fileIDs[left] < fileIDs[right]
	})
	for _, fileID := range fileIDs {
		hooksValue, found := lookup(raw[fileID], "hooks")
		hooksByEvent := asMap(hooksValue)
		if !found || hooksByEvent == nil {
			continue
		}
		for _, event := range hooksByEvent.keys {
			entries, _ := hooksByEvent.m[event].([]any)
			for _, entry := range entries {
				entryObject := asMap(entry)
				if entryObject == nil {
					continue
				}
				matcher, _ := entryObject.m["matcher"].(string)
				hookList, _ := entryObject.m["hooks"].([]any)
				for _, hook := range hookList {
					hookObject := asMap(hook)
					if hookObject == nil {
						continue
					}
					hookType, _ := hookObject.m["type"].(string)
					command, _ := hookObject.m["command"].(string)
					hookInfo := HookInfo{Scope: level(fileID), File: fileID, Event: event, Matcher: matcher, Type: hookType, Command: command}
					if hookType == "command" {
						hookInfo.Missing = missingPart(command)
					}
					hooks = append(hooks, hookInfo)
				}
			}
		}
	}
	return hooks
}

// --- santé -------------------------------------------------------------------------------------------

type CfgIssue struct {
	File string `json:"file"`
	Kind string `json:"kind"` // json, hook, permission, plugin, secret
	Msg  string `json:"msg"`
}

var broadRule = regexp.MustCompile(`^(\*|Bash|Bash\(\*\)|Bash\(:\*\)|Edit|Write|Read|WebFetch)$`)

func installedPlugins() map[string]bool {
	installed := map[string]bool{}
	content, err := readTrusted(filepath.Join(cfgDir(), "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var installedFile struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if json.Unmarshal(content, &installedFile) != nil {
		return nil
	}
	for pluginID := range installedFile.Plugins {
		installed[pluginID] = true
	}
	return installed
}

func configHealth(files []ConfigFile, raw map[string]any) []CfgIssue {
	var issues []CfgIssue
	for _, configFile := range files {
		if configFile.Error != "" {
			issues = append(issues, CfgIssue{configFile.ID, "json", configFile.Label + " : " + configFile.Error})
		}
	}
	for _, hook := range hooksOf(raw) {
		if hook.Missing != "" {
			issues = append(issues, CfgIssue{hook.File, "hook", L("srv.configcheck.hookMissing", "A1", hook.Event, "A2", hook.Missing, "A3", cut(hook.Command, hookCommandPreviewLen))})
		}
	}
	plugins := installedPlugins()
	fileIDs := make([]string, 0, len(raw))
	for fileID := range raw {
		fileIDs = append(fileIDs, fileID)
	}
	sort.Strings(fileIDs)
	for _, fileID := range fileIDs {
		doc := raw[fileID]
		for _, kind := range []string{"allow", "deny", "ask"} {
			rules, _ := lookup(doc, "permissions", kind)
			seen := map[string]bool{}
			for _, rule := range strList(rules) {
				if seen[rule] {
					issues = append(issues, CfgIssue{fileID, "permission", L("srv.configcheck.dupRule", "A1", kind, "A2", rule)})
				}
				seen[rule] = true
				if kind == "allow" && broadRule.MatchString(rule) {
					issues = append(issues, CfgIssue{fileID, "permission", L("srv.configcheck.broadRule", "A1", rule)})
				}
			}
		}
		if directories, found := lookup(doc, "permissions", "additionalDirectories"); found {
			for _, dir := range strList(directories) {
				if expanded := expandDir(dir, fileID); expanded != "" {
					if dirInfo, err := os.Stat(expanded); err != nil || !dirInfo.IsDir() {
						issues = append(issues, CfgIssue{fileID, "dossier", L("srv.configcheck.dirMissing", "A1", dir)})
					}
				}
			}
		}
		allowRules, _ := lookup(doc, "permissions", "allow")
		denyRules, _ := lookup(doc, "permissions", "deny")
		denied := map[string]bool{}
		for _, rule := range strList(denyRules) {
			denied[rule] = true
		}
		for _, rule := range strList(allowRules) {
			if denied[rule] {
				issues = append(issues, CfgIssue{fileID, "permission", L("srv.configcheck.allowAndDeny", "A1", rule)})
			}
		}
		if enabledPlugins, found := lookup(doc, "enabledPlugins"); found && plugins != nil {
			if enabledMap := asMap(enabledPlugins); enabledMap != nil {
				for _, pluginID := range enabledMap.keys {
					if enabled, _ := enabledMap.m[pluginID].(bool); enabled && !plugins[pluginID] {
						issues = append(issues, CfgIssue{fileID, "plugin", L("srv.configcheck.pluginMissing", "A1", pluginID)})
					}
				}
			}
		}
		if fileID[0] == 'p' { // versionné avec le projet : pas de secret ici
			if envValue, found := lookup(doc, "env"); found {
				if envMap := asMap(envValue); envMap != nil {
					for _, variableName := range envMap.keys {
						if variableValue, _ := envMap.m[variableName].(string); secretKey.MatchString(variableName) && variableValue != "" {
							issues = append(issues, CfgIssue{fileID, "secret", L("srv.configcheck.secretVar", "A1", variableName)})
						}
					}
				}
			}
		}
	}
	return issues
}

// --- serveurs MCP (lecture seule) ----------------------------------------------------------------------

type McpServer struct {
	Name     string   `json:"name"`
	Scope    string   `json:"scope"` // user, project (.mcp.json), local (~/.claude.json, par projet)
	Project  string   `json:"project,omitempty"`
	Type     string   `json:"type"`
	Command  string   `json:"command,omitempty"`
	URL      string   `json:"url,omitempty"`
	EnvKeys  []string `json:"envKeys,omitempty"`
	ArgCount int      `json:"argCount,omitempty"`
}

// mcpFrom : strict (appelant sans config_write) ne montre que l'exécutable, le nombre d'arguments et l'hôte.
func mcpFrom(servers any, scope, project string, strict bool) []McpServer {
	serverMap := asMap(servers)
	if serverMap == nil {
		return nil
	}
	var parsedServers []McpServer
	for _, name := range serverMap.keys {
		serverObject := asMap(serverMap.m[name])
		if serverObject == nil {
			continue
		}
		server := McpServer{Name: name, Scope: scope, Project: project}
		server.Type, _ = serverObject.m["type"].(string)
		server.Command, _ = serverObject.m["command"].(string)
		if args := strList(serverObject.m["args"]); len(args) > 0 {
			if strict {
				server.ArgCount = len(args)
			} else {
				server.Command = strings.TrimSpace(server.Command + " " + cut(mcpMaskArgs(args), mcpArgsPreviewLen))
			}
		}
		server.URL, _ = serverObject.m["url"].(string)
		server.URL = mcpMaskURL(server.URL, strict)
		if server.Type == "" {
			if server.URL != "" {
				server.Type = "http"
			} else {
				server.Type = "stdio"
			}
		}
		if envMap := asMap(serverObject.m["env"]); envMap != nil {
			server.EnvKeys = append(server.EnvKeys, envMap.keys...)
		}
		if headersMap := asMap(serverObject.m["headers"]); headersMap != nil {
			server.EnvKeys = append(server.EnvKeys, headersMap.keys...)
		}
		parsedServers = append(parsedServers, server)
	}
	return parsedServers
}

// mcpServers : les serveurs déclarés dans ~/.claude.json (utilisateur, puis par projet) et dans les .mcp.json des projets.
// Seules les clés des serveurs sont lues : le reste de ~/.claude.json (compte, identifiants) n'est jamais exposé.
func mcpServers() []McpServer { return mcpServersFor(false) }

func mcpServersFor(strict bool) []McpServer {
	var servers []McpServer
	if claudeJSONContent, readErr := readConfigFile(claudeJSON()); readErr == nil {
		if parsed, parseErr := parseOrdered(claudeJSONContent); parseErr == nil {
			if userServers, found := lookup(parsed, "mcpServers"); found {
				servers = append(servers, mcpFrom(userServers, "user", "", strict)...)
			}
			if projectsValue, found := lookup(parsed, "projects"); found {
				if projectsMap := asMap(projectsValue); projectsMap != nil {
					for _, projectPath := range projectsMap.keys {
						if projectServers, found := lookup(projectsMap.m[projectPath], "mcpServers"); found {
							servers = append(servers, mcpFrom(projectServers, "local", projectPath, strict)...)
						}
					}
				}
			}
		}
	}
	for _, liveProject := range liveProjects() {
		if mcpContent, readErr := readConfigFile(filepath.Join(liveProject.Project, ".mcp.json")); readErr == nil {
			if parsed, parseErr := parseOrdered(mcpContent); parseErr == nil {
				if projectServers, found := lookup(parsed, "mcpServers"); found {
					servers = append(servers, mcpFrom(projectServers, "project", liveProject.Project, strict)...)
				}
			}
		}
	}
	return servers
}

var mcpLine = regexp.MustCompile(`^(.+?): (.*) - (.+)$`)

// mcpStatus lance `claude mcp list` (il contrôle chaque serveur : quelques secondes) et renvoie l'état par nom.
var mcpStatus = func() (map[string]string, error) {
	commandContext, cancel := context.WithTimeout(context.Background(), mcpListTimeout)
	defer cancel()
	cmd := withPath(exec.CommandContext(commandContext, "claude", "mcp", "list"))
	cmd.Dir = workDir()
	listOutput, err := cmd.Output()
	if err != nil && len(listOutput) == 0 {
		return nil, err
	}
	statusByName := map[string]string{}
	for _, listLine := range strings.Split(string(listOutput), "\n") {
		if lineMatch := mcpLine.FindStringSubmatch(strings.TrimSpace(listLine)); lineMatch != nil {
			statusByName[lineMatch[1]] = strings.TrimSpace(lineMatch[3])
		}
	}
	return statusByName, nil
}

// expandDir : le chemin réel d'un dossier supplémentaire (« ~/ » = dossier personnel ; un chemin relatif se lit depuis le projet du
// fichier de réglages, id « p/<dossier> » ou « l/<dossier> »). Vide si on ne peut pas le savoir (relatif dans les réglages utilisateur).
func expandDir(dir, fileID string) string {
	switch {
	case dir == "~" || strings.HasPrefix(dir, "~/"):
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(dir, "~"))
	case filepath.IsAbs(dir):
		return dir
	case strings.HasPrefix(fileID, "p/") || strings.HasPrefix(fileID, "l/"):
		if projectDir := projectCwd(fileID[2:]); projectDir != "" {
			return filepath.Join(projectDir, dir)
		}
	}
	return ""
}

// mcpMaskURL : identifiants et paramètres d'adresse masqués (un jeton passe souvent en ?key=…).
// Illisible ou sans hôte : l'adresse est supprimée. strict : seuls schéma et hôte restent (un jeton peut loger dans le chemin).
func mcpMaskURL(raw string, strict bool) string {
	parsedURL, err := url.Parse(raw)
	if err != nil || parsedURL.Host == "" {
		return ""
	}
	parsedURL.User = nil
	if strict {
		return parsedURL.Scheme + "://" + parsedURL.Host
	}
	if parsedURL.RawQuery != "" {
		parsedURL.RawQuery = "…"
	}
	return parsedURL.String()
}

// mcpMaskArgs : la valeur qui suit un drapeau de type clé ou jeton, ou qui le suit après « = », est remplacée.
func mcpMaskArgs(args []string) string {
	masked := make([]string, len(args))
	hideNext := false
	for argIndex, arg := range args {
		switch {
		case hideNext:
			masked[argIndex], hideNext = "…", false
		case strings.HasPrefix(arg, "-") && secretKey.MatchString(arg):
			if flagName, _, hasValue := strings.Cut(arg, "="); hasValue {
				masked[argIndex] = flagName + "=…"
			} else {
				masked[argIndex], hideNext = arg, true
			}
		default:
			masked[argIndex] = arg
		}
	}
	return strings.Join(masked, " ")
}
