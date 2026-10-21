package main

// Skills de Claude Code : un dossier par skill, avec un SKILL.md (front matter YAML puis instructions) et, au besoin, des
// fichiers d'appui. Trois origines : personnels (~/.claude/skills), de projet (.claude/skills) et de plugins (lecture seule).
// Seul SKILL.md se modifie ici ; les autres fichiers du dossier sont listés.

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

type Skill struct {
	Key         string  `json:"key"` // u/<nom>, p/<dossier>/<nom>, x/<plugin>/<nom>
	Name        string  `json:"name"`
	Dir         string  `json:"dir"`   // nom du dossier
	Scope       string  `json:"scope"` // user, project, plugin
	Project     string  `json:"project,omitempty"`
	Plugin      string  `json:"plugin,omitempty"`
	ReadOnly    bool    `json:"readOnly,omitempty"`
	Description string  `json:"description"`
	Chars       int     `json:"chars"`            // description + when_to_use : ce que la liste des skills coûte en contexte
	Manual      bool    `json:"manual,omitempty"` // disable-model-invocation : Claude ne le lance pas seul (description hors contexte)
	Hidden      bool    `json:"hidden,omitempty"` // user-invocable: false
	Tools       string  `json:"tools,omitempty"`  // allowed-tools
	Files       int     `json:"files"`            // fichiers du dossier, SKILL.md compris
	KB          int64   `json:"kb"`
	Mtime       float64 `json:"mtime"`
	Path        string  `json:"path"`
	Warn        string  `json:"warn,omitempty"`
}

type SkillFile struct {
	Rel string `json:"rel"`
	KB  int64  `json:"kb"`
}

const (
	skillFile     = "SKILL.md"
	skillListCap  = 1536 // description + when_to_use tronquées à ce nombre de caractères dans la liste des skills
	skillMaxBytes = 256 << 10
)

var (
	skillName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`) // un dossier existant
	skillNew   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)         // un nouveau skill : minuscules, chiffres, tirets
	pluginIDRe = regexp.MustCompile(`^[\w.-]{1,100}@[\w.-]{1,100}$`)
)

const skillLateYAML = "le front matter doit commencer à la toute première ligne, sinon tout le fichier est pris pour du contenu"

// skillBase : le dossier qui contient les skills d'une clé, et si on peut y écrire.
func skillBase(key string) (base, name string, readonly bool, err error) {
	bad := errors.New("skill invalide")
	parts := strings.Split(key, "/")
	switch {
	case len(parts) == 2 && parts[0] == "u":
		return filepath.Join(cfgDir(), "skills"), parts[1], false, nilIf(skillName.MatchString(parts[1]), bad)
	case len(parts) == 3 && parts[0] == "p" && safeDir(parts[1]):
		cwd := projectCwd(parts[1])
		if fi, e := os.Stat(cwd); cwd == "" || e != nil || !fi.IsDir() {
			return "", "", false, errors.New("dossier du projet introuvable")
		}
		return filepath.Join(cwd, ".claude", "skills"), parts[2], false, nilIf(skillName.MatchString(parts[2]), bad)
	case len(parts) == 3 && parts[0] == "x" && pluginIDRe.MatchString(parts[1]):
		for _, p := range pluginSkillRoots() {
			if p.id == parts[1] {
				return p.base, parts[2], true, nilIf(skillName.MatchString(parts[2]), bad)
			}
		}
		return "", "", true, errors.New("plugin introuvable")
	}
	return "", "", false, bad
}

func nilIf(ok bool, err error) error {
	if ok {
		return nil
	}
	return err
}

type pluginRoot struct{ id, base string }

// pluginSkillRoots : le dossier skills/ de chaque plugin installé (une seule version par plugin : la dernière listée).
func pluginSkillRoots() []pluginRoot {
	b, err := os.ReadFile(filepath.Join(cfgDir(), "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var d struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(b, &d) != nil {
		return nil
	}
	var out []pluginRoot
	for id, vs := range d.Plugins {
		if !pluginIDRe.MatchString(id) || len(vs) == 0 {
			continue
		}
		p := vs[len(vs)-1].InstallPath
		if p == "" || !filepath.IsAbs(p) {
			continue
		}
		out = append(out, pluginRoot{id, filepath.Join(p, "skills")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

func boolField(m map[string]any, k string) (v, set bool) {
	switch x := m[k].(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(x) {
		case "true", "yes", "on", "1":
			return true, true
		case "false", "no", "off", "0":
			return false, true
		}
	case int:
		return x != 0, true
	}
	return false, false
}

func textField(m map[string]any, k string) string {
	switch x := m[k].(type) {
	case string:
		return strings.TrimSpace(x)
	case []any:
		return strings.Join(strList(x), " ")
	}
	return ""
}

// describeSkill : lit un SKILL.md et renseigne ce qui s'affiche dans la liste.
func describeSkill(s *Skill, text string) {
	m, err := parseMeta(text)
	if err != nil {
		s.Warn = err.Error() + " : le skill se charge sans aucun champ"
	}
	s.Name = textField(m, "name")
	if s.Name == "" {
		s.Name = s.Dir
	}
	s.Description = textField(m, "description")
	if s.Description == "" { // sans description, Claude Code retient la première ligne non vide du contenu
		_, body, _ := frontMatter(text)
		for _, l := range strings.Split(body, "\n") {
			if l = strings.TrimSpace(strings.TrimLeft(l, "# ")); l != "" {
				s.Description = cut(l, 200)
				break
			}
		}
		if s.Warn == "" {
			s.Warn = "pas de description : Claude Code retient la première ligne du contenu, et ne saura pas quand l'utiliser"
		}
	}
	s.Chars = len([]rune(s.Description + textField(m, "when_to_use")))
	if s.Chars > skillListCap && s.Warn == "" {
		s.Warn = "description trop longue : tronquée à 1 536 caractères dans la liste des skills"
	}
	s.Manual, _ = boolField(m, "disable-model-invocation")
	if ui, set := boolField(m, "user-invocable"); set {
		s.Hidden = !ui
	}
	s.Tools = textField(m, "allowed-tools")
	if s.Warn == "" && !strings.HasPrefix(text, "---\n") && strings.Contains(text[:min(len(text), 400)], "\nname:") {
		s.Warn = skillLateYAML
	}
}

func countSkillFiles(dir string) (n int, kb int64) {
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			n++
			if i, e := d.Info(); e == nil {
				kb += i.Size() / 1024
			}
		}
		return nil
	})
	return
}

func scanSkills(base, scope, project, plugin string, readonly bool, keyOf func(name string) string) []Skill {
	var out []Skill
	ents, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	for _, e := range ents {
		if !e.IsDir() || !skillName.MatchString(e.Name()) {
			continue
		}
		p := filepath.Join(base, e.Name(), skillFile)
		st, err := os.Stat(p)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		s := Skill{Key: keyOf(e.Name()), Dir: e.Name(), Scope: scope, Project: project, Plugin: plugin, ReadOnly: readonly, Mtime: float64(st.ModTime().UnixNano()) / 1e9, Path: p}
		if st.Size() <= skillMaxBytes {
			if b, err := os.ReadFile(p); err == nil {
				describeSkill(&s, string(b))
			}
		}
		if s.Name == "" {
			s.Name = s.Dir
		}
		s.Files, s.KB = countSkillFiles(filepath.Dir(p))
		out = append(out, s)
	}
	return out
}

// listSkills : tous les skills connus, personnels d'abord, puis de projet, puis de plugins.
func listSkills() []Skill {
	out := scanSkills(filepath.Join(cfgDir(), "skills"), "user", "", "", false, func(n string) string { return "u/" + n })
	for _, p := range liveProjects() {
		dir := p.Dir
		out = append(out, scanSkills(filepath.Join(p.Project, ".claude", "skills"), "project", p.Project, "", false, func(n string) string { return "p/" + dir + "/" + n })...)
	}
	for _, pr := range pluginSkillRoots() {
		id := pr.id
		out = append(out, scanSkills(pr.base, "plugin", "", id, true, func(n string) string { return "x/" + id + "/" + n })...)
	}
	if out == nil {
		out = []Skill{}
	}
	return out
}

// readSkill : le SKILL.md et la liste des fichiers du dossier.
func readSkill(key string) (string, []SkillFile, error) {
	base, name, _, err := skillBase(key)
	if err != nil {
		return "", nil, err
	}
	root, err := os.OpenRoot(filepath.Join(base, name))
	if err != nil {
		return "", nil, err
	}
	defer root.Close()
	f, err := root.Open(skillFile)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, skillMaxBytes+1))
	if err != nil {
		return "", nil, err
	}
	if len(b) > skillMaxBytes {
		return "", nil, errors.New("SKILL.md trop gros")
	}
	var files []SkillFile
	fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && len(files) < 200 {
			var kb int64
			if i, e := d.Info(); e == nil {
				kb = i.Size() / 1024
			}
			files = append(files, SkillFile{p, kb})
		}
		return nil
	})
	return string(b), files, nil
}

// saveSkill écrit SKILL.md (atomique) ; create : le skill ne doit pas exister et son nom suit la règle des nouveaux skills.
// La version remplacée va à la corbeille.
func saveSkill(key, text string, create bool) error {
	base, name, readonly, err := skillBase(key)
	if err != nil {
		return err
	}
	if readonly {
		return errors.New("skill de plugin : lecture seule (copiez-le dans vos skills pour le modifier)")
	}
	if len(text) > skillMaxBytes || strings.TrimSpace(text) == "" || !utf8.ValidString(text) {
		return errors.New("texte vide, trop long ou invalide")
	}
	dir := filepath.Join(base, name)
	if st, err := os.Lstat(dir); err == nil {
		if create {
			return errors.New("un skill de ce nom existe déjà")
		}
		if !st.IsDir() {
			return errors.New("« " + name + " » n'est pas un dossier")
		}
	} else if create && !skillNew.MatchString(name) {
		return errors.New("nom invalide : minuscules, chiffres et tirets (64 caractères au plus)")
	}
	if !create {
		if old, _, err := readSkill(key); err == nil && old != text {
			if err := saveTrash("k/"+key, filepath.Join(dir, skillFile), "remplacé", old); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	tmp := skillFile + ".gld-web.tmp"
	if err := root.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if err := root.Rename(tmp, skillFile); err != nil {
		root.Remove(tmp)
		return err
	}
	return nil
}

// removeSkill supprime un skill (SKILL.md à la corbeille). Un dossier qui contient d'autres fichiers n'est pas touché :
// la corbeille ne garde qu'un texte, et on ne perd pas des scripts sans le dire.
func removeSkill(key string) error {
	base, name, readonly, err := skillBase(key)
	if err != nil {
		return err
	}
	if readonly {
		return errors.New("skill de plugin : lecture seule")
	}
	text, files, err := readSkill(key)
	if err != nil {
		return err
	}
	if len(files) > 1 {
		return errors.New("ce skill contient d'autres fichiers que SKILL.md : supprimez son dossier à la main")
	}
	dir := filepath.Join(base, name)
	if err := saveTrash("k/"+key, filepath.Join(dir, skillFile), "supprimé", text); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, skillFile)); err != nil {
		return err
	}
	return os.Remove(dir)
}

const (
	copyMaxFiles = 50
	copyMaxBytes = 2 << 20
)

// copySkill copie un skill (dossier entier : fichiers ordinaires seulement) chez soi (« u ») ou dans un projet (« p/<dossier> »).
func copySkill(key, to string) (string, error) {
	base, name, _, err := skillBase(key)
	if err != nil {
		return "", err
	}
	var dst string
	switch {
	case to == "u":
		dst = "u/" + name
	case strings.HasPrefix(to, "p/") && safeDir(to[2:]):
		dst = to + "/" + name
	default:
		return "", errors.New("destination invalide")
	}
	dbase, dname, dro, err := skillBase(dst)
	if err != nil || dro {
		return "", errors.New("destination invalide")
	}
	if dbase == base && dname == name {
		return "", errors.New("déjà à cet endroit")
	}
	target := filepath.Join(dbase, dname)
	if _, err := os.Lstat(target); err == nil {
		return "", errors.New("un skill de ce nom existe déjà à destination")
	}
	src, err := os.OpenRoot(filepath.Join(base, name))
	if err != nil {
		return "", err
	}
	defer src.Close()
	n, total := 0, int64(0)
	type item struct {
		rel  string
		data []byte
	}
	var items []item
	err = fs.WalkDir(src.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil // dossiers (recréés à l'écriture), liens et fichiers spéciaux : ignorés
		}
		if n++; n > copyMaxFiles {
			return errors.New("trop de fichiers (50 au plus) : copiez le dossier à la main")
		}
		b, err := fs.ReadFile(src.FS(), p)
		if err != nil {
			return err
		}
		if total += int64(len(b)); total > copyMaxBytes {
			return errors.New("dossier trop gros (2 Mo au plus) : copiez-le à la main")
		}
		items = append(items, item{p, b})
		return nil
	})
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	dst2, err := os.OpenRoot(target)
	if err != nil {
		return "", err
	}
	defer dst2.Close()
	for _, it := range items {
		if d := filepath.Dir(it.rel); d != "." {
			if err := dst2.MkdirAll(d, 0o755); err != nil {
				return "", err
			}
		}
		if err := dst2.WriteFile(it.rel, it.data, 0o644); err != nil {
			return "", err
		}
	}
	return dst, nil
}

// skillSystem : consigne de rédaction d'un SKILL.md.
const skillSystem = `Tu rédiges un SKILL.md pour Claude Code : un en-tête YAML entre deux lignes « --- », puis les instructions en Markdown.
En-tête : name (minuscules, chiffres et tirets), description (ce que fait le skill et QUAND l'utiliser, avec les formulations qui doivent le déclencher ; l'essentiel d'abord, 1 000 caractères au plus), et seulement si utile : disable-model-invocation: true (workflow à effet de bord, lancé par l'utilisateur seul), allowed-tools (outils pré-autorisés pendant le tour), argument-hint.
Corps : impératif, concis, étapes numérotées, contraintes et format de sortie ; $ARGUMENTS là où l'utilisateur donne ses arguments. N'invente aucune exigence absente de la consigne ou des références.
Garde ce que contient déjà le fichier existant (sauf si la consigne le change) et sa langue. Réponds uniquement avec le contenu complet du fichier, sans bloc de code englobant ni commentaire.`

// skillDraft rédige ou révise un SKILL.md : la consigne, le texte actuel (key existant) et d'autres skills en référence.
func skillDraft(model, instruction, key string, refs []string) (string, float64, error) {
	if !slices.Contains(writeModels, model) {
		return "", 0, errors.New("modèle inconnu")
	}
	instruction = strings.TrimSpace(instruction)
	if instruction == "" || len(instruction) > 8<<10 || len(refs) > 5 {
		return "", 0, errors.New("consigne vide ou trop longue, ou trop de références")
	}
	var b strings.Builder
	b.WriteString("Consigne : " + instruction + "\n")
	var cur string
	if key != "" {
		text, _, err := readSkill(key)
		switch {
		case err == nil:
			cur = text
			b.WriteString("\nSKILL.md à réviser (" + key + ") :\n~~~~\n" + text + "\n~~~~\n")
		case errors.Is(err, os.ErrNotExist):
			b.WriteString("\n(Le skill n'existe pas encore : écris le fichier en entier.)\n")
		default:
			return "", 0, err
		}
	}
	for _, r := range refs {
		if r == key {
			continue
		}
		text, _, err := readSkill(r)
		if err != nil {
			return "", 0, err
		}
		b.WriteString("\nSkill de référence (" + r + ") :\n~~~~\n" + cut(text, 6000) + "\n~~~~\n")
	}
	out, cost, err := memDraft(model, skillSystem, b.String())
	if err != nil {
		return "", cost, err
	}
	// un modèle qui condense oublie parfois l'en-tête : on remet celui du skill d'origine
	return keepHeader(cur, out), cost, nil
}
