package main

import (
	"encoding/json"
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
func exportBackup(cats []string) ([]byte, int, error) {
	if len(cats) == 0 {
		return nil, 0, terr("srv.backup_export.catsVides")
	}
	for _, c := range cats {
		if !slices.Contains(backupCats, c) {
			return nil, 0, terr("srv.backup_export.catInconnue", "A1", c)
		}
	}
	has := func(c string) bool { return slices.Contains(cats, c) }
	files := map[string][]byte{}
	dir := cfgDir()

	if has("settings") {
		o, _, err := syncSettings(dir)
		if err != nil {
			return nil, 0, err
		}
		for _, k := range slices.Clone(o.keys) {
			if (syncSkip[k] && k != "permissions") || syncPluginKeys[k] || secretKey.MatchString(k) {
				o.del(k)
			}
		}
		if len(o.keys) > 0 {
			files["home/settings.json"] = []byte(formatOrdered(o))
		}
	}
	if has("skills") || has("memory") {
		for rel, c := range syncFiles(dir) {
			rel = filepath.ToSlash(rel)
			if isCmd := strings.HasPrefix(rel, "commands/"); (isCmd && has("skills")) || (!isCmd && has("memory")) {
				files["home/"+rel] = []byte(c)
			}
		}
	}
	if has("skills") {
		for n, fs := range syncSkills(dir) {
			for rel, c := range fs {
				files["home/skills/"+n+"/"+filepath.ToSlash(rel)] = []byte(c)
			}
		}
		snips, _ := listSnippets()
		for _, s := range snips {
			if s.Scope == "" {
				continue
			}
			if c, err := readSnippet(s.Key); err == nil {
				files["projects/"+s.Scope+"/commands/"+s.Path] = []byte(c)
			}
		}
	}
	if has("memory") {
		_, projects, _ := memList()
		for _, p := range projects {
			for _, f := range p.Files {
				// m/ : mémoire de Claude ; p/ et r/ vivent dans le dépôt du projet
				if !strings.HasPrefix(f.Key, "m/") {
					continue
				}
				if c, err := memRead(f.Key); err == nil {
					files["projects/"+p.Dir+"/memory/"+f.Name] = []byte(c)
				}
			}
		}
	}
	if has("plugins") {
		inst, _, err := listPlugins("", 1<<30)
		if err != nil {
			return nil, 0, err
		}
		ms, err := listMarketplaces()
		if err != nil {
			return nil, 0, err
		}
		b, _ := json.Marshal(pluginsDoc{Marketplaces: ms, Plugins: inst})
		files["plugins.json"] = b
	}
	if has("mcp") {
		b, err := exportMcp()
		if err != nil {
			return nil, 0, err
		}
		files["mcp.json"] = b
	}
	z, err := writeBackupZip(files, BackupManifest{Format: backupFormat, Gld: version, Date: time.Now().UTC().Format(time.RFC3339), Cats: cats})
	return z, len(files), err
}

// exportMcp : serveurs de portée utilisateur sans env, headers ni clé qui ressemble à un secret ; ce qui a été retiré est listé.
func exportMcp() ([]byte, error) {
	servers, stripped := newOmap(), map[string][]string{}
	if b, err := readTrusted(claudeJSON()); err == nil {
		if v, err := parseOrdered(b); err == nil {
			if s, ok := lookup(v, "mcpServers"); ok && asMap(s) != nil {
				for _, name := range asMap(s).keys {
					o := asMap(asMap(s).m[name])
					if o == nil {
						continue
					}
					for _, k := range slices.Clone(o.keys) {
						if k == "env" || k == "headers" || secretKey.MatchString(k) {
							o.del(k)
							stripped[name] = append(stripped[name], k)
						}
					}
					servers.set(name, o)
				}
			}
		}
	}
	return json.Marshal(struct {
		Servers  *omap               `json:"servers"`
		Stripped map[string][]string `json:"stripped"`
	}{servers, stripped})
}
