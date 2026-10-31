package main

// Skills de Claude Code : un dossier par skill, avec un SKILL.md (front matter YAML puis instructions) et, au besoin, des
// fichiers d'appui. Quatre emplacements : personnels (~/.claude/skills), de projet (.claude/skills), de plugins et du store
// (skills/synced, synchronisés depuis le compte claude.ai) ; les deux derniers sont en lecture seule. Chaque skill a aussi une
// origine : store, plugin, installé (dépôt, npx skills…) ou custom (écrit à la main).
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
	Key         string     `json:"key"` // u/<nom>, p/<dossier>/<nom>, x/<plugin>/<nom>, s/<lot>/<nom>
	Name        string     `json:"name"`
	Dir         string     `json:"dir"`              // nom du dossier
	Scope       string     `json:"scope"`            // user, project, plugin, store
	Origin      string     `json:"origin"`           // store, plugin, install, custom
	Source      string     `json:"source,omitempty"` // d'où il vient : plugin, dépôt, page d'accueil indiquée dans le front matter
	Project     string     `json:"project,omitempty"`
	Plugin      string     `json:"plugin,omitempty"`
	ReadOnly    bool       `json:"readOnly,omitempty"`
	Description string     `json:"description"`
	Chars       int        `json:"chars"`            // description + when_to_use : ce que la liste des skills coûte en contexte
	DescTokens  int        `json:"descTokens"`       // estimation en tokens de la description (permanente en contexte, sauf Manual)
	Tokens      int        `json:"tokens"`           // estimation en tokens du SKILL.md entier (chargé quand le skill est lancé)
	Manual      bool       `json:"manual,omitempty"` // disable-model-invocation : Claude ne le lance pas seul (description hors contexte)
	Hidden      bool       `json:"hidden,omitempty"` // user-invocable: false
	Tools       string     `json:"tools,omitempty"`  // allowed-tools
	Files       int        `json:"files"`            // fichiers du dossier, SKILL.md compris
	KB          int64      `json:"kb"`
	Mtime       float64    `json:"mtime"`
	Path        string     `json:"path"`
	Warn        string     `json:"warn,omitempty"`
	Lock        *SkillLock `json:"lock,omitempty"` // posé par `npx skills` : présent dans un fichier de verrouillage
}

type SkillFile struct {
	Rel string `json:"rel"`
	KB  int64  `json:"kb"`
}

const (
	skillFile     = "SKILL.md"
	skillListCap  = 1536 // description + when_to_use tronquées à ce nombre de caractères dans la liste des skills
	skillMaxBytes = 256 << 10

	skillTextSnippetMax   = 200  // description, source ou origine d'un skill affichées dans la liste
	frontMatterScanLength = 400  // début du texte où un « name: » trahit un front matter mal placé
	skillListedFilesMax   = 200  // fichiers du dossier listés par readSkill
	skillRefTextMax       = 6000 // texte d'un skill cité en référence dans la consigne de rédaction
	skillInstructionMax   = 8 << 10
	skillRefsMax          = 5
)

var (
	skillName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`) // un dossier existant
	skillNew   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)         // un nouveau skill : minuscules, chiffres, tirets
	pluginIDRe = regexp.MustCompile(`^[\w.-]{1,100}@[\w.-]{1,100}$`)
	bucketRe   = regexp.MustCompile(`^[\w-]{1,100}$`) // un lot de skills synchronisés
)

func skillLateYAML() string { return L("srv.skills.lateYAML") }

// skillBase : le dossier qui contient les skills d'une clé, et si on peut y écrire.
func skillBase(key string) (base, name string, readonly bool, err error) {
	invalidErr := terr("srv.skills.skillInvalide")
	parts := strings.Split(key, "/")
	switch {
	case len(parts) == 2 && parts[0] == "u":
		return filepath.Join(cfgDir(), "skills"), parts[1], false, nilIf(skillName.MatchString(parts[1]), invalidErr)
	case len(parts) == 3 && parts[0] == "p" && safeDir(parts[1]):
		projectDir, found := knownProjectCwd(parts[1])
		if !found {
			return "", "", false, terr("srv.memory.dossierDuProjetIntrouvable")
		}
		return filepath.Join(projectDir, ".claude", "skills"), parts[2], false, nilIf(skillName.MatchString(parts[2]), invalidErr)
	case len(parts) == 3 && parts[0] == "s" && bucketRe.MatchString(parts[1]):
		return filepath.Join(cfgDir(), "skills", "synced", parts[1]), parts[2], true, nilIf(skillName.MatchString(parts[2]), invalidErr)
	case len(parts) == 3 && parts[0] == "x" && pluginIDRe.MatchString(parts[1]):
		for _, plugin := range pluginSkillRoots() {
			if plugin.id == parts[1] {
				return plugin.base, parts[2], true, nilIf(skillName.MatchString(parts[2]), invalidErr)
			}
		}
		return "", "", true, terr("srv.skills.pluginIntrouvable")
	}
	return "", "", false, invalidErr
}

func nilIf(condition bool, err error) error {
	if condition {
		return nil
	}
	return err
}

type pluginRoot struct{ id, base string }

// pluginSkillRoots : le dossier skills/ de chaque plugin installé (une seule version par plugin : la dernière listée).
func pluginSkillRoots() []pluginRoot {
	content, err := readTrusted(filepath.Join(cfgDir(), "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var installed struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(content, &installed) != nil {
		return nil
	}
	var roots []pluginRoot
	for pluginID, versions := range installed.Plugins {
		if !pluginIDRe.MatchString(pluginID) || len(versions) == 0 {
			continue
		}
		installPath := versions[len(versions)-1].InstallPath
		if installPath == "" || !filepath.IsAbs(installPath) {
			continue
		}
		roots = append(roots, pluginRoot{pluginID, filepath.Join(installPath, "skills")})
	}
	sort.Slice(roots, func(left, right int) bool { return roots[left].id < roots[right].id })
	return roots
}

func boolField(meta map[string]any, key string) (value, set bool) {
	switch typedValue := meta[key].(type) {
	case bool:
		return typedValue, true
	case string:
		switch strings.ToLower(typedValue) {
		case "true", "yes", "on", "1":
			return true, true
		case "false", "no", "off", "0":
			return false, true
		}
	case int:
		return typedValue != 0, true
	}
	return false, false
}

func textField(meta map[string]any, key string) string {
	switch typedValue := meta[key].(type) {
	case string:
		return strings.TrimSpace(typedValue)
	case []any:
		return strings.Join(strList(typedValue), " ")
	}
	return ""
}

// describeSkill : lit un SKILL.md et renseigne ce qui s'affiche dans la liste.
func describeSkill(skill *Skill, text string) {
	meta, err := parseMeta(text)
	if err != nil {
		skill.Warn = err.Error() + " : le skill se charge sans aucun champ"
	}
	skill.Name = textField(meta, "name")
	if skill.Name == "" {
		skill.Name = skill.Dir
	}
	skill.Description = textField(meta, "description")
	if skill.Description == "" { // sans description, Claude Code retient la première ligne non vide du contenu
		_, body, _ := frontMatter(text)
		for _, bodyLine := range strings.Split(body, "\n") {
			if bodyLine = strings.TrimSpace(strings.TrimLeft(bodyLine, "# ")); bodyLine != "" {
				skill.Description = cut(bodyLine, skillTextSnippetMax)
				break
			}
		}
		if skill.Warn == "" {
			skill.Warn = L("srv.skills.noDescription")
		}
	}
	listed := skill.Description + textField(meta, "when_to_use")
	skill.Chars = len([]rune(listed))
	skill.DescTokens = estimateTokens(len(cut(listed, skillListCap)))
	skill.Tokens = estimateTokens(len(text))
	if skill.Chars > skillListCap && skill.Warn == "" {
		skill.Warn = L("srv.skills.descTooLong")
	}
	skill.Manual, _ = boolField(meta, "disable-model-invocation")
	if userInvocable, set := boolField(meta, "user-invocable"); set {
		skill.Hidden = !userInvocable
	}
	skill.Tools = textField(meta, "allowed-tools")
	hasLateFrontMatter := !strings.HasPrefix(text, "---\n") && strings.Contains(text[:min(len(text), frontMatterScanLength)], "\nname:")
	if skill.Warn == "" && hasLateFrontMatter {
		skill.Warn = skillLateYAML()
	}
}

// LockEntry : un skill du fichier de verrouillage de `npx skills`.
type LockEntry struct{ Source, SourceType, SourceURL, SkillPath, Hash, InstalledAt, UpdatedAt string }

// SkillLock : ce que le verrou dit d'un skill installé par `npx skills`, et comment il est posé (lien ou copie).
type SkillLock struct {
	Source      string `json:"source"`
	SourceType  string `json:"sourceType"`
	SkillPath   string `json:"skillPath"`
	Hash        string `json:"hash"`
	InstalledAt string `json:"installedAt"`
	UpdatedAt   string `json:"updatedAt"`
	Link        bool   `json:"link"`
	Global      bool   `json:"global"`
	Managed     bool   `json:"managed"` // lien vers le dossier canonique de `npx skills` : le seul cas où gld laisse mettre à jour ou retirer
}

func (entry LockEntry) label() string {
	source := strings.TrimSpace(entry.Source)
	if entry.SourceType != "" && source != "" {
		source = entry.SourceType + " : " + source
	}
	return cut(source, skillTextSnippetMax)
}

// readLock : le skills-lock.json d'un projet ou le ~/.agents/.skill-lock.json personnel ; nil si absent ou illisible.
func readLock(path string) map[string]LockEntry {
	content, err := readTrusted(path)
	if err != nil {
		return nil
	}
	var lockFile struct {
		Skills map[string]struct {
			Source          string `json:"source"`
			SourceType      string `json:"sourceType"`
			SourceURL       string `json:"sourceUrl"`
			SkillPath       string `json:"skillPath"`
			SkillFolderHash string `json:"skillFolderHash"`
			InstalledAt     string `json:"installedAt"`
			UpdatedAt       string `json:"updatedAt"`
		} `json:"skills"`
	}
	if json.Unmarshal(content, &lockFile) != nil {
		return nil
	}
	entries := map[string]LockEntry{}
	for name, entry := range lockFile.Skills {
		entries[name] = LockEntry{entry.Source, entry.SourceType, entry.SourceURL, entry.SkillPath, entry.SkillFolderHash, entry.InstalledAt, entry.UpdatedAt}
	}
	return entries
}

// installedFrom : la source d'un skill installé, d'après le fichier de verrouillage, sinon d'après le front matter (homepage,
// repository ou source, ou un bloc metadata d'un outil d'installation). Vide pour un skill écrit à la main.
func installedFrom(name string, locks map[string]LockEntry, text string) string {
	if entry, found := locks[name]; found {
		return entry.label()
	}
	meta, _ := parseMeta(text)
	for _, key := range []string{"homepage", "repository", "source"} {
		if value := textField(meta, key); value != "" {
			return cut(value, skillTextSnippetMax)
		}
	}
	return ""
}

func countSkillFiles(dir string) (fileCount int, kilobytes int64) {
	filepath.WalkDir(dir, func(walkPath string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			fileCount++
			if fileInfo, infoErr := entry.Info(); infoErr == nil {
				kilobytes += fileInfo.Size() / bytesPerKilobyte
			}
		}
		return nil
	})
	return
}

func scanSkills(base, scope, projectDir, plugin string, readonly bool, locks map[string]LockEntry, keyOf func(name string) string) []Skill {
	var skills []Skill
	entries, err := readDirTrusted(base)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !skillName.MatchString(entry.Name()) {
			continue
		}
		if target, statErr := os.Stat(filepath.Join(base, entry.Name())); statErr != nil || !target.IsDir() { // suit les liens symboliques
			continue
		}
		skillPath := filepath.Join(base, entry.Name(), skillFile)
		fileInfo, skillStatErr := os.Stat(skillPath)
		if skillStatErr != nil || !fileInfo.Mode().IsRegular() {
			continue
		}
		skill := Skill{Key: keyOf(entry.Name()), Dir: entry.Name(), Scope: scope, Project: projectDir, Plugin: plugin, ReadOnly: readonly, Mtime: float64(fileInfo.ModTime().UnixNano()) / 1e9, Path: skillPath}
		var text string
		if fileInfo.Size() <= skillMaxBytes {
			if content, readErr := readTrusted(skillPath); readErr == nil {
				text = string(content)
				describeSkill(&skill, text)
			}
		}
		switch scope {
		case "store":
			skill.Origin, skill.Source = "store", L("srv.skills.synced")
		case "plugin":
			skill.Origin, skill.Source = "plugin", plugin
		default:
			if source := installedFrom(entry.Name(), locks, text); source != "" {
				skill.Origin, skill.Source = "install", source
				if lockEntry, found := locks[entry.Name()]; found {
					lock := SkillLock{lockEntry.Source, lockEntry.SourceType, lockEntry.SkillPath, lockEntry.Hash, lockEntry.InstalledAt, lockEntry.UpdatedAt, false, scope == "user", false}
					if linkInfo, lstatErr := os.Lstat(filepath.Join(base, entry.Name())); lstatErr == nil {
						lock.Link = linkInfo.Mode()&os.ModeSymlink != 0
					}
					canonDir := filepath.Join(skillsHome(), ".agents", "skills")
					if scope == "project" {
						canonDir = filepath.Join(projectDir, ".agents", "skills")
					}
					lock.Managed = managedByNpx(base, canonDir, entry.Name())
					skill.Lock = &lock
				}
			} else {
				skill.Origin = "custom"
			}
		}
		if skill.Name == "" {
			skill.Name = skill.Dir
		}
		dir, _ := filepath.EvalSymlinks(filepath.Dir(skillPath)) // WalkDir ne descend pas dans un lien
		skill.Files, skill.KB = countSkillFiles(dir)
		skills = append(skills, skill)
	}
	return skills
}

// listSkills : tous les skills connus, personnels d'abord, puis de projet, puis de plugins.
func listSkills() []Skill {
	skills := scanSkills(filepath.Join(cfgDir(), "skills"), "user", "", "", false, readLock(personalLockPath()), func(name string) string { return "u/" + name })
	for _, liveProject := range liveProjects() {
		dir := liveProject.Dir
		skills = append(skills, scanSkills(filepath.Join(liveProject.Project, ".claude", "skills"), "project", liveProject.Project, "", false, readLock(filepath.Join(liveProject.Project, "skills-lock.json")), func(name string) string { return "p/" + dir + "/" + name })...)
	}
	for _, plugin := range pluginSkillRoots() {
		scopeID := plugin.id
		skills = append(skills, scanSkills(plugin.base, "plugin", "", scopeID, true, nil, func(name string) string { return "x/" + scopeID + "/" + name })...)
	}
	if buckets, err := readDirTrusted(filepath.Join(cfgDir(), "skills", "synced")); err == nil {
		for _, bucket := range buckets {
			if bucket.IsDir() && bucketRe.MatchString(bucket.Name()) {
				scopeID := bucket.Name()
				skills = append(skills, scanSkills(filepath.Join(cfgDir(), "skills", "synced", scopeID), "store", "", "", true, nil, func(name string) string { return "s/" + scopeID + "/" + name })...)
			}
		}
	}
	if skills == nil {
		skills = []Skill{}
	}
	return skills
}

// readSkill : le SKILL.md et la liste des fichiers du dossier.
func readSkill(key string) (string, []SkillFile, error) {
	base, name, _, err := skillBase(key)
	if err != nil {
		return "", nil, err
	}
	skillRoot, err := os.OpenRoot(filepath.Join(base, name))
	if err != nil {
		return "", nil, err
	}
	defer skillRoot.Close()
	skillMD, err := skillRoot.Open(skillFile)
	if err != nil {
		return "", nil, err
	}
	defer skillMD.Close()
	content, err := io.ReadAll(io.LimitReader(skillMD, skillMaxBytes+1))
	if err != nil {
		return "", nil, err
	}
	if len(content) > skillMaxBytes {
		return "", nil, terr("srv.skills.skillMdTropGros")
	}
	var files []SkillFile
	fs.WalkDir(skillRoot.FS(), ".", func(walkPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && entry.Type().IsRegular() && len(files) < skillListedFilesMax {
			var kilobytes int64
			if fileInfo, infoErr := entry.Info(); infoErr == nil {
				kilobytes = fileInfo.Size() / bytesPerKilobyte
			}
			files = append(files, SkillFile{walkPath, kilobytes})
		}
		return nil
	})
	return string(content), files, nil
}

// saveSkill écrit SKILL.md (atomique) ; create : le skill ne doit pas exister et son nom suit la règle des nouveaux skills.
// La version remplacée va à la corbeille.
func saveSkill(key, text string, create bool) error {
	base, name, readonly, err := skillBase(key)
	if err != nil {
		return err
	}
	if readonly {
		return terr("srv.skills.skillDePluginLecture")
	}
	if len(text) > skillMaxBytes || strings.TrimSpace(text) == "" || !utf8.ValidString(text) {
		return terr("srv.skills.texteVideTropLong")
	}
	dir := filepath.Join(base, name)
	if fileInfo, lstatErr := os.Lstat(dir); lstatErr == nil { // #nosec G703 -- chemin géré par gld par conception, confiné par rootfs.go sous GLD_RUN_AS
		if create {
			return terr("srv.skills.unSkillDeCe")
		}
		if !fileInfo.IsDir() {
			return terr("srv.skills.nEstPasUn", "A1", name)
		}
	} else if create && !skillNew.MatchString(name) {
		return terr("srv.skills.nomInvalideMinusculesChiffres")
	}
	if !create {
		if previous, _, readErr := readSkill(key); readErr == nil && previous != text {
			if trashErr := saveTrash("k/"+key, filepath.Join(dir, skillFile), "remplacé", previous); trashErr != nil {
				return trashErr
			}
		}
	}
	if mkdirErr := mkdirTrusted(dir, 0o755); mkdirErr != nil {
		return mkdirErr
	}
	skillRoot, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer skillRoot.Close()
	temporaryName := skillFile + ".gld-web.tmp"
	if fileWriteErr := skillRoot.WriteFile(temporaryName, []byte(text), 0o644); fileWriteErr != nil {
		return fileWriteErr
	}
	if renameErr := skillRoot.Rename(temporaryName, skillFile); renameErr != nil {
		skillRoot.Remove(temporaryName)
		return renameErr
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
		return terr("srv.skills.skillDePluginLecture2")
	}
	if fileInfo, lstatErr := os.Lstat(filepath.Join(base, name)); lstatErr == nil && !fileInfo.IsDir() { // #nosec G703 -- chemin géré par gld par conception, confiné par rootfs.go sous GLD_RUN_AS ; lien symbolique : le dossier cible n'est pas à nous
		return terr("srv.skills.nEstPasUn", "A1", name)
	}
	text, files, err := readSkill(key)
	if err != nil {
		return err
	}
	dir := filepath.Join(base, name)
	// readSkill ne compte que les fichiers réguliers : un lien ou un sous-dossier vide bloquerait la suppression du dossier
	// après celle de SKILL.md, et le skill disparaîtrait en laissant son dossier.
	if entries, readDirErr := readDirTrusted(dir); len(files) > 1 || readDirErr != nil || len(entries) != 1 {
		return terr("srv.skills.ceSkillContientD")
	}
	if trashErr := saveTrash("k/"+key, filepath.Join(dir, skillFile), "supprimé", text); trashErr != nil {
		return trashErr
	}
	if removeErr := rRemove(base, filepath.Join(dir, skillFile)); removeErr != nil {
		return removeErr
	}
	return rRemove(base, dir)
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
	var destinationKey string
	switch {
	case to == "u":
		destinationKey = "u/" + name
	case strings.HasPrefix(to, "p/") && safeDir(to[2:]):
		destinationKey = to + "/" + name
	default:
		return "", terr("srv.memory.destinationInvalide")
	}
	destinationBase, destinationName, destinationReadOnly, err := skillBase(destinationKey)
	if err != nil || destinationReadOnly {
		return "", terr("srv.memory.destinationInvalide")
	}
	if destinationBase == base && destinationName == name {
		return "", terr("srv.memory.dejaACetEndroit")
	}
	target := filepath.Join(destinationBase, destinationName)
	if _, lstatErr := os.Lstat(target); lstatErr == nil {
		return "", terr("srv.skills.unSkillDeCe2")
	}
	sourceRoot, err := os.OpenRoot(filepath.Join(base, name))
	if err != nil {
		return "", err
	}
	defer sourceRoot.Close()
	fileCount, totalBytes := 0, int64(0)
	type copiedFile struct {
		rel  string
		data []byte
		mode fs.FileMode
	}
	var copiedFiles []copiedFile
	err = fs.WalkDir(sourceRoot.FS(), ".", func(walkPath string, entry fs.DirEntry, entryErr error) error {
		if entryErr != nil || !entry.Type().IsRegular() {
			return nil // dossiers (recréés à l'écriture), liens et fichiers spéciaux : ignorés
		}
		if fileCount++; fileCount > copyMaxFiles {
			return terr("srv.skills.tropDeFichiersAu")
		}
		content, entryErr := fs.ReadFile(sourceRoot.FS(), walkPath)
		if entryErr != nil {
			return entryErr
		}
		if totalBytes += int64(len(content)); totalBytes > copyMaxBytes {
			return terr("srv.skills.dossierTropGrosMo")
		}
		mode := fs.FileMode(0o644)
		if fileInfo, infoErr := entry.Info(); infoErr == nil {
			mode |= fileInfo.Mode() & 0o111 // un scripts/run.sh doit rester exécutable ; rien d'autre n'est repris de la source
		}
		copiedFiles = append(copiedFiles, copiedFile{walkPath, content, mode})
		return nil
	})
	if err != nil {
		return "", err
	}
	if mkdirErr := mkdirTrusted(target, 0o755); mkdirErr != nil {
		return "", mkdirErr
	}
	destinationRoot, err := os.OpenRoot(target)
	if err != nil {
		return "", err
	}
	defer destinationRoot.Close()
	for _, copied := range copiedFiles {
		if parentDir := filepath.Dir(copied.rel); parentDir != "." {
			if mkdirAllErr := destinationRoot.MkdirAll(parentDir, 0o755); mkdirAllErr != nil {
				return "", mkdirAllErr
			}
		}
		if fileWriteErr := destinationRoot.WriteFile(copied.rel, copied.data, copied.mode); fileWriteErr != nil {
			return "", fileWriteErr
		}
	}
	return destinationKey, nil
}

// skillSystem : consigne de rédaction d'un SKILL.md.
func skillSystem() string { return L("prompt.skillSystem") }

// skillDraft rédige ou révise un SKILL.md : la consigne, le texte actuel (key existant) et d'autres skills en référence.
func skillDraft(model, instruction, key string, refs []string) (string, float64, error) {
	if !slices.Contains(writeModels, model) {
		return "", 0, terr("srv.memwrite.modeleInconnu")
	}
	instruction = strings.TrimSpace(instruction)
	if instruction == "" || len(instruction) > skillInstructionMax || len(refs) > skillRefsMax {
		return "", 0, terr("srv.memwrite.consigneVideOuTrop")
	}
	var prompt strings.Builder
	prompt.WriteString(L("prompt.instruction", "A1", instruction) + "\n")
	var currentText string
	if key != "" {
		text, _, readErr := readSkill(key)
		switch {
		case readErr == nil:
			currentText = text
			prompt.WriteString("\n" + L("prompt.skillToRevise", "A1", key) + "\n~~~~\n" + text + "\n~~~~\n")
		case errors.Is(readErr, os.ErrNotExist):
			prompt.WriteString("\n" + L("prompt.skillMissing") + "\n")
		default:
			return "", 0, readErr
		}
	}
	for _, refKey := range refs {
		if refKey == key {
			continue
		}
		refText, _, refErr := readSkill(refKey)
		if refErr != nil {
			return "", 0, refErr
		}
		prompt.WriteString("\n" + L("prompt.skillRef", "A1", refKey) + "\n~~~~\n" + cut(refText, skillRefTextMax) + "\n~~~~\n")
	}
	draft, cost, err := memDraft(model, skillSystem(), prompt.String())
	if err != nil {
		return "", cost, err
	}
	// un modèle qui condense oublie parfois l'en-tête : on remet celui du skill d'origine
	return keepHeader(currentText, draft), cost, nil
}
