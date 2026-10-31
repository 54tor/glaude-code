package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Plugins, marketplaces et serveurs MCP d'une archive : tout passe par la CLI claude, seule à écrire dans ~/.claude et ~/.claude.json.

var mcpNameRe = regexp.MustCompile(`^[\w.-]{1,100}$`)

// mcpAddJSON : variable pour que les tests remplacent l'appel à claude.
const mcpAddTimeout = 30 * time.Second

var mcpAddJSON = func(name string, doc []byte) error {
	// un nom en « - » serait lu comme une option
	if !mcpNameRe.MatchString(name) || name[0] == '-' {
		return terr("srv.backup_cmds.nomMcpRefuse", "A1", name)
	}
	var serverFields map[string]json.RawMessage
	if json.Unmarshal(doc, &serverFields) != nil || serverFields == nil {
		return terr("srv.backup_cmds.mcpPasUnObjet", "A1", name)
	}
	commandContext, cancel := context.WithTimeout(context.Background(), mcpAddTimeout)
	defer cancel()
	cmd := withPath(exec.CommandContext(commandContext, "claude", "mcp", "add-json", name, string(doc), "-s", "user"))
	cmd.Dir = workDir()
	combinedOutput, err := cmd.CombinedOutput()
	if err != nil {
		return cmdErr(string(combinedOutput), err)
	}
	return nil
}

func cmdErr(commandOutput string, err error) error {
	if commandOutput = strings.TrimSpace(commandOutput); commandOutput != "" {
		return fmt.Errorf("%s", commandOutput)
	}
	return err
}

// dashArg : une valeur de l'archive qui commencerait par « - » serait lue comme une option de claude.
func dashArg(value string) bool { return strings.HasPrefix(value, "-") }

func readStaged(archiveStage *stage, name string, destination any) bool {
	content, err := os.ReadFile(filepath.Join(archiveStage.dir, name))
	return err == nil && json.Unmarshal(content, destination) == nil
}

func marketSource(marketplace Marketplace) string {
	if marketplace.Repo != "" {
		return marketplace.Repo
	}
	return marketplace.URL
}

// --- marketplaces ---

func planMarkets(archiveStage *stage) ([]SyncItem, error) {
	var archivedPlugins pluginsDoc
	if !readStaged(archiveStage, "plugins.json", &archivedPlugins) {
		return nil, nil
	}
	installedNames := map[string]bool{}
	if installedMarketplaces, err := listMarketplaces(); err == nil {
		for _, installedMarketplace := range installedMarketplaces {
			installedNames[installedMarketplace.Name] = true
		}
	}
	var items []SyncItem
	for _, archivedMarketplace := range archivedPlugins.Marketplaces {
		if dashArg(marketSource(archivedMarketplace)) {
			continue
		}
		status := "new"
		if installedNames[archivedMarketplace.Name] {
			status = "same"
		}
		items = append(items, SyncItem{ID: "market:" + archivedMarketplace.Name, Kind: "market", Label: archivedMarketplace.Name, Status: status, Cat: "plugins", From: marketSource(archivedMarketplace)})
	}
	return items, nil
}

func applyMarkets(archiveStage *stage, picks []SyncPick, _ map[string]string) []BackupResult {
	var archivedPlugins pluginsDoc
	readStaged(archiveStage, "plugins.json", &archivedPlugins)
	results := make([]BackupResult, 0, len(picks))
	for _, pick := range picks {
		name := strings.TrimPrefix(pick.ID, "market:")
		result := BackupResult{ID: pick.ID, Status: "skipped"}
		for _, marketplace := range archivedPlugins.Marketplaces {
			if marketplace.Name != name {
				continue
			}
			if dashArg(marketSource(marketplace)) {
				result = BackupResult{ID: pick.ID, Status: "failed", Message: L("srv.backup_cmds.valeurRefusee", "A1", marketSource(marketplace))}
				break
			}
			commandOutput, err := marketplaceAct("add", marketSource(marketplace), "")
			result = BackupResult{ID: pick.ID, Status: "ok"}
			if err != nil {
				result = BackupResult{ID: pick.ID, Status: "failed", Message: cmdErr(commandOutput, err).Error()}
			}
			break
		}
		results = append(results, result)
	}
	return results
}

// --- plugins ---

func pluginItemID(plugin PluginInst) string { return "plugin:" + plugin.Scope + ":" + plugin.ID }

// projectKeyFor : le dossier de transcripts de ~/.claude/projects dont le projet est ce chemin.
func projectKeyFor(path string) string {
	for _, memTarget := range memTargets() {
		if memTarget.Project == path {
			return memTarget.Dir
		}
	}
	return ""
}

func planPlugins(archiveStage *stage) ([]SyncItem, error) {
	var archivedPlugins pluginsDoc
	if !readStaged(archiveStage, "plugins.json", &archivedPlugins) {
		return nil, nil
	}
	installedByKey := map[string]PluginInst{}
	if installed, _, err := listPlugins("", 0); err == nil {
		for _, installedPlugin := range installed {
			installedByKey[installedPlugin.Scope+":"+installedPlugin.ID+":"+installedPlugin.Project] = installedPlugin
		}
	}
	var items []SyncItem
	for _, archivedPlugin := range archivedPlugins.Plugins {
		projectMissing := archivedPlugin.Scope != "user" && !isDirPath(archivedPlugin.Project)
		if dashArg(archivedPlugin.ID) || projectMissing {
			continue
		}
		status := "new"
		if installedPlugin, isInstalled := installedByKey[archivedPlugin.Scope+":"+archivedPlugin.ID+":"+archivedPlugin.Project]; isInstalled {
			status = "same"
			if installedPlugin.Enabled != archivedPlugin.Enabled {
				status = "diff"
			}
		}
		syncItem := SyncItem{ID: pluginItemID(archivedPlugin), Kind: "plugin", Label: archivedPlugin.ID, Status: status, Cat: "plugins", From: archivedPlugin.Scope, To: archivedPlugin.Project}
		items = append(items, syncItem)
	}
	return items, nil
}

// skippedPlugins : plugins de portée project/local dont le dossier de projet n'existe pas ici.
func skippedPlugins(archiveStage *stage) []string {
	var archivedPlugins pluginsDoc
	var skipped []string
	if readStaged(archiveStage, "plugins.json", &archivedPlugins) {
		for _, plugin := range archivedPlugins.Plugins {
			if plugin.Scope != "user" && !isDirPath(plugin.Project) {
				skipped = append(skipped, pluginItemID(plugin))
			}
		}
	}
	return skipped
}

func applyPlugins(archiveStage *stage, picks []SyncPick, _ map[string]string) []BackupResult {
	var archivedPlugins pluginsDoc
	readStaged(archiveStage, "plugins.json", &archivedPlugins)
	installedByKey := map[string]PluginInst{}
	if installed, _, err := listPlugins("", 0); err == nil {
		for _, installedPlugin := range installed {
			installedByKey[installedPlugin.Scope+":"+installedPlugin.ID+":"+installedPlugin.Project] = installedPlugin
		}
	}
	results := make([]BackupResult, 0, len(picks))
	for _, pick := range picks {
		var archivedPlugin *PluginInst
		for pluginIndex := range archivedPlugins.Plugins {
			if pluginItemID(archivedPlugins.Plugins[pluginIndex]) == pick.ID {
				archivedPlugin = &archivedPlugins.Plugins[pluginIndex]
				break
			}
		}
		if archivedPlugin == nil {
			results = append(results, BackupResult{ID: pick.ID, Status: "skipped"})
			continue
		}
		if dashArg(archivedPlugin.ID) {
			results = append(results, BackupResult{ID: pick.ID, Status: "failed", Message: L("srv.backup_cmds.valeurRefusee", "A1", archivedPlugin.ID)})
			continue
		}
		projectKey := ""
		if archivedPlugin.Scope != "user" {
			if projectKey = projectKeyFor(archivedPlugin.Project); projectKey == "" {
				results = append(results, BackupResult{ID: pick.ID, Status: "skipped"})
				continue
			}
		}
		installedPlugin, installed := installedByKey[archivedPlugin.Scope+":"+archivedPlugin.ID+":"+archivedPlugin.Project]
		var err error
		var commandOutput string
		if !installed {
			commandOutput, err = pluginAct("install", archivedPlugin.ID, archivedPlugin.Scope, projectKey)
		}
		// une installation active le plugin : seul l'état « désactivé » demande un appel de plus
		enabledStateDiffers := installed && installedPlugin.Enabled != archivedPlugin.Enabled
		installedAsDisabled := !installed && !archivedPlugin.Enabled
		if err == nil && (enabledStateDiffers || installedAsDisabled) {
			action := "enable"
			if !archivedPlugin.Enabled {
				action = "disable"
			}
			commandOutput, err = pluginAct(action, archivedPlugin.ID, archivedPlugin.Scope, projectKey)
		}
		result := BackupResult{ID: pick.ID, Status: "ok"}
		if err != nil {
			result = BackupResult{ID: pick.ID, Status: "failed", Message: cmdErr(commandOutput, err).Error()}
		}
		results = append(results, result)
	}
	return results
}

// --- serveurs MCP ---

type mcpArchive struct {
	Servers  map[string]json.RawMessage `json:"servers"`
	Stripped map[string][]string        `json:"stripped"`
}

func planMcp(archiveStage *stage) ([]SyncItem, error) {
	var archivedMcp mcpArchive
	if !readStaged(archiveStage, "mcp.json", &archivedMcp) {
		return nil, nil
	}
	installedNames := map[string]bool{}
	for _, server := range mcpServers() {
		if server.Scope == "user" {
			installedNames[server.Name] = true
		}
	}
	names := make([]string, 0, len(archivedMcp.Servers))
	for serverName := range archivedMcp.Servers {
		names = append(names, serverName)
	}
	sort.Strings(names)
	var items []SyncItem
	for _, serverName := range names {
		var serverFields map[string]json.RawMessage
		json.Unmarshal(archivedMcp.Servers[serverName], &serverFields)
		syncItem := SyncItem{ID: "mcp:" + serverName, Kind: "mcp", Label: serverName, Status: "new", Cat: "mcp"}
		if installedNames[serverName] {
			syncItem.Status = "same"
		}
		if _, hasCommand := serverFields["command"]; hasCommand {
			syncItem.Risk = "exec"
		}
		if strippedKeys := archivedMcp.Stripped[serverName]; len(strippedKeys) > 0 {
			syncItem.From = L("srv.backup_cmds.aSaisir", "A1", strings.Join(strippedKeys, ", "))
		}
		items = append(items, syncItem)
	}
	return items, nil
}

func applyMcp(archiveStage *stage, picks []SyncPick, _ map[string]string) []BackupResult {
	var archivedMcp mcpArchive
	readStaged(archiveStage, "mcp.json", &archivedMcp)
	results := make([]BackupResult, 0, len(picks))
	for _, pick := range picks {
		name := strings.TrimPrefix(pick.ID, "mcp:")
		serverDoc, found := archivedMcp.Servers[name]
		if !found {
			results = append(results, BackupResult{ID: pick.ID, Status: "skipped"})
			continue
		}
		result := BackupResult{ID: pick.ID, Status: "ok"}
		if err := mcpAddJSON(name, serverDoc); err != nil {
			result = BackupResult{ID: pick.ID, Status: "failed", Message: err.Error()}
		}
		results = append(results, result)
	}
	return results
}
