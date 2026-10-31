// Mémoire de Claude : globale (~/.claude/CLAUDE.md, rules/), par projet (projects/<dossier>/memory/)
// et fichiers CLAUDE.md du projet. Seuls ces fichiers-là sont lisibles, modifiables ou supprimables.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

type MemFile struct {
	Key   string  `json:"key"` // g/…, m/<dossier>/…, p/<dossier>/…
	Name  string  `json:"name"`
	KB    int64   `json:"kb"`
	Mtime float64 `json:"mtime"`
	Path  string  `json:"path"` // chemin complet, pour le copier

	Type   string   `json:"type,omitempty"`   // metadata.type du front matter (entrées de mémoire)
	Paths  []string `json:"paths,omitempty"`  // règle scopée : globs du front matter `paths:`
	Tokens int      `json:"tokens"`           // estimation (calibrée sur les comptes réels), ou compte réel si Exact
	Exact  bool     `json:"exact,omitempty"`  // Tokens est un compte réel (voir tokens.go)
	Always bool     `json:"always,omitempty"` // chargé à chaque session
	Off    bool     `json:"off,omitempty"`    // règle désactivée (fichier .md.off)
	Warn   string   `json:"warn,omitempty"`   // front matter invalide
	Gld    bool     `json:"gld,omitempty"`    // règle livrée par gld (metadata.source: gld)
}

type MemProject struct {
	Dir     string    `json:"dir"`
	Project string    `json:"project"`
	Missing bool      `json:"missing"` // dossier du projet introuvable : Project est alors deviné depuis le nom
	Files   []MemFile `json:"files"`
}

const memMax = 1 << 20

const (
	bytesPerKilobyte     = 1024
	nanosecondsPerSecond = 1e9
)

var (
	mdName   = regexp.MustCompile(`^[^./\\][^/\\]*\.md$`)
	ruleName = regexp.MustCompile(`^[^./\\][^/\\]*\.md(\.off)?$`) // une règle désactivée garde son nom + « .off »
	// Fichiers du dossier de travail d'un projet affichés dans la zone Mémoire. Seuls les CLAUDE.md sont chargés par Claude Code ;
	// les autres (AGENTS.md, CONVENTIONS.md, PLAN.md, TOOLING.md, README.md) sont de la documentation du projet, qu'on lit et édite ici.
	projectFiles = []string{"CLAUDE.md", "CLAUDE.local.md", ".claude/CLAUDE.md", "AGENTS.md", "CONVENTIONS.md", "PLAN.md", "TOOLING.md", "README.md"}
)

// globalClaude : ce chemin est le CLAUDE.md global (~/.claude/CLAUDE.md), même atteint comme fichier d'un projet
// (le projet « ~ » a pour .claude/CLAUDE.md… le fichier global). Il n'est alors géré que dans la section Globale.
func globalClaude(path string) bool { return isCfgEntry(path, "CLAUDE.md") }

// cfgDirs : le dossier de config de ce processus, la config principale (vue d'un home enfant) et celle de chaque home connu.
func cfgDirs() []string {
	configDirs := []string{cfgDir()}
	homes := readHomes()
	if mainConfigDir := os.Getenv(ideMainCfgEnv); mainConfigDir != "" && filepath.IsAbs(mainConfigDir) {
		configDirs = append(configDirs, mainConfigDir)
		homes = append(homes, readHomesFile(filepath.Join(mainConfigDir, homesFileName))...)
	}
	for _, knownHome := range homes {
		if filepath.IsAbs(knownHome.Dir) {
			configDirs = append(configDirs, knownHome.Dir)
		}
	}
	return configDirs
}

// isCfgEntry : path est <config d'un home>/<name> (même fichier ou dossier, liens suivis).
func isCfgEntry(path, name string) bool {
	pathInfo, pathStatErr := os.Stat(path)
	for _, homeConfigDir := range cfgDirs() {
		candidatePath := filepath.Join(homeConfigDir, name)
		if filepath.Clean(path) == candidatePath {
			return true
		}
		if candidateInfo, candidateStatErr := os.Stat(candidatePath); pathStatErr == nil && candidateStatErr == nil && os.SameFile(pathInfo, candidateInfo) {
			return true
		}
	}
	return false
}

func safeDir(dirName string) bool {
	isSpecialName := dirName == "" || dirName == "." || dirName == ".."
	return !isSpecialName && !strings.ContainsAny(dirName, `/\`)
}

// knownProjectCwd : le dossier de travail d'un projet dont le dossier de transcripts existe sous projects/ (un nom reçu
// d'un client ne se décode jamais vers un dossier arbitraire du disque).
func knownProjectCwd(projectDir string) (string, bool) {
	if !safeDir(projectDir) || !isDirPath(filepath.Join(root, projectDir)) {
		return "", false
	}
	workingDir := projectCwd(projectDir)
	return workingDir, workingDir != "" && isDirPath(workingDir)
}

// projectCwd : le dossier de travail d'un dossier de transcripts (sa session la plus récente, sinon son nom décodé).
func projectCwd(dir string) string {
	files, _ := filepath.Glob(filepath.Join(root, dir, "*.jsonl"))
	var newestInfo os.FileInfo
	var newestPath string
	for _, transcriptPath := range files {
		transcriptInfo, err := os.Stat(transcriptPath)
		if err != nil || transcriptInfo.Size() <= 0 {
			continue
		}
		isNewest := newestInfo == nil || transcriptInfo.ModTime().After(newestInfo.ModTime())
		if isNewest {
			newestInfo, newestPath = transcriptInfo, transcriptPath
		}
	}
	if newestInfo != nil {
		if sessionCwd := info(newestPath, newestInfo).Cwd; sessionCwd != "" {
			return sessionCwd
		}
	}
	return decodeDir(dir)
}

// memResolve traduit une clé en (dossier de base, chemin relatif), après validation stricte.
func memResolve(key string) (string, string, error) {
	invalidKeyErr := terr("srv.memory.fichierDeMemoireInvalide")
	switch {
	case key == "g/CLAUDE.md":
		return cfgDir(), "CLAUDE.md", nil
	case strings.HasPrefix(key, "g/rules/"):
		if fileName := strings.TrimPrefix(key, "g/rules/"); ruleName.MatchString(fileName) {
			return filepath.Join(cfgDir(), "rules"), fileName, nil
		}
	case strings.HasPrefix(key, "g/gldrules/"): // règles livrées par gld : lisibles ici, jamais modifiables
		if fileName := strings.TrimPrefix(key, "g/gldrules/"); ruleName.MatchString(fileName) {
			return builtinDir(), fileName, nil
		}
	case strings.HasPrefix(key, "m/"):
		// Le dossier du projet doit exister : sans quoi chaque nom reçu créerait un dossier dans projects/.
		if projectDir, fileName, hasSeparator := strings.Cut(key[2:], "/"); hasSeparator && safeDir(projectDir) && mdName.MatchString(fileName) && isDirPath(filepath.Join(root, projectDir)) {
			return filepath.Join(root, projectDir, "memory"), fileName, nil
		}
	case strings.HasPrefix(key, "r/"): // règle d'un projet : <dossier du projet>/.claude/rules/<nom>
		if projectDir, fileName, hasSeparator := strings.Cut(key[2:], "/"); hasSeparator && safeDir(projectDir) && ruleName.MatchString(fileName) {
			workingDir, isKnownProject := knownProjectCwd(projectDir)
			if !isKnownProject {
				return "", "", terr("srv.memory.dossierDuProjetIntrouvable")
			}
			base, err := projectRulesDir(workingDir)
			return base, fileName, err
		}
	case strings.HasPrefix(key, "p/"):
		if projectDir, fileName, hasSeparator := strings.Cut(key[2:], "/"); hasSeparator && safeDir(projectDir) {
			for _, projectFile := range projectFiles {
				if fileName == projectFile {
					workingDir, isKnownProject := knownProjectCwd(projectDir)
					if !isKnownProject {
						return "", "", terr("srv.memory.dossierDuProjetIntrouvable")
					}
					if globalClaude(filepath.Join(workingDir, filepath.FromSlash(fileName))) {
						return "", "", terr("srv.memory.cEstLeClaude")
					}
					return workingDir, filepath.FromSlash(fileName), nil
				}
			}
		}
	}
	return "", "", invalidKeyErr
}

// projectRulesDir : le dossier .claude/rules d'un projet ; refuse un projet absent, un lien symbolique et le dossier des règles
// globales (un projet lancé depuis ~).
func projectRulesDir(cwd string) (string, error) {
	if cwdInfo, statErr := os.Stat(cwd); cwd == "" || statErr != nil || !cwdInfo.IsDir() {
		return "", terr("srv.memory.dossierDuProjetIntrouvable")
	}
	rules := filepath.Join(cwd, ".claude", "rules")
	if isCfgEntry(rules, "rules") {
		return "", terr("srv.memory.ceSontLesRegles")
	}
	for _, claudePath := range []string{filepath.Join(cwd, ".claude"), rules} {
		if claudePathInfo, lstatErr := os.Lstat(claudePath); lstatErr == nil && claudePathInfo.Mode()&os.ModeSymlink != 0 {
			return "", terr("srv.memory.claudeEstUnLien")
		}
	}
	return rules, nil
}

func memFile(key, path string) (MemFile, bool) {
	fileInfo, err := os.Stat(path)
	if err != nil || !fileInfo.Mode().IsRegular() {
		return MemFile{}, false
	}
	memoryFile := MemFile{
		Key:   key,
		Name:  filepath.Base(path),
		KB:    fileInfo.Size() / bytesPerKilobyte,
		Mtime: float64(fileInfo.ModTime().UnixNano()) / nanosecondsPerSecond,
		Path:  path,
	}
	annotate(&memoryFile)
	return memoryFile, true
}

// isTempPath : un dossier de travail jetable (/tmp, $TMPDIR…) n'est pas une destination de déplacement.
func isTempPath(path string) bool {
	for _, tempRoot := range []string{"/tmp", "/var/tmp", "/private/tmp", os.TempDir()} {
		if path == tempRoot || strings.HasPrefix(path, strings.TrimRight(tempRoot, "/")+"/") {
			return true
		}
	}
	return false
}

// MemTarget : un projet vers la mémoire duquel une entrée peut être déplacée.
type MemTarget struct {
	Dir     string `json:"dir"`
	Project string `json:"project"`
}

// memTargets : un projet par dossier de transcripts, sans lire ses fichiers de mémoire (ce que memList fait en plus).
func memTargets() []MemTarget {
	dirs, _ := readDirTrusted(root)
	targets := []MemTarget{}
	for _, dirEntry := range dirs {
		if dirEntry.IsDir() {
			targets = append(targets, MemTarget{dirEntry.Name(), projectCwd(dirEntry.Name())})
		}
	}
	sort.Slice(targets, func(leftIndex, rightIndex int) bool { return targets[leftIndex].Project < targets[rightIndex].Project })
	return targets
}

// liveProjects : les projets dont le dossier de travail existe encore, avec ou sans fichier de mémoire. Les réglages, skills et
// .mcp.json d'un projet vivent dans son dossier, pas dans ~/.claude. ~ et ~/.claude n'en sont pas : leur .claude/ est le
// dossier de configuration lui-même, déjà lu comme utilisateur.
func liveProjects() []MemTarget {
	home, _ := os.UserHomeDir()
	targets := memTargets()
	liveTargets := []MemTarget{}
	for _, target := range targets {
		if target.Project == "" || target.Project == cfgDir() || (home != "" && target.Project == home) {
			continue
		}
		if projectInfo, err := os.Stat(target.Project); err == nil && projectInfo.IsDir() {
			liveTargets = append(liveTargets, target)
		}
	}
	return liveTargets
}

// pickerProjects : liveProjects sans les dossiers jetables, pour les sélecteurs de projet de l'interface.
func pickerProjects() []MemTarget {
	pickable := []MemTarget{}
	for _, liveProject := range liveProjects() {
		if !isTempPath(liveProject.Project) {
			pickable = append(pickable, liveProject)
		}
	}
	return pickable
}

func memList() ([]MemFile, []MemProject, []MemTarget) {
	global := []MemFile{}
	if memoryFile, isListed := memFile("g/CLAUDE.md", filepath.Join(cfgDir(), "CLAUDE.md")); isListed {
		global = append(global, memoryFile)
	}
	rules, _ := filepath.Glob(filepath.Join(cfgDir(), "rules", "*.md*"))
	for _, filePath := range rules {
		if memoryFile, isListed := memFile("g/rules/"+filepath.Base(filePath), filePath); isListed && ruleName.MatchString(memoryFile.Name) {
			memoryFile.Name = "rules/" + memoryFile.Name
			global = append(global, memoryFile)
		}
	}
	built, _ := filepath.Glob(filepath.Join(builtinDir(), "*.md"))
	for _, filePath := range built {
		if memoryFile, isListed := memFile("g/gldrules/"+filepath.Base(filePath), filePath); isListed && ruleName.MatchString(memoryFile.Name) {
			memoryFile.Name = "rules/" + memoryFile.Name
			global = append(global, memoryFile)
		}
	}
	projects := []MemProject{}
	targets := []MemTarget{}
	dirs, _ := readDirTrusted(root)
	for _, dirEntry := range dirs {
		if !dirEntry.IsDir() {
			continue
		}
		memoryProject := MemProject{Dir: dirEntry.Name(), Files: []MemFile{}}
		mems, _ := filepath.Glob(filepath.Join(root, dirEntry.Name(), "memory", "*.md"))
		for _, filePath := range mems {
			if memoryFile, isListed := memFile("m/"+dirEntry.Name()+"/"+filepath.Base(filePath), filePath); isListed && mdName.MatchString(memoryFile.Name) {
				memoryProject.Files = append(memoryProject.Files, memoryFile)
			}
		}
		memoryProject.Project = projectCwd(dirEntry.Name())
		isProjectMissing := memoryProject.Project == "" || !isDirPath(memoryProject.Project)
		if isProjectMissing {
			memoryProject.Missing = true
			if memoryProject.Project == "" { // nom encodé (« / » devenus « - ») : approximation lisible
				memoryProject.Project = strings.ReplaceAll(dirEntry.Name(), "-", "/")
			}
		} else {
			for _, projectFile := range projectFiles {
				path := filepath.Join(memoryProject.Project, filepath.FromSlash(projectFile))
				if globalClaude(path) {
					continue // déjà listé dans la section Globale
				}
				if memoryFile, isListed := memFile("p/"+dirEntry.Name()+"/"+projectFile, path); isListed {
					memoryFile.Name = projectFile
					memoryProject.Files = append(memoryProject.Files, memoryFile)
				}
			}
			if rulesDir, rulesDirErr := projectRulesDir(memoryProject.Project); rulesDirErr == nil {
				projectRuleFiles, _ := filepath.Glob(filepath.Join(rulesDir, "*.md*"))
				for _, filePath := range projectRuleFiles {
					if memoryFile, isListed := memFile("r/"+dirEntry.Name()+"/"+filepath.Base(filePath), filePath); isListed && ruleName.MatchString(memoryFile.Name) {
						memoryFile.Name = "rules/" + memoryFile.Name
						memoryProject.Files = append(memoryProject.Files, memoryFile)
					}
				}
			}
		}
		if len(memoryProject.Files) > 0 {
			projects = append(projects, memoryProject)
		}
		if !isTempPath(memoryProject.Project) {
			targets = append(targets, MemTarget{dirEntry.Name(), memoryProject.Project})
		}
	}
	sort.Slice(projects, func(leftIndex, rightIndex int) bool {
		return projects[leftIndex].Project < projects[rightIndex].Project
	})
	sort.Slice(targets, func(leftIndex, rightIndex int) bool { return targets[leftIndex].Project < targets[rightIndex].Project })
	return global, projects, targets
}

func memRead(key string) (string, error) {
	base, rel, err := memResolve(key)
	if err != nil {
		return "", err
	}
	memoryRoot, err := os.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer memoryRoot.Close()
	content, err := memoryRoot.ReadFile(rel)
	if err == nil && (len(content) > memMax || !utf8.Valid(content)) {
		err = terr("srv.memory.fichierTropGrosOu")
	}
	return string(content), err
}

// memSave remplace le fichier (écriture atomique, droits conservés) ou le crée ; la version remplacée va à la corbeille.
func memSave(key, text string) error { return memWrite(key, text, true) }

var memWriteMu sync.Mutex // deux écritures de la même entrée se partageaient le même fichier temporaire

// memWrite : keep met d'abord la version actuelle à la corbeille (pas pour les mises à jour d'index, dérivées).
func memWrite(key, text string, keep bool) error {
	if len(text) > memMax || !utf8.ValidString(text) {
		return terr("srv.memory.texteTropLongOu")
	}
	memWriteMu.Lock()
	defer memWriteMu.Unlock()
	base, rel, err := memResolve(key)
	if err != nil {
		return err
	}
	old, readErr := memRead(key)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr // trop gros ou pas en UTF-8 : le remplacer le perdrait sans passer par la corbeille
	}
	if keep && readErr == nil && old != text {
		if trashErr := toTrash(key, "remplacé"); trashErr != nil {
			return trashErr
		}
	}
	if mkdirErr := mkdirTrusted(base, 0o755); mkdirErr != nil {
		return mkdirErr
	}
	memoryRoot, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer memoryRoot.Close()
	mode := os.FileMode(0o644)
	if fileInfo, statErr := memoryRoot.Stat(rel); statErr == nil {
		mode = fileInfo.Mode().Perm()
	}
	if parentDir := filepath.Dir(rel); parentDir != "." {
		if mkdirAllErr := memoryRoot.MkdirAll(parentDir, 0o755); mkdirAllErr != nil {
			return mkdirAllErr
		}
	}
	tempName := rel + ".gld-web.tmp"
	if tempWriteErr := memoryRoot.WriteFile(tempName, []byte(text), mode); tempWriteErr != nil {
		return tempWriteErr
	}
	if renameErr := memoryRoot.Rename(tempName, rel); renameErr != nil {
		memoryRoot.Remove(tempName)
		return renameErr
	}
	return nil
}

// memRemove met le fichier à la corbeille puis le supprime ; pour une entrée de mémoire de projet,
// sa ligne d'index dans MEMORY.md part aussi.
func memRemove(key string) error { return memDelete(key, true) }

func memDelete(key string, trash bool) error {
	base, rel, err := memResolve(key)
	if err != nil {
		return err
	}
	if trash {
		if trashErr := toTrash(key, "supprimé"); trashErr != nil {
			return trashErr
		}
	}
	memoryRoot, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer memoryRoot.Close()
	if removeErr := memoryRoot.Remove(rel); removeErr != nil {
		return removeErr
	}
	if !strings.HasPrefix(key, "m/") || rel == "MEMORY.md" {
		return nil
	}
	indexContent, err := memoryRoot.ReadFile("MEMORY.md")
	if err != nil {
		return nil // pas d'index : rien à nettoyer
	}
	var keep []string
	for _, indexLine := range strings.SplitAfter(string(indexContent), "\n") {
		if !strings.Contains(indexLine, "]("+rel+")") {
			keep = append(keep, indexLine)
		}
	}
	if rewrittenIndex := strings.Join(keep, ""); rewrittenIndex != string(indexContent) {
		return memWrite(strings.TrimSuffix(key, rel)+"MEMORY.md", rewrittenIndex, false)
	}
	return nil
}

const mergeMax = 100

// mergePrompt : la demande de fusion faite à Claude, et le dossier où la lancer (le projet concerné, sinon ~).
// outputKey : le document de sortie, l'un des fichiers choisis (vide : le premier).
func mergePrompt(keys []string, outputKey string) (string, string, error) {
	if len(keys) < 2 || len(keys) > mergeMax {
		return "", "", terr("srv.memory.choisisDeAFichiers", "A1", mergeMax)
	}
	if outputKey == "" {
		outputKey = keys[0]
	}
	// Hors sélection : CLAUDE.md global ou fichier du projet (CLAUDE.md, AGENTS.md…), à créer s'il n'existe pas.
	outPath, extra := "", ""
	if !slices.Contains(keys, outputKey) {
		if outputKey != "g/CLAUDE.md" && !strings.HasPrefix(outputKey, "p/") {
			return "", "", terr("srv.memory.documentDeSortieInvalide")
		}
		outputBase, outputRel, outputResolveErr := memResolve(outputKey)
		if outputResolveErr != nil {
			return "", "", outputResolveErr
		}
		outPath, extra = filepath.Join(outputBase, outputRel), L("prompt.mergeOutExtra")
	}
	home, _ := os.UserHomeDir()
	cwd := ""
	var prompt strings.Builder
	prompt.WriteString(L("prompt.mergeHeader") + "\n")
	for _, key := range keys {
		base, rel, resolveErr := memResolve(key)
		if resolveErr != nil {
			return "", "", resolveErr
		}
		filePath := filepath.Join(base, rel)
		if _, statErr := os.Stat(filePath); statErr != nil {
			return "", "", terr("srv.memory.introuvable", "A1", filePath)
		}
		prompt.WriteString("- " + filePath + "\n")
		if key == outputKey {
			outPath = filePath
		}
		projectDir, _, hasSeparator := strings.Cut(key[2:], "/")
		isProjectKey := key[0] == 'm' || key[0] == 'p'
		if cwd == "" && isProjectKey && hasSeparator {
			if projectWorkingDir := projectCwd(projectDir); projectWorkingDir != "" && isDirPath(projectWorkingDir) {
				cwd = projectWorkingDir
			}
		}
	}
	prompt.WriteString("\n" + L("prompt.mergeRules", "A1", outPath, "A2", extra) + "\n")
	if cwd == "" { // règles globales : le dossier de configuration, plutôt que ~ tout entier (autre dossier à approuver)
		if configDir := filepath.Join(home, ".claude"); isDirPath(configDir) {
			cwd = configDir
		} else {
			cwd = home
		}
	}
	return prompt.String(), cwd, nil
}

// memTransfer déplace une entrée de mémoire de projet ou une règle vers la mémoire d'un autre projet (destination = son dossier de
// transcripts) ou vers les règles globales (destination = "global"), ou la copie si keep. Sans écraser ; l'index suit.
func memTransfer(key, destination string, keep bool) (string, error) {
	if !strings.HasPrefix(key, "m/") && !strings.HasPrefix(key, "g/rules/") && !strings.HasPrefix(key, "r/") {
		return "", terr("srv.memory.seulesLesEntreesDe")
	}
	_, rel, err := memResolve(key)
	if err != nil {
		return "", err
	}
	destinationKey := "g/rules/" + rel
	switch {
	case destination == "global":
	case strings.HasPrefix(destination, "r:"): // règles d'un projet (to = « r:<dossier> »)
		if !safeDir(destination[2:]) {
			return "", terr("srv.memory.destinationInvalide")
		}
		destinationKey = "r/" + destination[2:] + "/" + rel
		if _, _, destinationErr := memResolve(destinationKey); destinationErr != nil {
			return "", destinationErr
		}
	case strings.HasPrefix(key, "r/") && destination != strings.Split(key, "/")[1]: // une règle ne retourne qu'à la mémoire de son propre projet
		return "", terr("srv.memory.uneRegleDeProjet")
	default:
		if !safeDir(destination) {
			return "", terr("srv.memory.destinationInvalide")
		}
		if projectInfo, statErr := os.Stat(filepath.Join(root, destination)); statErr != nil || !projectInfo.IsDir() {
			return "", terr("srv.memory.projetDeDestinationIntrouvable")
		}
		destinationKey = "m/" + destination + "/" + rel
	}
	if destinationKey == key {
		return "", terr("srv.memory.dejaACetEndroit")
	}
	if _, existingErr := memRead(destinationKey); existingErr == nil || !errors.Is(existingErr, os.ErrNotExist) {
		return "", terr("srv.memory.unFichierDuMeme")
	}
	text, err := memRead(key)
	if err != nil {
		return "", err
	}
	newIndexLine := "- [" + strings.TrimSuffix(rel, ".md") + "](" + rel + ")\n"
	if strings.HasPrefix(key, "m/") { // la ligne d'index d'origine, avec son accroche, part avec l'entrée
		if sourceIndex, sourceIndexErr := memRead(key[:strings.LastIndex(key, "/")+1] + "MEMORY.md"); sourceIndexErr == nil {
			for _, indexLine := range strings.SplitAfter(sourceIndex, "\n") {
				if strings.Contains(indexLine, "]("+rel+")") {
					newIndexLine = strings.TrimRight(indexLine, "\n") + "\n"
					break
				}
			}
		}
	}
	if saveErr := memSave(destinationKey, text); saveErr != nil {
		return "", saveErr
	}
	if !keep {
		if deleteErr := memDelete(key, false); deleteErr != nil { // déplacé, pas supprimé : rien à mettre à la corbeille
			return "", deleteErr
		}
	}
	if strings.HasPrefix(destinationKey, "m/") && rel != "MEMORY.md" {
		indexKey := destinationKey[:strings.LastIndex(destinationKey, "/")+1] + "MEMORY.md"
		destinationIndex, _ := memRead(indexKey)
		if destinationIndex != "" && !strings.HasSuffix(destinationIndex, "\n") {
			destinationIndex += "\n"
		}
		if indexWriteErr := memWrite(indexKey, destinationIndex+newIndexLine, false); indexWriteErr != nil {
			return "", indexWriteErr
		}
	}
	return destinationKey, nil
}

func isDirPath(path string) bool {
	fileInfo, err := os.Stat(path)
	return err == nil && fileInfo.IsDir()
}

// memRename renomme un fichier dans son dossier ; la ligne d'index d'une entrée de projet suit.
func memRename(key, name string) (string, error) {
	_, rel, err := memResolve(key)
	if err != nil {
		return "", err
	}
	if rel == "MEMORY.md" || !(strings.HasPrefix(key, "m/") || strings.HasPrefix(key, "g/rules/") || strings.HasPrefix(key, "r/")) {
		return "", terr("srv.memory.seulesLesEntreesDe")
	}
	destinationKey := key[:strings.LastIndex(key, "/")+1] + name
	if destinationKey == key {
		return "", terr("srv.memory.dejaACetEndroit")
	}
	if _, _, destinationErr := memResolve(destinationKey); destinationErr != nil || name == "MEMORY.md" {
		return "", terr("srv.memory.destinationInvalide")
	}
	if _, existingErr := memRead(destinationKey); existingErr == nil || !errors.Is(existingErr, os.ErrNotExist) {
		return "", terr("srv.memory.unFichierDuMeme")
	}
	text, err := memRead(key)
	if err != nil {
		return "", err
	}
	newIndexLine := "- [" + strings.TrimSuffix(name, ".md") + "](" + name + ")\n"
	indexKey := key[:strings.LastIndex(key, "/")+1] + "MEMORY.md"
	if strings.HasPrefix(key, "m/") {
		if currentIndex, indexReadErr := memRead(indexKey); indexReadErr == nil {
			for _, indexLine := range strings.SplitAfter(currentIndex, "\n") {
				if strings.Contains(indexLine, "]("+rel+")") {
					newIndexLine = strings.ReplaceAll(strings.TrimRight(indexLine, "\n"), "]("+rel+")", "]("+name+")") + "\n"
					break
				}
			}
		}
	}
	if saveErr := memSave(destinationKey, text); saveErr != nil {
		return "", saveErr
	}
	if deleteErr := memDelete(key, false); deleteErr != nil {
		return "", deleteErr
	}
	if strings.HasPrefix(key, "m/") {
		updatedIndex, _ := memRead(indexKey)
		if updatedIndex != "" && !strings.HasSuffix(updatedIndex, "\n") {
			updatedIndex += "\n"
		}
		if indexWriteErr := memWrite(indexKey, updatedIndex+newIndexLine, false); indexWriteErr != nil {
			return "", indexWriteErr
		}
	}
	return destinationKey, nil
}
