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
	From   string `json:"from"`
	To     string `json:"to"`
}

type SyncPick struct{ ID, Mode string } // Mode : merge ou replace

var (
	// jamais copiés : secrets et programmes qui en produisent
	syncSkip     = map[string]bool{"env": true, "apiKeyHelper": true, "awsAuthRefresh": true, "awsCredentialExport": true, "otelHeadersHelper": true, "permissions": true}
	syncPermKind = []string{"allow", "ask", "deny", "additionalDirectories"}
	syncSegRe    = regexp.MustCompile(`^[\w][\w. -]*$`)
)

const (
	syncFileMax  = 256 << 10
	syncSkillMax = 2 << 20
	syncPreview  = 4000
)

func homeDir(name string) (string, error) {
	if name == "" {
		return cfgDir(), nil
	}
	if h, ok := homeByName(name); ok {
		return h.Dir, nil
	}
	return "", terr("srv.homesync.dossierDeConfigurationInconnu", "A1", name)
}

func syncSettings(dir string) (*omap, []byte, error) {
	path := filepath.Join(dir, "settings.json")
	if err := notSymlink(path); err != nil {
		return nil, nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return newOmap(), nil, nil
	}
	if strings.TrimSpace(string(b)) == "" {
		return newOmap(), b, nil
	}
	v, err := parseOrdered(b)
	if err != nil {
		return nil, nil, terr("srv.homesync.estIllisible", "A1", path, "A2", err.Error())
	}
	o := asMap(v)
	if o == nil {
		return nil, nil, terr("srv.homesync.unObjetJsonEst", "A1", path)
	}
	return o, b, nil
}

func cutSync(s string) string {
	if len(s) > syncPreview {
		return s[:syncPreview] + "\n…"
	}
	return s
}

// syncPickNeedsConfig : seuls CLAUDE.md et rules/*.md sont du texte inerte ; réglages, permissions, skills et commandes
// font exécuter du code par Claude Code. Un identifiant non reconnu est traité comme tel.
func syncPickNeedsConfig(id string) bool {
	rel, ok := strings.CutPrefix(id, "file:")
	if !ok || strings.Contains(rel, "\\") || path.Clean(rel) != rel {
		return true
	}
	return rel != "CLAUDE.md" && !(strings.HasPrefix(rel, "rules/") && strings.HasSuffix(rel, ".md"))
}

// syncFile : fichiers texte copiables d'un dossier de configuration, par chemin relatif (CLAUDE.md, rules/*.md, commands/**.md).
func syncFiles(dir string) map[string]string {
	out := map[string]string{}
	read := func(rel string) {
		p := filepath.Join(dir, rel)
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > syncFileMax {
			return
		}
		if b, err := os.ReadFile(p); err == nil {
			out[rel] = string(b)
		}
	}
	read("CLAUDE.md")
	for _, sub := range []string{"rules", "commands"} {
		base := walkRoot(filepath.Join(dir, sub))
		filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			r, _ := filepath.Rel(base, p)
			rel := filepath.Join(sub, r)
			if d.IsDir() {
				if strings.Count(rel, string(filepath.Separator)) > 3 || (p != base && strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), ".md") && !strings.HasPrefix(d.Name(), ".") && syncRelOK(rel) {
				read(rel)
			}
			return nil
		})
	}
	return out
}

// syncSkills : pour chaque skill (dossier de skills/), ses fichiers réguliers et leur contenu. Un skill trop gros est ignoré.
func syncSkills(dir string) map[string]map[string]string {
	out := map[string]map[string]string{}
	ents, _ := os.ReadDir(filepath.Join(dir, "skills"))
	for _, e := range ents {
		if !e.IsDir() || !syncSegRe.MatchString(e.Name()) {
			continue
		}
		root := walkRoot(filepath.Join(dir, "skills", e.Name()))
		files, total, ok := map[string]string{}, int64(0), true
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			fi, err := d.Info()
			if err != nil || !fi.Mode().IsRegular() {
				return nil
			}
			if total += fi.Size(); total > syncSkillMax || len(files) >= 100 {
				ok = false
				return fs.SkipAll
			}
			rel, _ := filepath.Rel(root, p)
			if b, err := os.ReadFile(p); err == nil && syncRelOK(rel) {
				files[rel] = string(b)
			}
			return nil
		})
		if ok && len(files) > 0 {
			out[e.Name()] = files
		}
	}
	return out
}

// syncRelOK : un chemin relatif sans « .. », sans segment caché ni caractère inattendu.
func syncRelOK(rel string) bool {
	for _, s := range strings.Split(filepath.ToSlash(rel), "/") {
		if !syncSegRe.MatchString(s) || strings.Contains(s, "..") {
			return false
		}
	}
	return true
}

func listText(l []string) string { return strings.Join(l, "\n") }

// sameSet : mêmes éléments des deux côtés, sans égard à l'ordre ni aux doublons.
func sameSet(a, b []string) bool {
	set := func(l []string) map[string]bool {
		m := map[string]bool{}
		for _, x := range l {
			m[x] = true
		}
		return m
	}
	return maps.Equal(set(a), set(b))
}

func skillText(files map[string]string) string {
	var ks []string
	for k, v := range files {
		ks = append(ks, fmt.Sprintf("%s (%d o)", k, len(v)))
	}
	sort.Strings(ks)
	return listText(ks)
}

func sameFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// syncPlan compare deux dossiers de configuration, du point de vue de la copie de from vers to.
func syncPlan(from, to string) ([]SyncItem, error) {
	fd, err := homeDir(from)
	if err != nil {
		return nil, err
	}
	td, err := homeDir(to)
	if err != nil {
		return nil, err
	}
	if fd == td {
		return nil, terr("srv.homesync.laSourceEtLa")
	}
	fs, _, err := syncSettings(fd)
	if err != nil {
		return nil, err
	}
	ts, _, err := syncSettings(td)
	if err != nil {
		return nil, err
	}
	items := []SyncItem{}
	for _, k := range fs.keys {
		if syncSkip[k] || secretKey.MatchString(k) {
			continue
		}
		sv, _ := fs.get(k)
		tv, has := ts.get(k)
		it := SyncItem{ID: "set:" + k, Kind: "setting", Label: k, Status: "new", Merge: asMap(sv) != nil, From: cutSync(formatOrdered(maskSecrets(sv, k)))}
		if has {
			it.To = cutSync(formatOrdered(maskSecrets(tv, k)))
			it.Status = "diff"
			if formatOrdered(sv) == formatOrdered(tv) {
				it.Status = "same"
			}
		}
		if k == "hooks" || k == "statusLine" {
			it.Risk = "exec"
		}
		items = append(items, it)
	}
	for _, kind := range syncPermKind {
		sv, _ := lookup(fs, "permissions", kind)
		tv, _ := lookup(ts, "permissions", kind)
		sl, tl := strList(sv), strList(tv)
		if len(sl) == 0 {
			continue
		}
		it := SyncItem{ID: "perm:" + kind, Kind: "perm", Label: "permissions." + kind, Status: "new", Merge: true, From: cutSync(listText(sl)), To: cutSync(listText(tl))}
		if len(tl) > 0 {
			it.Status = "diff"
			if sameSet(sl, tl) {
				it.Status = "same"
			}
		}
		items = append(items, it)
	}
	sf, tf := syncFiles(fd), syncFiles(td)
	var rels []string
	for r := range sf {
		rels = append(rels, r)
	}
	sort.Strings(rels)
	for _, r := range rels {
		it := SyncItem{ID: "file:" + filepath.ToSlash(r), Kind: "file", Label: filepath.ToSlash(r), Status: "new", From: cutSync(sf[r])}
		if t, ok := tf[r]; ok {
			it.To, it.Status = cutSync(t), "diff"
			if t == sf[r] {
				it.Status = "same"
			}
		}
		items = append(items, it)
	}
	ss, ts2 := syncSkills(fd), syncSkills(td)
	var names []string
	for n := range ss {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		it := SyncItem{ID: "skill:" + n, Kind: "skill", Label: "skills/" + n, Status: "new", From: skillText(ss[n])}
		if t, ok := ts2[n]; ok {
			it.To, it.Status = skillText(t), "diff"
			if sameFiles(ss[n], t) {
				it.Status = "same"
			}
		}
		items = append(items, it)
	}
	return items, nil
}

// syncBackup copie le fichier existant dans <cible>/gld-web-sync-backups/<horodatage>/<chemin relatif> avant de le remplacer.
func syncBackup(dir, stamp, rel string) error {
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		return nil // rien à sauvegarder : le fichier n'existe pas
	}
	return writeConfig(filepath.Join(dir, "gld-web-sync-backups", stamp, rel), b)
}

// syncApply copie les éléments choisis de from vers to. Chaque élément est revalidé côté serveur (le navigateur ne désigne que des
// identifiants du plan). Renvoie un résumé.
func syncApply(from, to string, picks []SyncPick) (string, error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	plan, err := syncPlan(from, to)
	if err != nil {
		return "", err
	}
	byID := map[string]SyncItem{}
	for _, it := range plan {
		byID[it.ID] = it
	}
	fd, _ := homeDir(from)
	td, _ := homeDir(to)
	fs, _, _ := syncSettings(fd)
	ts, torig, err := syncSettings(td)
	if err != nil {
		return "", err
	}
	if torig != nil && strings.TrimSpace(string(torig)) != "" && formatOrdered(ts)+"\n" != string(torig) {
		return "", terr("srv.homesync.leSettingsJsonDe")
	}
	sf, ss, tsk := syncFiles(fd), syncSkills(fd), syncSkills(td)
	stamp := time.Now().Format("20060102-150405")
	settingsTouched, n := false, 0
	type fileOut struct {
		rel, content string
		exec         bool
	}
	var writes []fileOut
	var removes []string // fichiers d'un skill remplacé absents de la source
	for _, p := range picks {
		it, ok := byID[p.ID]
		if !ok || it.Status == "same" {
			continue
		}
		mode := p.Mode
		if mode != "merge" || !it.Merge {
			mode = "replace"
		}
		kind, name, _ := strings.Cut(p.ID, ":")
		switch kind {
		case "set":
			sv, _ := fs.get(name)
			tv, has := ts.get(name)
			if mode == "merge" && has {
				if so, to := asMap(sv), asMap(tv); so != nil && to != nil {
					for _, k := range so.keys {
						if _, dup := to.get(k); !dup {
							to.set(k, so.m[k])
						}
					}
					break
				}
			}
			ts.set(name, sv)
		case "perm":
			sv, _ := lookup(fs, "permissions", name)
			tv, _ := lookup(ts, "permissions", name)
			next := strList(sv)
			if mode == "merge" {
				have := map[string]bool{}
				next = strList(tv)
				for _, r := range next {
					have[r] = true
				}
				for _, r := range strList(sv) {
					if !have[r] {
						next = append(next, r)
					}
				}
			}
			setRules(ts, name, next)
		case "file":
			writes = append(writes, fileOut{filepath.FromSlash(name), sf[filepath.FromSlash(name)], false})
		case "skill":
			for rel, c := range ss[name] {
				p := filepath.Join("skills", name, rel)
				fi, err := os.Lstat(filepath.Join(fd, p)) // Lstat : un lien n'apporte jamais son mode
				writes = append(writes, fileOut{p, c, err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0})
			}
			for rel := range tsk[name] {
				if _, keep := ss[name][rel]; !keep {
					removes = append(removes, filepath.Join("skills", name, rel))
				}
			}
		default:
			continue
		}
		if kind == "set" || kind == "perm" {
			settingsTouched = true
		}
		n++
	}
	if settingsTouched {
		if err := syncBackup(td, stamp, "settings.json"); err != nil {
			return "", terr("srv.homesync.sauvegardeImpossible", "A1", err.Error())
		}
		if err := writeConfig(filepath.Join(td, "settings.json"), []byte(formatOrdered(ts)+"\n")); err != nil {
			return "", err
		}
	}
	for _, w := range writes {
		if !syncRelOK(w.rel) {
			return "", terr("srv.homesync.cheminRefuse", "A1", w.rel)
		}
		if err := syncBackup(td, stamp, w.rel); err != nil {
			return "", terr("srv.homesync.sauvegardeImpossible", "A1", err.Error())
		}
		if err := writeConfigExec(filepath.Join(td, w.rel), []byte(w.content), w.exec); err != nil {
			return "", err
		}
	}
	for _, rel := range removes {
		if !syncRelOK(rel) {
			return "", terr("srv.homesync.cheminRefuse", "A1", rel)
		}
		if err := syncBackup(td, stamp, rel); err != nil {
			return "", terr("srv.homesync.sauvegardeImpossible", "A1", err.Error())
		}
		if err := rRemove(td, filepath.Join(td, rel)); err != nil {
			return "", err
		}
	}
	return L("srv.homesync.copied", "A1", n, "A2", filepath.Join(td, "gld-web-sync-backups", stamp)), nil
}
