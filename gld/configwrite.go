package main

// Écriture des réglages : un petit ensemble de modifications permises (réglages simples, variables d'environnement,
// plugins activés, règles de permission), appliquées sans réordonner ni reformater le fichier, avec aperçu des
// différences et sauvegarde horodatée avant toute écriture.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type CfgEdit struct {
	File  string   `json:"file"`
	Op    string   `json:"op"` // set, delete, add, remove
	Path  []string `json:"path"`
	Value any      `json:"value"`
	Dry   bool     `json:"dry"`
}

// Réglages simples modifiables : clé -> type de valeur.
var simpleKeys = map[string]string{
	"model": "string", "effortLevel": "string", "theme": "string", "language": "string", "editorMode": "string",
	"preferredNotifChannel": "string", "teammateMode": "string", "dialogExpiry": "string", "feedbackDrafts": "string",
	"crossSessionInbound": "string", "cleanupPeriodDays": "number",
	"autoCompactEnabled": "bool", "promptSuggestionEnabled": "bool", "awaySummaryEnabled": "bool", "idleCompaction": "bool",
	"fileCheckpointingEnabled": "bool", "remoteControlAtStartup": "bool", "skipDangerousModePermissionPrompt": "bool",
	"switchModelsOnFlag": "bool", "inputNeededNotifEnabled": "bool", "agentPushNotifEnabled": "bool",
	"useAutoModeDuringPlan": "bool", "workflowKeywordTriggerEnabled": "bool", "skipWorkflowUsageWarning": "bool",
}

var (
	envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,99}$`)
	wordRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,40}$`)
	// nom d'un skill ou d'un serveur MCP, comme Claude Code l'écrit dans skillOverrides et disabledMcpjsonServers
	skillNameRe = regexp.MustCompile(`^[\w.:-]{1,100}$`)
	skillModes  = map[string]bool{"on": true, "name-only": true, "user-invocable-only": true, "off": true}
)

// validate vérifie que la modification fait partie de celles permises, et normalise sa valeur.
func (edit *CfgEdit) validate() error {
	path := edit.Path
	switch {
	case len(path) == 1 && simpleKeys[path[0]] != "":
		if edit.Op == "delete" {
			return nil
		}
		if edit.Op != "set" {
			return terr("srv.configwrite.operationRefusee")
		}
		switch simpleKeys[path[0]] {
		case "bool":
			if _, isBool := edit.Value.(bool); !isBool {
				return terr("srv.configwrite.unBooleenEstAttendu", "A1", path[0])
			}
		case "number":
			number, isNumber := edit.Value.(float64)
			if !isNumber || number < 0 || number > 1e9 || number != float64(int64(number)) {
				return terr("srv.configwrite.unEntierPositifEst", "A1", path[0])
			}
			edit.Value = json.Number(fmt.Sprint(int64(number)))
		default:
			text, isString := edit.Value.(string)
			if !isString || text == "" || len(text) > 100 || strings.ContainsAny(text, "\n\r") {
				return terr("srv.configwrite.unTexteCourtEst", "A1", path[0])
			}
		}
	case len(path) == 2 && path[0] == "env" && envName.MatchString(path[1]):
		if edit.Op == "delete" {
			return nil
		}
		value, isString := edit.Value.(string)
		if edit.Op != "set" || !isString || len(value) > 2000 || strings.ContainsAny(value, "\n\r") {
			return terr("srv.configwrite.variableDEnvironnementUn")
		}
	case len(path) == 2 && path[0] == "enabledPlugins" && pluginIDRe.MatchString(path[1]):
		if edit.Op == "delete" {
			return nil
		}
		if _, isBool := edit.Value.(bool); !isBool || edit.Op != "set" {
			return terr("srv.configwrite.pluginUnBooleenEst")
		}
	case len(path) == 2 && path[0] == "permissions" && (path[1] == "allow" || path[1] == "deny" || path[1] == "ask"):
		rule, isString := edit.Value.(string)
		if (edit.Op != "add" && edit.Op != "remove") || !isString || strings.TrimSpace(rule) == "" || len(rule) > 300 || strings.ContainsAny(rule, "\n\r") {
			return terr("srv.configwrite.regleDePermissionUn")
		}
		edit.Value = strings.TrimSpace(rule)
	case len(path) == 2 && path[0] == "permissions" && path[1] == "additionalDirectories":
		// dossiers accessibles en plus du dossier de travail : un chemin sur une ligne (absolu, « ~/… » ou relatif au projet)
		directory, isString := edit.Value.(string)
		directory = strings.TrimSpace(directory)
		if (edit.Op != "add" && edit.Op != "remove") || !isString || directory == "" || len(directory) > 500 || strings.ContainsAny(directory, "\n\r\x00") {
			return terr("srv.configwrite.dossierSupplementaireUnChemin")
		}
		edit.Value = directory
	case len(path) == 2 && path[0] == "skillOverrides" && skillNameRe.MatchString(path[1]):
		if edit.Op == "delete" {
			return nil
		}
		if mode, isString := edit.Value.(string); edit.Op != "set" || !isString || !skillModes[mode] {
			return terr("srv.configwrite.cetteModificationNEst", "A1", strings.Join(path, "."))
		}
	case len(path) == 1 && path[0] == "disabledMcpjsonServers":
		if serverName, isString := edit.Value.(string); (edit.Op != "add" && edit.Op != "remove") || !isString || !skillNameRe.MatchString(serverName) {
			return terr("srv.configwrite.cetteModificationNEst", "A1", path[0])
		}
	case len(path) == 2 && path[0] == "permissions" && path[1] == "defaultMode":
		if edit.Op == "delete" {
			return nil
		}
		if mode, isString := edit.Value.(string); edit.Op != "set" || !isString || !wordRe.MatchString(mode) {
			return terr("srv.configwrite.modeParDefautUn")
		}
	default:
		return terr("srv.configwrite.cetteModificationNEst", "A1", strings.Join(path, "."))
	}
	return nil
}

// parent descend jusqu'à l'objet qui contient la dernière clé (en le créant au besoin).
func (orderedMap *omap) parent(path []string, create bool) *omap {
	current := orderedMap
	for _, key := range path[:len(path)-1] {
		next, found := current.get(key)
		child := asMap(next)
		if !found || child == nil {
			if !create {
				return nil
			}
			child = newOmap()
			current.set(key, child)
		}
		current = child
	}
	return current
}

func (edit *CfgEdit) apply(doc *omap) error {
	lastKey := edit.Path[len(edit.Path)-1]
	parentMap := doc.parent(edit.Path, edit.Op != "delete" && edit.Op != "remove")
	switch edit.Op {
	case "set":
		parentMap.set(lastKey, edit.Value)
	case "delete":
		if parentMap == nil {
			return nil
		}
		parentMap.del(lastKey)
		if len(edit.Path) == 2 { // un conteneur devenu vide disparaît
			if container := asMap(doc.m[edit.Path[0]]); container != nil && len(container.keys) == 0 {
				doc.del(edit.Path[0])
			}
		}
	case "add", "remove":
		var list []any
		if parentMap != nil {
			list, _ = parentMap.m[lastKey].([]any)
		}
		matchIndex := -1
		for itemIndex, listItem := range list {
			if text, _ := listItem.(string); text == edit.Value {
				matchIndex = itemIndex
			}
		}
		if edit.Op == "add" {
			if matchIndex >= 0 {
				return terr("srv.configwrite.cetteRegleExisteDeja")
			}
			parentMap.set(lastKey, append(list, edit.Value))
		} else {
			if matchIndex < 0 {
				return terr("srv.configwrite.cetteRegleNExiste")
			}
			list = append(list[:matchIndex], list[matchIndex+1:]...)
			if len(list) == 0 {
				parentMap.del(lastKey)
				if container := asMap(doc.m[edit.Path[0]]); container != nil && len(container.keys) == 0 {
					doc.del(edit.Path[0])
				}
			} else {
				parentMap.set(lastKey, list)
			}
		}
	}
	return nil
}

func backupsDir() string { return filepath.Join(cfgDir(), "gld-web-config-backups") }

func backupName(id string) string {
	return time.Now().Format("20060102-150405.000000") + "__" + strings.ReplaceAll(id, "/", "~") + ".json"
}

const maxConfigBackups = 100

var backupRe = regexp.MustCompile(`^(\d{8}-\d{6}\.\d{6})__([\w~.-]+)\.json$`)

func saveBackup(id string, content []byte) error {
	if err := rMkdirAll(cfgDir(), backupsDir(), 0o700); err != nil {
		return err
	}
	for {
		backupFile, err := rOpenFile(cfgDir(), filepath.Join(backupsDir(), backupName(id)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) { // même microseconde : on recommence, jamais d'écrasement d'une sauvegarde
			continue
		}
		if err != nil {
			return err
		}
		_, writeErr := backupFile.Write(content)
		if closeErr := backupFile.Close(); writeErr != nil || closeErr != nil {
			return errors.Join(writeErr, closeErr)
		}
		break
	}
	dirEntries, _ := readDirTrusted(backupsDir()) // les maxConfigBackups dernières seulement ; un fichier étranger au dossier n'est jamais touché
	var backups []os.DirEntry
	for _, dirEntry := range dirEntries {
		if backupRe.MatchString(dirEntry.Name()) {
			backups = append(backups, dirEntry)
		}
	}
	if len(backups) > maxConfigBackups {
		sort.Slice(backups, func(left, right int) bool {
			return backups[left].Name() < backups[right].Name()
		})
		for _, oldBackup := range backups[:len(backups)-maxConfigBackups] {
			rRemove(cfgDir(), filepath.Join(backupsDir(), oldBackup.Name()))
		}
	}
	return nil
}

// notSymlink refuse un fichier de réglages (ou son dossier) qui est un lien symbolique : l'écriture le remplacerait,
// ou suivrait le lien vers un fichier qu'on n'a pas voulu toucher.
func notSymlink(path string) error {
	for _, candidate := range []string{filepath.Dir(path), path} {
		if fileInfo, err := os.Lstat(candidate); err == nil && fileInfo.Mode()&os.ModeSymlink != 0 {
			return terr("srv.configwrite.estUnLienSymbolique", "A1", candidate)
		}
	}
	return nil
}

// writeConfig remplace le fichier de façon atomique, droits conservés (0600 par défaut). Le fichier temporaire a un nom
// aléatoire et est créé en exclusivité : un lien déposé à sa place ne peut pas détourner l'écriture.
func writeConfig(path string, content []byte) error { return writeConfigExec(path, content, false) }

// writeConfigExec : executable ajoute le bit exécutable du propriétaire, posé sur le fichier temporaire avant le renommage, jamais par nom
// sur la cible.
func writeConfigExec(path string, content []byte, executable bool) error {
	if err := notSymlink(path); err != nil {
		return err
	}
	// Sous cfgDir(), tout le chemin est confiné ; ailleurs (réglages d'un projet), le dossier du fichier sert de racine.
	base := cfgDir()
	if _, relErr := relIn(base, path); relErr != nil {
		base = filepath.Dir(path)
		if mkdirErr := os.MkdirAll(base, 0o755); mkdirErr != nil {
			return mkdirErr
		}
		base = filepath.Dir(base) // .claude/ lui-même reste vérifié par la racine
	}
	if err := rMkdirAll(base, filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if configRoot, rel, confineErr := confine(base, path); confineErr == nil {
		if fileInfo, statErr := configRoot.Lstat(rel); statErr == nil && fileInfo.Mode().IsRegular() {
			mode = fileInfo.Mode().Perm() & 0o777
		}
		configRoot.Close()
	}
	if executable {
		mode |= 0o100
	}
	return rWriteAtomic(base, path, content, mode)
}

// configEdit applique une modification : renvoie le texte avant et après ; n'écrit que si edit.Dry est faux.
// cfgMu sérialise les lecture-modification-écriture des réglages : deux requêtes simultanées perdraient une modification.
var cfgMu sync.Mutex

func configEdit(edit CfgEdit) (before, after string, err error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if err = edit.validate(); err != nil {
		return
	}
	path, err := configPath(edit.File)
	if err != nil {
		return
	}
	if err = notSymlink(path); err != nil {
		return
	}
	original, readErr := readTrusted(path)
	exists := readErr == nil
	text := string(original)
	if !exists || strings.TrimSpace(text) == "" {
		text = "{}\n"
	}
	parsed, err := parseOrdered([]byte(text))
	if err != nil {
		return "", "", terr("srv.configwrite.fichierIllisible", "A1", err.Error())
	}
	doc := asMap(parsed)
	if doc == nil {
		return "", "", terr("srv.configwrite.leFichierDoitContenir")
	}
	if formatOrdered(doc) != strings.TrimRight(text, "\n") {
		return "", "", terr("srv.configwrite.miseEnFormeParticuliere")
	}
	before = text
	if err = edit.apply(doc); err != nil {
		return "", "", err
	}
	after = formatOrdered(doc) + "\n"
	if edit.Dry || after == before {
		return before, after, nil
	}
	if exists {
		if err = saveBackup(edit.File, original); err != nil {
			return "", "", terr("srv.homesync.sauvegardeImpossible", "A1", err.Error())
		}
	}
	return before, after, writeConfig(path, []byte(after))
}

type BackupInfo struct {
	Name string `json:"name"`
	At   string `json:"at"`
	File string `json:"file"` // identifiant du fichier de réglages
	KB   int64  `json:"kb"`
}

func listBackups() []BackupInfo {
	backups := []BackupInfo{}
	entries, _ := readDirTrusted(backupsDir())
	for _, entry := range entries {
		if nameMatch := backupRe.FindStringSubmatch(entry.Name()); nameMatch != nil {
			fileInfo, _ := entry.Info()
			var sizeKB int64
			if fileInfo != nil {
				sizeKB = fileInfo.Size() / 1024
			}
			backups = append(backups, BackupInfo{entry.Name(), nameMatch[1], strings.ReplaceAll(nameMatch[2], "~", "/"), sizeKB})
		}
	}
	sort.Slice(backups, func(left, right int) bool {
		return backups[left].Name > backups[right].Name
	})
	return backups
}

// restoreBackup remet une sauvegarde en place (l'état actuel est sauvegardé à son tour).
func restoreBackup(name string) error {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	nameMatch := backupRe.FindStringSubmatch(name)
	if nameMatch == nil {
		return terr("srv.configwrite.sauvegardeInvalide")
	}
	id := strings.ReplaceAll(nameMatch[2], "~", "/")
	path, err := configPath(id)
	if err != nil {
		return err
	}
	content, err := readTrusted(filepath.Join(backupsDir(), name))
	if err != nil {
		return err
	}
	if _, parseErr := parseOrdered(content); parseErr != nil {
		return terr("srv.configwrite.laSauvegardeNEst")
	}
	if current, readErr := readTrusted(path); readErr == nil {
		if saveErr := saveBackup(id, current); saveErr != nil {
			return saveErr
		}
	}
	return writeConfig(path, content)
}
