package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var backupCats = []string{"skills", "plugins", "settings", "memory", "mcp"}

type pluginsDoc struct {
	Marketplaces []Marketplace `json:"marketplaces"`
	Plugins      []PluginInst  `json:"plugins"`
}

// exportBackup : archive des catégories choisies de la configuration utilisée ; n = nombre de fichiers (hors manifeste).
func exportBackup(categories []string) ([]byte, int, error) {
	if len(categories) == 0 {
		return nil, 0, terr("srv.backup_export.catsVides")
	}
	for _, category := range categories {
		if !slices.Contains(backupCats, category) {
			return nil, 0, terr("srv.backup_export.catInconnue", "A1", category)
		}
	}
	hasCategory := func(category string) bool { return slices.Contains(categories, category) }
	files, executable := map[string][]byte{}, map[string]bool{}
	configDir := cfgDir()

	if hasCategory("settings") {
		settings, _, err := syncSettings(configDir)
		if err != nil {
			return nil, 0, err
		}
		for _, key := range slices.Clone(settings.keys) {
			skippedBySync := syncSkip[key] && key != "permissions"
			if skippedBySync || syncPluginKeys[key] || secretKey.MatchString(key) {
				settings.del(key)
			}
		}
		dropSecrets(settings)
		if len(settings.keys) > 0 {
			files["home/settings.json"] = []byte(formatOrdered(settings))
		}
	}
	if hasCategory("skills") || hasCategory("memory") {
		for rel, content := range syncFiles(configDir) {
			rel = filepath.ToSlash(rel)
			isCommandFile := strings.HasPrefix(rel, "commands/")
			wantedCommand := isCommandFile && hasCategory("skills")
			wantedMemory := !isCommandFile && hasCategory("memory")
			if wantedCommand || wantedMemory {
				files["home/"+rel] = []byte(content)
			}
		}
	}
	if hasCategory("skills") {
		for skillName, skillFiles := range syncSkills(configDir) {
			for rel, content := range skillFiles {
				name := "home/skills/" + skillName + "/" + filepath.ToSlash(rel)
				files[name] = []byte(content)
				fileInfo, err := os.Lstat(filepath.Join(configDir, "skills", skillName, rel))
				isExecutable := err == nil && fileInfo.Mode().IsRegular() && fileInfo.Mode()&executableBits != 0
				if isExecutable {
					executable[name] = true
				}
			}
		}
		snippets, _ := listSnippets()
		for _, projectSnippet := range snippets {
			if projectSnippet.Scope == "" {
				continue
			}
			if content, err := readSnippet(projectSnippet.Key); err == nil {
				files["projects/"+projectSnippet.Scope+"/commands/"+projectSnippet.Path] = []byte(content)
			}
		}
	}
	if hasCategory("memory") {
		_, projects, _ := memList()
		for _, memProject := range projects {
			for _, memoryFile := range memProject.Files {
				// m/ : mémoire de Claude ; p/ et r/ vivent dans le dépôt du projet
				if !strings.HasPrefix(memoryFile.Key, "m/") {
					continue
				}
				if content, err := memRead(memoryFile.Key); err == nil {
					files["projects/"+memProject.Dir+"/memory/"+memoryFile.Name] = []byte(content)
				}
			}
		}
	}
	if hasCategory("plugins") {
		installedPlugins, _, err := listPlugins("", 1<<30)
		if err != nil {
			return nil, 0, err
		}
		marketplaces, err := listMarketplaces()
		if err != nil {
			return nil, 0, err
		}
		pluginsJSON, _ := json.Marshal(pluginsDoc{Marketplaces: marketplaces, Plugins: installedPlugins})
		files["plugins.json"] = pluginsJSON
	}
	if hasCategory("mcp") {
		mcpJSON, err := exportMcp()
		if err != nil {
			return nil, 0, err
		}
		files["mcp.json"] = mcpJSON
	}
	archive, err := writeBackupZipExec(files, executable, BackupManifest{Format: backupFormat, Gld: version, Date: time.Now().UTC().Format(time.RFC3339), Cats: categories})
	return archive, len(files), err
}

// exportMcp : serveurs de portée utilisateur sans env, headers ni clé qui ressemble à un secret ; ce qui a été retiré est listé.
func exportMcp() ([]byte, error) {
	servers, stripped := newOmap(), map[string][]string{}
	if claudeJSONContent, readErr := readTrusted(claudeJSON()); readErr == nil {
		if parsed, parseErr := parseOrdered(claudeJSONContent); parseErr == nil {
			if serversValue, found := lookup(parsed, "mcpServers"); found && asMap(serversValue) != nil {
				for _, name := range asMap(serversValue).keys {
					server := asMap(asMap(serversValue).m[name])
					if server == nil {
						continue
					}
					for _, key := range slices.Clone(server.keys) {
						if key == "env" || key == "headers" || secretKey.MatchString(key) {
							server.del(key)
							stripped[name] = append(stripped[name], key)
						}
					}
					dropSecrets(server)
					for _, key := range []string{"args", "url"} {
						if hasSecretString(server.m[key]) {
							server.del(key)
							stripped[name] = append(stripped[name], key)
						}
					}
					servers.set(name, server)
				}
			}
		}
	}
	return json.Marshal(struct {
		Servers  *omap               `json:"servers"`
		Stripped map[string][]string `json:"stripped"`
	}{servers, stripped})
}

// dropSecrets retire à toute profondeur les clés dont le nom ressemble à un secret.
func dropSecrets(value any) {
	switch typedValue := value.(type) {
	case *omap:
		for _, key := range slices.Clone(typedValue.keys) {
			if secretKey.MatchString(key) {
				typedValue.del(key)
			} else {
				dropSecrets(typedValue.m[key])
			}
		}
	case []any:
		for _, element := range typedValue {
			dropSecrets(element)
		}
	}
}

// hasSecretString : une chaîne (ou un élément de liste) qui ressemble à un secret, p. ex. « --api-key » ou « ?token=… ».
func hasSecretString(value any) bool {
	switch typedValue := value.(type) {
	case string:
		return secretKey.MatchString(typedValue)
	case []any:
		return slices.ContainsFunc(typedValue, hasSecretString)
	}
	return false
}
