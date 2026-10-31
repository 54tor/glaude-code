package main

// Synchronisation des réglages entre deux dossiers de configuration Claude (homes.go) : réglages de settings.json (clé par clé),
// règles de permission, CLAUDE.md, rules/, commandes et skills. Chaque élément se compare, se prévisualise et se choisit
// individuellement ; rien n'est écrit sans sauvegarde du fichier remplacé. Les secrets (env, jetons, helpers d'authentification) ne
// sont jamais proposés. Les serveurs MCP vivent dans .claude.json, que les sessions ouvertes réécrivent en continu : ils restent hors
// de cette copie (`claude mcp add` s'en charge).

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type SyncItem struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // setting, perm, file, skill
	Label  string `json:"label"`
	Status string `json:"status"`         // new (absent de la cible), diff, same
	Risk   string `json:"risk,omitempty"` // exec : contient des commandes qui s'exécuteront
	Merge  bool   `json:"merge"`          // « fusionner » possible (listes, objets) en plus de « remplacer »
	Cat    string `json:"cat,omitempty"`
	From   string `json:"from"`
	To     string `json:"to"`
}

type SyncPick struct{ ID, Mode string } // Mode : merge ou replace

var (
	// jamais copiés : secrets et programmes qui en produisent
	syncSkip     = map[string]bool{"env": true, "apiKeyHelper": true, "awsAuthRefresh": true, "awsCredentialExport": true, "otelHeadersHelper": true, "permissions": true}
	syncPermKind = []string{"allow", "ask", "deny", "additionalDirectories"}
	syncSegRe    = regexp.MustCompile(`^[\w][\w. -]*$`)
	// clés de settings.json gérées à part quand skipPlugin est actif
	syncPluginKeys = map[string]bool{"enabledPlugins": true, "extraKnownMarketplaces": true}
)

const (
	syncFileMax  = 256 << 10
	syncSkillMax = 2 << 20
	syncPreview  = 4000
	// syncDirMaxDepth : profondeur de dossiers parcourue sous rules/ et commands/.
	syncDirMaxDepth   = 3
	syncSkillMaxFiles = 100
)

func homeDir(name string) (string, error) {
	if name == "" {
		return cfgDir(), nil
	}
	if home, found := homeByName(name); found {
		return home.Dir, nil
	}
	return "", terr("srv.homesync.dossierDeConfigurationInconnu", "A1", name)
}

func syncSettings(dir string) (*omap, []byte, error) {
	path := filepath.Join(dir, "settings.json")
	if symlinkErr := notSymlink(path); symlinkErr != nil {
		return nil, nil, symlinkErr
	}
	content, readErr := readTrusted(path)
	if readErr != nil {
		return newOmap(), nil, nil
	}
	if strings.TrimSpace(string(content)) == "" {
		return newOmap(), content, nil
	}
	parsed, readErr := parseOrdered(content)
	if readErr != nil {
		return nil, nil, terr("srv.homesync.estIllisible", "A1", path, "A2", readErr.Error())
	}
	settings := asMap(parsed)
	if settings == nil {
		return nil, nil, terr("srv.homesync.unObjetJsonEst", "A1", path)
	}
	return settings, content, nil
}

func cutSync(text string) string {
	if len(text) > syncPreview {
		return text[:syncPreview] + "\n…"
	}
	return text
}

// syncPickNeedsConfig : seuls CLAUDE.md et rules/*.md sont du texte inerte ; réglages, permissions, skills et commandes
// font exécuter du code par Claude Code. Un identifiant non reconnu est traité comme tel.
func syncPickNeedsConfig(id string) bool {
	rel, isFile := strings.CutPrefix(id, "file:")
	isCleanPath := !strings.Contains(rel, "\\") && path.Clean(rel) == rel
	if !isFile || !isCleanPath {
		return true
	}
	isRuleFile := strings.HasPrefix(rel, "rules/") && strings.HasSuffix(rel, ".md")
	return rel != "CLAUDE.md" && !isRuleFile
}

// syncFile : fichiers texte copiables d'un dossier de configuration, par chemin relatif (CLAUDE.md, rules/*.md, commands/**.md).
func syncFiles(dir string) map[string]string {
	files := map[string]string{}
	read := func(rel string) {
		filePath := filepath.Join(dir, rel)
		fileInfo, lstatErr := os.Lstat(filePath)
		if lstatErr != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() > syncFileMax {
			return
		}
		if content, readErr := readTrusted(filePath); readErr == nil {
			files[rel] = string(content)
		}
	}
	read("CLAUDE.md")
	for _, sub := range []string{"rules", "commands"} {
		base := walkRoot(filepath.Join(dir, sub))
		filepath.WalkDir(base, func(walkedPath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			relativeToBase, _ := filepath.Rel(base, walkedPath)
			rel := filepath.Join(sub, relativeToBase)
			if entry.IsDir() {
				isTooDeep := strings.Count(rel, string(filepath.Separator)) > syncDirMaxDepth
				isHiddenSubdir := walkedPath != base && strings.HasPrefix(entry.Name(), ".")
				if isTooDeep || isHiddenSubdir {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(entry.Name(), ".md") && !strings.HasPrefix(entry.Name(), ".") && syncRelOK(rel) {
				read(rel)
			}
			return nil
		})
	}
	return files
}

// syncSkills : pour chaque skill (dossier de skills/), ses fichiers réguliers et leur contenu. Un skill trop gros est ignoré.
func syncSkills(dir string) map[string]map[string]string {
	skills := map[string]map[string]string{}
	entries, _ := readDirTrusted(filepath.Join(dir, "skills"))
	for _, entry := range entries {
		if !entry.IsDir() || !syncSegRe.MatchString(entry.Name()) {
			continue
		}
		root := walkRoot(filepath.Join(dir, "skills", entry.Name()))
		files, total, withinLimits := map[string]string{}, int64(0), true
		filepath.WalkDir(root, func(walkedPath string, walkedEntry fs.DirEntry, walkErr error) error {
			if walkErr != nil || walkedEntry.IsDir() {
				return nil
			}
			fileInfo, walkErr := walkedEntry.Info()
			if walkErr != nil || !fileInfo.Mode().IsRegular() {
				return nil
			}
			total += fileInfo.Size()
			if total > syncSkillMax || len(files) >= syncSkillMaxFiles {
				withinLimits = false
				return fs.SkipAll
			}
			rel, _ := filepath.Rel(root, walkedPath)
			if content, readErr := readTrusted(walkedPath); readErr == nil && syncRelOK(rel) {
				files[rel] = string(content)
			}
			return nil
		})
		if withinLimits && len(files) > 0 {
			skills[entry.Name()] = files
		}
	}
	return skills
}

// syncRelOK : un chemin relatif sans « .. », sans segment caché ni caractère inattendu.
func syncRelOK(rel string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(rel), "/") {
		if !syncSegRe.MatchString(segment) || strings.Contains(segment, "..") {
			return false
		}
	}
	return true
}

func listText(lines []string) string { return strings.Join(lines, "\n") }

// sameSet : mêmes éléments des deux côtés, sans égard à l'ordre ni aux doublons.
func sameSet(left, right []string) bool {
	set := func(values []string) map[string]bool {
		members := map[string]bool{}
		for _, value := range values {
			members[value] = true
		}
		return members
	}
	return maps.Equal(set(left), set(right))
}

func skillText(files map[string]string) string {
	var lines []string
	for name, content := range files {
		lines = append(lines, fmt.Sprintf("%s (%d o)", name, len(content)))
	}
	sort.Strings(lines)
	return listText(lines)
}

func sameFiles(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for name, leftContent := range left {
		if rightContent, found := right[name]; !found || rightContent != leftContent {
			return false
		}
	}
	return true
}

// syncPlan compare deux dossiers de configuration, du point de vue de la copie de from vers to.
func syncPlan(from, to string) ([]SyncItem, error) {
	fromDir, err := homeDir(from)
	if err != nil {
		return nil, err
	}
	toDir, err := homeDir(to)
	if err != nil {
		return nil, err
	}
	return syncPlanDirs(fromDir, toDir, false)
}

// syncPlanDirs est syncPlan sur deux dossiers déjà résolus ; skipPlugin écarte syncPluginKeys.
func syncPlanDirs(fromDir, toDir string, skipPlugin bool) ([]SyncItem, error) {
	if fromDir == toDir {
		return nil, terr("srv.homesync.laSourceEtLa")
	}
	sourceSettings, _, err := syncSettings(fromDir)
	if err != nil {
		return nil, err
	}
	targetSettings, _, err := syncSettings(toDir)
	if err != nil {
		return nil, err
	}
	items := []SyncItem{}
	for _, key := range sourceSettings.keys {
		isSkipped := syncSkip[key] || secretKey.MatchString(key) || (skipPlugin && syncPluginKeys[key])
		if isSkipped {
			continue
		}
		sourceValue, _ := sourceSettings.get(key)
		targetValue, has := targetSettings.get(key)
		syncItem := SyncItem{
			ID:     "set:" + key,
			Kind:   "setting",
			Label:  key,
			Status: "new",
			Merge:  asMap(sourceValue) != nil,
			From:   cutSync(formatOrdered(maskSecrets(sourceValue, key))),
		}
		if has {
			syncItem.To = cutSync(formatOrdered(maskSecrets(targetValue, key)))
			syncItem.Status = "diff"
			if formatOrdered(sourceValue) == formatOrdered(targetValue) {
				syncItem.Status = "same"
			}
		}
		if key == "hooks" || key == "statusLine" {
			syncItem.Risk = "exec"
		}
		items = append(items, syncItem)
	}
	for _, kind := range syncPermKind {
		sourceValue, _ := lookup(sourceSettings, "permissions", kind)
		targetValue, _ := lookup(targetSettings, "permissions", kind)
		sourceRules, targetRules := strList(sourceValue), strList(targetValue)
		if len(sourceRules) == 0 {
			continue
		}
		syncItem := SyncItem{
			ID:     "perm:" + kind,
			Kind:   "perm",
			Label:  "permissions." + kind,
			Status: "new",
			Merge:  true,
			From:   cutSync(listText(sourceRules)),
			To:     cutSync(listText(targetRules)),
		}
		if len(targetRules) > 0 {
			syncItem.Status = "diff"
			if sameSet(sourceRules, targetRules) {
				syncItem.Status = "same"
			}
		}
		items = append(items, syncItem)
	}
	sourceFiles, targetFiles := syncFiles(fromDir), syncFiles(toDir)
	var rels []string
	for relativePath := range sourceFiles {
		rels = append(rels, relativePath)
	}
	sort.Strings(rels)
	for _, relativePath := range rels {
		syncItem := SyncItem{
			ID:     "file:" + filepath.ToSlash(relativePath),
			Kind:   "file",
			Label:  filepath.ToSlash(relativePath),
			Status: "new",
			From:   cutSync(sourceFiles[relativePath]),
		}
		if targetContent, found := targetFiles[relativePath]; found {
			syncItem.To, syncItem.Status = cutSync(targetContent), "diff"
			if targetContent == sourceFiles[relativePath] {
				syncItem.Status = "same"
			}
		}
		items = append(items, syncItem)
	}
	sourceSkills, targetSkills := syncSkills(fromDir), syncSkills(toDir)
	var names []string
	for name := range sourceSkills {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		syncItem := SyncItem{
			ID:     "skill:" + name,
			Kind:   "skill",
			Label:  "skills/" + name,
			Status: "new",
			From:   skillText(sourceSkills[name]),
		}
		if targetSkillFiles, found := targetSkills[name]; found {
			syncItem.To, syncItem.Status = skillText(targetSkillFiles), "diff"
			if sameFiles(sourceSkills[name], targetSkillFiles) {
				syncItem.Status = "same"
			}
		}
		items = append(items, syncItem)
	}
	return items, nil
}

// syncBackup copie le fichier existant dans <cible>/gld-web-sync-backups/<horodatage>/<chemin relatif> avant de le remplacer.
func syncBackup(dir, stamp, rel string) error {
	content, err := readTrusted(filepath.Join(dir, rel))
	if err != nil {
		return nil // rien à sauvegarder : le fichier n'existe pas
	}
	return writeConfig(filepath.Join(dir, "gld-web-sync-backups", stamp, rel), content)
}

// syncApply copie les éléments choisis de from vers to. Chaque élément est revalidé côté serveur (le navigateur ne désigne que des
// identifiants du plan). Renvoie un résumé.
func syncApply(from, to string, picks []SyncPick) (string, error) {
	fromDir, err := homeDir(from)
	if err != nil {
		return "", err
	}
	toDir, err := homeDir(to)
	if err != nil {
		return "", err
	}
	return syncApplyDirs(fromDir, toDir, picks, false)
}

// syncApplyDirs est syncApply sur deux dossiers déjà résolus.
func syncApplyDirs(fromDir, toDir string, picks []SyncPick, skipPlugin bool) (string, error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	plan, setupErr := syncPlanDirs(fromDir, toDir, skipPlugin)
	if setupErr != nil {
		return "", setupErr
	}
	byID := map[string]SyncItem{}
	for _, planItem := range plan {
		byID[planItem.ID] = planItem
	}
	sourceSettings, _, _ := syncSettings(fromDir)
	targetSettings, targetOriginal, setupErr := syncSettings(toDir)
	if setupErr != nil {
		return "", setupErr
	}
	targetWasHandEdited := targetOriginal != nil && strings.TrimSpace(string(targetOriginal)) != "" && formatOrdered(targetSettings)+"\n" != string(targetOriginal)
	if targetWasHandEdited {
		return "", terr("srv.homesync.leSettingsJsonDe")
	}
	sourceFiles, sourceSkills, targetSkills := syncFiles(fromDir), syncSkills(fromDir), syncSkills(toDir)
	stamp := time.Now().Format("20060102-150405.000000") // deux synchros dans la même seconde ne partagent pas leur sauvegarde
	settingsTouched, copiedCount := false, 0
	type fileOut struct {
		rel, content string
		exec         bool
	}
	var writes []fileOut
	var removes []string // fichiers d'un skill remplacé absents de la source
	for _, pick := range picks {
		planItem, found := byID[pick.ID]
		if !found || planItem.Status == "same" {
			continue
		}
		mode := pick.Mode
		if mode != "merge" || !planItem.Merge {
			mode = "replace"
		}
		kind, name, _ := strings.Cut(pick.ID, ":")
		switch kind {
		case "set":
			sourceValue, _ := sourceSettings.get(name)
			targetValue, has := targetSettings.get(name)
			if mode == "merge" && has {
				if sourceObject, targetObject := asMap(sourceValue), asMap(targetValue); sourceObject != nil && targetObject != nil {
					for _, key := range sourceObject.keys {
						if _, dup := targetObject.get(key); !dup {
							targetObject.set(key, sourceObject.m[key])
						}
					}
					break
				}
			}
			targetSettings.set(name, sourceValue)
		case "perm":
			sourceValue, _ := lookup(sourceSettings, "permissions", name)
			targetValue, _ := lookup(targetSettings, "permissions", name)
			next := strList(sourceValue)
			if mode == "merge" {
				have := map[string]bool{}
				next = strList(targetValue)
				for _, rule := range next {
					have[rule] = true
				}
				for _, rule := range strList(sourceValue) {
					if !have[rule] {
						next = append(next, rule)
					}
				}
			}
			setRules(targetSettings, name, next)
		case "file":
			writes = append(writes, fileOut{filepath.FromSlash(name), sourceFiles[filepath.FromSlash(name)], false})
		case "skill":
			for rel, content := range sourceSkills[name] {
				skillFilePath := filepath.Join("skills", name, rel)
				fileInfo, lstatErr := os.Lstat(filepath.Join(fromDir, skillFilePath)) // Lstat : un lien n'apporte jamais son mode
				isExecutable := lstatErr == nil && fileInfo.Mode().IsRegular() && fileInfo.Mode()&executableBits != 0
				writes = append(writes, fileOut{skillFilePath, content, isExecutable})
			}
			for rel := range targetSkills[name] {
				if _, keep := sourceSkills[name][rel]; !keep {
					removes = append(removes, filepath.Join("skills", name, rel))
				}
			}
		default:
			continue
		}
		if kind == "set" || kind == "perm" {
			settingsTouched = true
		}
		copiedCount++
	}
	// Tout est validé puis sauvegardé avant la première écriture : un refus n'écrit rien, une panne en cours de route laisse une
	// sauvegarde complète de ce qui a pu changer.
	touched := []string{}
	if settingsTouched {
		touched = append(touched, "settings.json")
	}
	for _, pendingWrite := range writes {
		touched = append(touched, pendingWrite.rel)
	}
	touched = append(touched, removes...)
	for _, rel := range touched {
		if !syncRelOK(rel) {
			return "", terr("srv.homesync.cheminRefuse", "A1", rel)
		}
	}
	for _, rel := range touched {
		if backupErr := syncBackup(toDir, stamp, rel); backupErr != nil {
			return "", terr("srv.homesync.sauvegardeImpossible", "A1", backupErr.Error())
		}
	}
	if settingsTouched {
		if settingsWriteErr := writeConfig(filepath.Join(toDir, "settings.json"), []byte(formatOrdered(targetSettings)+"\n")); settingsWriteErr != nil {
			return "", settingsWriteErr
		}
	}
	for _, pendingWrite := range writes {
		if fileWriteErr := writeConfigExec(filepath.Join(toDir, pendingWrite.rel), []byte(pendingWrite.content), pendingWrite.exec); fileWriteErr != nil {
			return "", fileWriteErr
		}
	}
	for _, rel := range removes {
		if removeErr := rRemove(toDir, filepath.Join(toDir, rel)); removeErr != nil {
			return "", removeErr
		}
	}
	return L("srv.homesync.copied", "A1", copiedCount, "A2", filepath.Join(toDir, "gld-web-sync-backups", stamp)), nil
}
