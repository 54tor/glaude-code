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
)

// validate vérifie que la modification fait partie de celles permises, et normalise sa valeur.
func (e *CfgEdit) validate() error {
	p := e.Path
	switch {
	case len(p) == 1 && simpleKeys[p[0]] != "":
		if e.Op == "delete" {
			return nil
		}
		if e.Op != "set" {
			return terr("srv.configwrite.operationRefusee")
		}
		switch simpleKeys[p[0]] {
		case "bool":
			if _, ok := e.Value.(bool); !ok {
				return terr("srv.configwrite.unBooleenEstAttendu", "A1", p[0])
			}
		case "number":
			f, ok := e.Value.(float64)
			if !ok || f < 0 || f > 1e9 || f != float64(int64(f)) {
				return terr("srv.configwrite.unEntierPositifEst", "A1", p[0])
			}
			e.Value = json.Number(fmt.Sprint(int64(f)))
		default:
			s, ok := e.Value.(string)
			if !ok || s == "" || len(s) > 100 || strings.ContainsAny(s, "\n\r") {
				return terr("srv.configwrite.unTexteCourtEst", "A1", p[0])
			}
		}
	case len(p) == 2 && p[0] == "env" && envName.MatchString(p[1]):
		if e.Op == "delete" {
			return nil
		}
		s, ok := e.Value.(string)
		if e.Op != "set" || !ok || len(s) > 2000 || strings.ContainsAny(s, "\n\r") {
			return terr("srv.configwrite.variableDEnvironnementUn")
		}
	case len(p) == 2 && p[0] == "enabledPlugins" && pluginIDRe.MatchString(p[1]):
		if e.Op == "delete" {
			return nil
		}
		if _, ok := e.Value.(bool); !ok || e.Op != "set" {
			return terr("srv.configwrite.pluginUnBooleenEst")
		}
	case len(p) == 2 && p[0] == "permissions" && (p[1] == "allow" || p[1] == "deny" || p[1] == "ask"):
		s, ok := e.Value.(string)
		if (e.Op != "add" && e.Op != "remove") || !ok || strings.TrimSpace(s) == "" || len(s) > 300 || strings.ContainsAny(s, "\n\r") {
			return terr("srv.configwrite.regleDePermissionUn")
		}
		e.Value = strings.TrimSpace(s)
	case len(p) == 2 && p[0] == "permissions" && p[1] == "additionalDirectories":
		// dossiers accessibles en plus du dossier de travail : un chemin sur une ligne (absolu, « ~/… » ou relatif au projet)
		s, ok := e.Value.(string)
		s = strings.TrimSpace(s)
		if (e.Op != "add" && e.Op != "remove") || !ok || s == "" || len(s) > 500 || strings.ContainsAny(s, "\n\r\x00") {
			return terr("srv.configwrite.dossierSupplementaireUnChemin")
		}
		e.Value = s
	case len(p) == 2 && p[0] == "permissions" && p[1] == "defaultMode":
		if e.Op == "delete" {
			return nil
		}
		if s, ok := e.Value.(string); e.Op != "set" || !ok || !wordRe.MatchString(s) {
			return terr("srv.configwrite.modeParDefautUn")
		}
	default:
		return terr("srv.configwrite.cetteModificationNEst", "A1", strings.Join(p, "."))
	}
	return nil
}

// parent descend jusqu'à l'objet qui contient la dernière clé (en le créant au besoin).
func (o *omap) parent(path []string, create bool) *omap {
	cur := o
	for _, k := range path[:len(path)-1] {
		next, ok := cur.get(k)
		n := asMap(next)
		if !ok || n == nil {
			if !create {
				return nil
			}
			n = newOmap()
			cur.set(k, n)
		}
		cur = n
	}
	return cur
}

func (e *CfgEdit) apply(doc *omap) error {
	last := e.Path[len(e.Path)-1]
	par := doc.parent(e.Path, e.Op != "delete" && e.Op != "remove")
	switch e.Op {
	case "set":
		par.set(last, e.Value)
	case "delete":
		if par == nil {
			return nil
		}
		par.del(last)
		if len(e.Path) == 2 { // un conteneur devenu vide disparaît
			if c := asMap(doc.m[e.Path[0]]); c != nil && len(c.keys) == 0 {
				doc.del(e.Path[0])
			}
		}
	case "add", "remove":
		var list []any
		if par != nil {
			list, _ = par.m[last].([]any)
		}
		idx := -1
		for i, x := range list {
			if s, _ := x.(string); s == e.Value {
				idx = i
			}
		}
		if e.Op == "add" {
			if idx >= 0 {
				return terr("srv.configwrite.cetteRegleExisteDeja")
			}
			par.set(last, append(list, e.Value))
		} else {
			if idx < 0 {
				return terr("srv.configwrite.cetteRegleNExiste")
			}
			list = append(list[:idx], list[idx+1:]...)
			if len(list) == 0 {
				par.del(last)
				if c := asMap(doc.m[e.Path[0]]); c != nil && len(c.keys) == 0 {
					doc.del(e.Path[0])
				}
			} else {
				par.set(last, list)
			}
		}
	}
	return nil
}

func backupsDir() string { return filepath.Join(cfgDir(), "gld-web-config-backups") }

func backupName(id string) string {
	return time.Now().Format("20060102-150405.000000") + "__" + strings.ReplaceAll(id, "/", "~") + ".json"
}

var backupRe = regexp.MustCompile(`^(\d{8}-\d{6}\.\d{6})__([\w~.-]+)\.json$`)

func saveBackup(id string, content []byte) error {
	if err := os.MkdirAll(backupsDir(), 0o700); err != nil {
		return err
	}
	for {
		f, err := os.OpenFile(filepath.Join(backupsDir(), backupName(id)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) { // même microseconde : on recommence, jamais d'écrasement d'une sauvegarde
			continue
		}
		if err != nil {
			return err
		}
		_, werr := f.Write(content)
		if cerr := f.Close(); werr != nil || cerr != nil {
			return errors.Join(werr, cerr)
		}
		break
	}
	all, _ := os.ReadDir(backupsDir()) // les 100 dernières seulement ; un fichier étranger au dossier n'est jamais touché
	var ents []os.DirEntry
	for _, e := range all {
		if backupRe.MatchString(e.Name()) {
			ents = append(ents, e)
		}
	}
	if len(ents) > 100 {
		sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
		for _, e := range ents[:len(ents)-100] {
			os.Remove(filepath.Join(backupsDir(), e.Name()))
		}
	}
	return nil
}

// notSymlink refuse un fichier de réglages (ou son dossier) qui est un lien symbolique : l'écriture le remplacerait,
// ou suivrait le lien vers un fichier qu'on n'a pas voulu toucher.
func notSymlink(path string) error {
	for _, p := range []string{filepath.Dir(path), path} {
		if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return terr("srv.configwrite.estUnLienSymbolique", "A1", p)
		}
	}
	return nil
}

// writeConfig remplace le fichier de façon atomique, droits conservés (0600 par défaut). Le fichier temporaire a un nom
// aléatoire et est créé en exclusivité : un lien déposé à sa place ne peut pas détourner l'écriture.
// writeSynced : os.WriteFile suivi d'un fsync, pour un fichier temporaire qui remplacera l'original par renommage : sans
// fsync, une coupure juste après le renommage peut laisser un fichier vide à la place de l'original.
func writeSynced(name string, b []byte, mode os.FileMode) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	return errors.Join(err, f.Sync(), f.Close())
}

func writeConfig(path string, content []byte) error {
	if err := notSymlink(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".gld-web-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(content)
	serr := f.Sync() // sans lui, une coupure juste après le renommage peut laisser un settings.json vide
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr, os.Chmod(tmp, mode)); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// configEdit applique une modification : renvoie le texte avant et après ; n'écrit que si e.Dry est faux.
// cfgMu sérialise les lecture-modification-écriture des réglages : deux requêtes simultanées perdraient une modification.
var cfgMu sync.Mutex

func configEdit(e CfgEdit) (before, after string, err error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if err = e.validate(); err != nil {
		return
	}
	path, err := configPath(e.File)
	if err != nil {
		return
	}
	if err = notSymlink(path); err != nil {
		return
	}
	orig, rerr := os.ReadFile(path)
	exists := rerr == nil
	text := string(orig)
	if !exists || strings.TrimSpace(text) == "" {
		text = "{}\n"
	}
	v, err := parseOrdered([]byte(text))
	if err != nil {
		return "", "", terr("srv.configwrite.fichierIllisible", "A1", err.Error())
	}
	doc := asMap(v)
	if doc == nil {
		return "", "", terr("srv.configwrite.leFichierDoitContenir")
	}
	if formatOrdered(doc) != strings.TrimRight(text, "\n") {
		return "", "", terr("srv.configwrite.miseEnFormeParticuliere")
	}
	before = text
	if err = e.apply(doc); err != nil {
		return "", "", err
	}
	after = formatOrdered(doc) + "\n"
	if e.Dry || after == before {
		return before, after, nil
	}
	if exists {
		if err = saveBackup(e.File, orig); err != nil {
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
	out := []BackupInfo{}
	ents, _ := os.ReadDir(backupsDir())
	for _, e := range ents {
		if m := backupRe.FindStringSubmatch(e.Name()); m != nil {
			st, _ := e.Info()
			var kb int64
			if st != nil {
				kb = st.Size() / 1024
			}
			out = append(out, BackupInfo{e.Name(), m[1], strings.ReplaceAll(m[2], "~", "/"), kb})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out
}

// restoreBackup remet une sauvegarde en place (l'état actuel est sauvegardé à son tour).
func restoreBackup(name string) error {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	m := backupRe.FindStringSubmatch(name)
	if m == nil {
		return terr("srv.configwrite.sauvegardeInvalide")
	}
	id := strings.ReplaceAll(m[2], "~", "/")
	path, err := configPath(id)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(backupsDir(), name))
	if err != nil {
		return err
	}
	if _, err := parseOrdered(b); err != nil {
		return terr("srv.configwrite.laSauvegardeNEst")
	}
	if cur, err := os.ReadFile(path); err == nil {
		if err := saveBackup(id, cur); err != nil {
			return err
		}
	}
	return writeConfig(path, b)
}
