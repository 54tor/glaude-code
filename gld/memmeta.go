// Métadonnées et santé de la mémoire : front matter (type, paths), budget de contexte, index MEMORY.md,
// imports « @fichier », liens « [[nom]] », aperçu des fichiers qu'une règle scopée cible, recherche plein texte.
package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v3"
)

const indexLines = 200 // Claude Code ne charge que les 200 premières lignes de MEMORY.md

const (
	memExcerptMax      = 120
	globMaxLength      = 200
	globMaxBraces      = 4
	memMatchTimeout    = 5 * time.Second
	memMatchMaxVisited = 50000
	memMatchMaxFiles   = 50
	memSearchMinQuery  = 2
)

// frontMatter : le bloc YAML en tête du texte et le reste. hasFrontMatter est faux s'il n'y en a pas.
var blankLinesRe = regexp.MustCompile(`\n{2,}`)

func frontMatter(text string) (yamlText, body string, hasFrontMatter bool) {
	if !strings.HasPrefix(text, "---\n") {
		return "", text, false
	}
	rest := text[4:]
	separatorIndex := strings.Index("\n"+rest, "\n---")
	if separatorIndex < 0 {
		return "", text, false
	}
	yamlText = rest[:max(separatorIndex-1, 0)]
	after := rest[min(separatorIndex+3, len(rest)):]
	if newlineIndex := strings.Index(after, "\n"); newlineIndex >= 0 {
		after = after[newlineIndex+1:]
	} else {
		after = ""
	}
	return yamlText, after, true
}

// parseMeta : le front matter décodé. Une erreur YAML est renvoyée telle quelle (affichée comme avertissement).
func parseMeta(text string) (map[string]any, error) {
	yamlText, _, hasFrontMatter := frontMatter(text)
	if !hasFrontMatter {
		if strings.HasPrefix(text, "---\n") {
			return nil, terr("srv.memmeta.frontMatterNonFerme")
		}
		return nil, nil
	}
	var meta map[string]any
	if err := yaml.Unmarshal([]byte(yamlText), &meta); err != nil {
		return nil, terr("srv.memmeta.frontMatterInvalide", "A1", cut(strings.ReplaceAll(err.Error(), "\n", " "), memExcerptMax))
	}
	return meta, nil
}

// metaString : un champ texte du front matter, sur une ligne (une valeur repliée « >- » comprise) ; "" s'il manque.
func metaString(text, key string) string {
	meta, _ := parseMeta(text)
	value, _ := meta[key].(string)
	return strings.Join(strings.Fields(value), " ")
}

// globsOf : les motifs du champ `paths:` (liste ou chaîne, séparés par des virgules).
func globsOf(meta map[string]any) ([]string, error) {
	var patterns []string
	switch pathsValue := meta["paths"].(type) {
	case nil:
		return nil, nil
	case string:
		patterns = splitGlobs(pathsValue)
	case []any:
		for _, element := range pathsValue {
			pattern, isString := element.(string)
			if !isString || strings.TrimSpace(pattern) == "" {
				return nil, terr("srv.memmeta.pathsDoitContenirDes")
			}
			patterns = append(patterns, strings.TrimSpace(pattern))
		}
	default:
		return nil, terr("srv.memmeta.pathsDoitEtreUne")
	}
	for _, pattern := range patterns {
		if !globOK(pattern) {
			return nil, terr("srv.memmeta.motifInvalide", "A1", pattern)
		}
	}
	if len(patterns) == 0 {
		return nil, terr("srv.memmeta.pathsEstVideLa")
	}
	return patterns, nil
}

// globOK : motif valide et borné. doublestar recurse à chaque alternative d'accolade : n groupes coûtent 2^n appels.
func globOK(pattern string) bool {
	return len(pattern) <= globMaxLength && strings.Count(pattern, "{") <= globMaxBraces && doublestar.ValidatePattern(pattern)
}

// splitGlobs sépare des motifs par des virgules, hors accolades : « a/*.{ts,tsx}, b/** » donne deux motifs.
func splitGlobs(text string) []string {
	var patterns []string
	depth, start := 0, 0
	add := func(end int) {
		if pattern := strings.TrimSpace(text[start:end]); pattern != "" {
			patterns = append(patterns, pattern)
		}
	}
	for byteIndex, character := range text {
		switch character {
		case '{':
			depth++
		case '}':
			depth = max(depth-1, 0)
		case ',':
			if depth == 0 {
				add(byteIndex)
				start = byteIndex + 1
			}
		}
	}
	add(len(text))
	return patterns
}

// isClaudeFile : un CLAUDE.md (celui que Claude Code charge), par opposition aux autres fichiers de documentation du projet.
func isClaudeFile(key string) bool {
	return strings.HasSuffix(key, "CLAUDE.md") || strings.HasSuffix(key, "CLAUDE.local.md")
}

// annotate complète un MemFile : type, portée, budget de contexte, chargement à chaque session, avertissement.
func annotate(memoryFile *MemFile) {
	memoryFile.Off = strings.HasSuffix(memoryFile.Name, ".off")
	text, err := memRead(memoryFile.Key)
	if err != nil {
		return
	}
	budgetLength := len(text)
	if strings.HasSuffix(memoryFile.Key, "/MEMORY.md") { // seules les 200 premières lignes comptent
		lines := strings.SplitAfter(text, "\n")
		if len(lines) > indexLines {
			budgetLength = len(strings.Join(lines[:indexLines], ""))
		}
	}
	memoryFile.Tokens = estimateTokens(budgetLength)
	if exactTokens, known := knownTokens(text[:budgetLength]); known {
		memoryFile.Tokens, memoryFile.Exact = exactTokens, true
	}
	meta, err := parseMeta(text)
	if err != nil {
		memoryFile.Warn = err.Error()
	}
	if memoryType, _ := meta["type"].(string); memoryType != "" {
		memoryFile.Type = memoryType
	} else if metadata, _ := meta["metadata"].(map[string]any); metadata != nil {
		memoryFile.Type, _ = metadata["type"].(string)
	}
	source, _ := meta["source"].(string)
	if metadata, _ := meta["metadata"].(map[string]any); source == "" && metadata != nil {
		source, _ = metadata["source"].(string)
	}
	isGldRulesDir := strings.HasPrefix(memoryFile.Key, "g/gldrules/")
	isGldSourcedRule := source == "gld" && strings.HasPrefix(memoryFile.Key, "g/rules/")
	memoryFile.Gld = isGldRulesDir || isGldSourcedRule
	rule := strings.HasPrefix(memoryFile.Key, "g/rules/") || strings.HasPrefix(memoryFile.Key, "r/")
	if rule && meta != nil {
		if _, has := meta["paths"]; has {
			patterns, globsErr := globsOf(meta)
			if globsErr != nil {
				memoryFile.Warn = globsErr.Error()
			}
			memoryFile.Paths = patterns
			if patterns == nil {
				memoryFile.Paths = []string{}
			}
		}
	}
	switch {
	case memoryFile.Off:
	case memoryFile.Key == "g/CLAUDE.md", strings.HasPrefix(memoryFile.Key, "p/") && isClaudeFile(memoryFile.Key):
		memoryFile.Always = true
	case rule:
		memoryFile.Always = memoryFile.Paths == nil
	case strings.HasSuffix(memoryFile.Key, "/MEMORY.md"):
		memoryFile.Always = true
	}
}

// --- santé -------------------------------------------------------------------------------------

type Issue struct {
	Key  string `json:"key"`
	Kind string `json:"kind"` // index-ligne, index-absent, index-long, import, lien, front-matter, perime
	Msg  string `json:"msg"`
	Dir  string `json:"dir,omitempty"` // dossier de mémoire concerné, pour la réparation de l'index
}

var (
	mdLink    = regexp.MustCompile(`\]\(([^)\s:]+\.md)\)`) // sans « : » : une URL (https:, mailto:) n'est pas un fichier de mémoire
	wikiLink  = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)
	importRef = regexp.MustCompile(`(?:^|\s)@([~./\w][^\s]*[./][^\s]*)`)
)

const staleAfter = 180 * 24 * time.Hour

// memHealth : les incohérences de la mémoire, sans rien modifier.
func memHealth() []Issue {
	issues := []Issue{}
	global, projects, _ := memList()
	check := func(memoryFile MemFile) {
		if memoryFile.Warn != "" {
			issues = append(issues, Issue{Key: memoryFile.Key, Kind: "front-matter", Msg: memoryFile.Warn})
		}
	}
	imports := func(memoryFile MemFile) {
		text, err := memRead(memoryFile.Key)
		if err != nil || memoryFile.Off {
			return
		}
		base, rel, err := memResolve(memoryFile.Key)
		if err != nil {
			return
		}
		dir := filepath.Dir(filepath.Join(base, rel))
		home, _ := os.UserHomeDir()
		inCode := false
		for _, textLine := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(textLine), "```") {
				inCode = !inCode
			}
			if inCode || strings.HasPrefix(strings.TrimSpace(textLine), "`") {
				continue
			}
			for _, match := range importRef.FindAllStringSubmatch(textLine, -1) {
				importPath := strings.TrimRight(match[1], ".,;:)")
				switch {
				case strings.HasPrefix(importPath, "~/"):
					importPath = filepath.Join(home, importPath[2:])
				case !filepath.IsAbs(importPath):
					importPath = filepath.Join(dir, importPath)
				}
				if _, statErr := os.Stat(importPath); statErr != nil {
					issues = append(issues, Issue{Key: memoryFile.Key, Kind: "import", Msg: L("srv.memmeta.importMissing", "A1", match[1])})
				}
			}
		}
	}
	// Les règles et CLAUDE.md citent des mémoires de n'importe quel projet : un lien est cassé si aucun projet n'a cette entrée.
	allSlugs := map[string]bool{}
	for _, memoryProject := range projects {
		for _, memoryFile := range memoryProject.Files {
			if strings.HasPrefix(memoryFile.Key, "m/") {
				allSlugs[strings.TrimSuffix(memoryFile.Name, ".md")] = true
			}
		}
	}
	links := func(memoryFile MemFile) {
		text, err := memRead(memoryFile.Key)
		if err != nil {
			return
		}
		for _, match := range wikiLink.FindAllStringSubmatch(text, -1) {
			if !allSlugs[strings.TrimSuffix(strings.TrimSpace(match[1]), ".md")] {
				issues = append(issues, Issue{Key: memoryFile.Key, Kind: "lien", Msg: L("srv.memmeta.brokenLink", "A1", match[1])})
			}
		}
	}
	for _, memoryFile := range global {
		check(memoryFile)
		links(memoryFile)
		if memoryFile.Key == "g/CLAUDE.md" {
			imports(memoryFile)
		}
	}
	for _, memoryProject := range projects {
		names := map[string]bool{}
		var entries []MemFile
		for _, memoryFile := range memoryProject.Files {
			check(memoryFile)
			if !strings.HasPrefix(memoryFile.Key, "m/") {
				links(memoryFile)
			}
			if strings.HasPrefix(memoryFile.Key, "p/") && isClaudeFile(memoryFile.Key) { // les @mentions d'un README ne sont pas des imports
				imports(memoryFile)
			}
			if strings.HasPrefix(memoryFile.Key, "m/") {
				names[memoryFile.Name] = true
				if memoryFile.Name != "MEMORY.md" {
					entries = append(entries, memoryFile)
				}
			}
		}
		if len(names) == 0 {
			continue
		}
		indexKey := "m/" + memoryProject.Dir + "/MEMORY.md"
		indexText, indexErr := memRead(indexKey)
		if indexErr != nil && names["MEMORY.md"] {
			continue
		}
		listed := map[string]bool{}
		if indexErr == nil {
			if lineCount := len(strings.Split(strings.TrimRight(indexText, "\n"), "\n")); lineCount > indexLines {
				issues = append(issues, Issue{Key: indexKey, Kind: "index-long", Dir: memoryProject.Dir,
					Msg: L("srv.memmeta.indexTooLong", "A1", lineCount, "A2", indexLines)})
			}
			for _, match := range mdLink.FindAllStringSubmatch(indexText, -1) {
				listed[match[1]] = true
				if !names[match[1]] {
					issues = append(issues, Issue{Key: indexKey, Kind: "index-ligne", Dir: memoryProject.Dir, Msg: L("srv.memmeta.indexDangling", "A1", match[1])})
				}
			}
		}
		slugs := map[string]bool{}
		for _, memoryFile := range entries {
			slugs[strings.TrimSuffix(memoryFile.Name, ".md")] = true
			if !listed[memoryFile.Name] {
				issues = append(issues, Issue{Key: memoryFile.Key, Kind: "index-absent", Dir: memoryProject.Dir, Msg: L("srv.memmeta.notInIndex")})
			}
			if time.Since(time.Unix(int64(memoryFile.Mtime), 0)) > staleAfter {
				issues = append(issues, Issue{Key: memoryFile.Key, Kind: "perime", Msg: L("srv.memmeta.stale")})
			}
		}
		for _, memoryFile := range entries {
			text, readErr := memRead(memoryFile.Key)
			if readErr != nil {
				continue
			}
			for _, match := range wikiLink.FindAllStringSubmatch(text, -1) {
				if !slugs[strings.TrimSuffix(strings.TrimSpace(match[1]), ".md")] {
					issues = append(issues, Issue{Key: memoryFile.Key, Kind: "lien", Msg: L("srv.memmeta.brokenLink", "A1", match[1])})
				}
			}
		}
	}
	return issues
}

// memRepairIndex : retire de MEMORY.md les lignes qui pointent vers un fichier absent et y ajoute les entrées qui n'y sont pas.
func memRepairIndex(dir string) (removed, added int, err error) {
	if !safeDir(dir) {
		return 0, 0, terr("srv.routes_sessions.dossierInvalide")
	}
	indexKey := "m/" + dir + "/MEMORY.md"
	indexText, _ := memRead(indexKey)
	files, _ := filepath.Glob(filepath.Join(root, dir, "memory", "*.md"))
	have := map[string]bool{}
	for _, filePath := range files {
		have[filepath.Base(filePath)] = true
	}
	var keep []string
	listed := map[string]bool{}
	for _, line := range strings.SplitAfter(indexText, "\n") {
		if match := mdLink.FindStringSubmatch(line); match != nil {
			if !have[match[1]] {
				removed++
				continue
			}
			listed[match[1]] = true
		}
		keep = append(keep, line)
	}
	rewritten := strings.Join(keep, "")
	if rewritten != "" && !strings.HasSuffix(rewritten, "\n") {
		rewritten += "\n"
	}
	sort.Strings(files)
	for _, filePath := range files {
		fileName := filepath.Base(filePath)
		if fileName == "MEMORY.md" || listed[fileName] || !mdName.MatchString(fileName) {
			continue
		}
		text, _ := memRead("m/" + dir + "/" + fileName)
		title := strings.TrimSuffix(fileName, ".md")
		if metaName := metaString(text, "name"); metaName != "" {
			title = metaName
		}
		line := "- [" + title + "](" + fileName + ")"
		if description := metaString(text, "description"); description != "" {
			line += " — " + description
		}
		rewritten += line + "\n"
		added++
	}
	if removed+added == 0 {
		return 0, 0, nil
	}
	return removed, added, memSave(indexKey, rewritten)
}

// --- portée des règles ----------------------------------------------------------------------------

// memMatches : les fichiers d'un projet ciblés par les motifs `paths:` d'une règle (au plus 50 renvoyés).
func memMatches(key, dir string) (files []string, total int, truncated bool, err error) {
	memoryFile, found := memFileByKey(key)
	if !found || len(memoryFile.Paths) == 0 {
		return nil, 0, false, terr("srv.memmeta.cetteRegleNEst")
	}
	if !safeDir(dir) {
		return nil, 0, false, terr("srv.memmeta.projetInvalide")
	}
	cwd := ""
	for _, liveProject := range liveProjects() {
		if liveProject.Dir == dir {
			cwd = liveProject.Project
		}
	}
	if cwd == "" {
		return nil, 0, false, terr("srv.memory.dossierDuProjetIntrouvable")
	}
	walkContext, cancel := context.WithTimeout(context.Background(), memMatchTimeout)
	defer cancel()
	visited := 0
	skip := map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, ".venv": true, "target": true}
	cwd = walkRoot(cwd)
	filepath.WalkDir(cwd, func(walkedPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() && skip[entry.Name()] && walkedPath != cwd {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		visited++
		if visited > memMatchMaxVisited || walkContext.Err() != nil {
			truncated = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(cwd, walkedPath)
		rel = filepath.ToSlash(rel)
		for _, pattern := range memoryFile.Paths {
			if matched, _ := doublestar.Match(pattern, rel); matched {
				total++
				if total <= memMatchMaxFiles {
					files = append(files, rel)
				}
				break
			}
		}
		return nil
	})
	return files, total, truncated, nil
}

func memFileByKey(key string) (MemFile, bool) {
	base, rel, err := memResolve(key)
	if err != nil {
		return MemFile{}, false
	}
	return memFile(key, filepath.Join(base, rel))
}

var pathsBlock = regexp.MustCompile(`(?m)^paths:[^\n]*\n(?:[ \t]+[^\n]*\n|-[ \t][^\n]*\n)*`)

// setPaths remplace (ou ajoute, ou retire si paths est vide) le champ `paths:` du front matter, sans toucher au reste.
func setPaths(text string, paths []string) (string, error) {
	var newBlock strings.Builder
	if len(paths) > 0 {
		newBlock.WriteString("paths:\n")
		for _, pattern := range paths {
			if !globOK(pattern) || strings.ContainsAny(pattern, "\n\"") {
				return "", terr("srv.memmeta.motifInvalide", "A1", pattern)
			}
			newBlock.WriteString("  - \"" + pattern + "\"\n")
		}
	}
	yamlText, body, hasFrontMatter := frontMatter(text)
	if !hasFrontMatter {
		if newBlock.Len() == 0 {
			return text, nil
		}
		return "---\n" + newBlock.String() + "---\n\n" + text, nil
	}
	yamlText = strings.TrimRight(yamlText, "\n")
	if yamlText != "" {
		yamlText += "\n"
	}
	if pathsBlock.MatchString(yamlText) {
		yamlText = pathsBlock.ReplaceAllString(yamlText, "")
	}
	yamlText += newBlock.String()
	if strings.TrimSpace(yamlText) == "" {
		return strings.TrimLeft(body, "\n"), nil
	}
	updated := "---\n" + yamlText + "---\n" + body
	if _, err := parseMeta(updated); err != nil {
		return "", err
	}
	return updated, nil
}

// memTypes : les types d'entrée de mémoire prévus par Claude Code.
var memTypes = []string{"user", "feedback", "project", "reference"}

var (
	typeIndented = regexp.MustCompile(`(?m)^([ \t]+type:)[^\n]*$`)
	typeTop      = regexp.MustCompile(`(?m)^type:[^\n]*$`)
	metaLine     = regexp.MustCompile(`(?m)^metadata:[ \t]*$`)
	topKey       = regexp.MustCompile(`(?m)^[^ \t\n#]`)
)

// setType fixe metadata.type dans le front matter (créé au besoin), sans toucher aux autres champs ; "" retire le type.
// metaBlock : les lignes indentées sous metadata: (vide s'il n'y en a pas), jusqu'à la prochaine clé de premier niveau.
func metaBlock(yamlText string) (start, end int) {
	loc := metaLine.FindStringIndex(yamlText)
	if loc == nil {
		return len(yamlText), len(yamlText)
	}
	start = loc[1]
	if next := topKey.FindStringIndex(yamlText[start+1:]); next != nil {
		return start, start + 1 + next[0]
	}
	return start, len(yamlText)
}

func setType(text, memoryType string) (string, error) {
	if memoryType != "" && !slices.Contains(memTypes, memoryType) {
		return "", terr("srv.memmeta.typeInconnu", "A1", memoryType)
	}
	yamlText, body, hasFrontMatter := frontMatter(text)
	if !hasFrontMatter {
		if memoryType == "" {
			return text, nil
		}
		return "---\nmetadata:\n  type: " + memoryType + "\n---\n\n" + text, nil
	}
	// Un en-tête déjà invalide pour un lecteur YAML strict (description avec « : » non guillemetée, très courant) se modifie quand même :
	// la modification est textuelle, et on ne refuse que de rendre invalide un en-tête qui ne l'était pas.
	_, origErr := parseMeta(text)
	yamlText = strings.TrimRight(yamlText, "\n") + "\n"
	// Seul le type: indenté sous metadata: est le nôtre : un « type: » sous une autre clé (hooks…) n'est jamais touché.
	metaStart, metaEnd := metaBlock(yamlText)
	block := yamlText[metaStart:metaEnd]
	switch {
	case memoryType == "":
		yamlText = yamlText[:metaStart] + typeIndented.ReplaceAllString(block, "") + yamlText[metaEnd:]
		yamlText = typeTop.ReplaceAllString(yamlText, "")
	case typeIndented.MatchString(block):
		done := false
		yamlText = yamlText[:metaStart] + typeIndented.ReplaceAllStringFunc(block, func(textLine string) string {
			if done {
				return textLine
			}
			done = true
			return typeIndented.FindStringSubmatch(textLine)[1] + " " + memoryType
		}) + yamlText[metaEnd:]
	case typeTop.MatchString(yamlText):
		yamlText = typeTop.ReplaceAllString(yamlText, "type: "+memoryType)
	case metaLine.MatchString(yamlText):
		yamlText = metaLine.ReplaceAllString(yamlText, "metadata:\n  type: "+memoryType)
	default:
		yamlText += "metadata:\n  type: " + memoryType + "\n"
	}
	yamlText = blankLinesRe.ReplaceAllString(yamlText, "\n")
	updated := "---\n" + yamlText + "---\n" + body
	if origErr == nil {
		if _, err := parseMeta(updated); err != nil {
			return "", err
		}
	}
	return updated, nil
}

// --- recherche plein texte ---------------------------------------------------------------------------

// memSearch : les fichiers de mémoire dont le contenu contient query (insensible à la casse), avec la première ligne trouvée.
func memSearch(query string) map[string]string {
	hits := map[string]string{}
	query = strings.ToLower(strings.TrimSpace(query))
	if len(query) < memSearchMinQuery {
		return hits
	}
	global, projects, _ := memList()
	all := append([]MemFile{}, global...)
	for _, memoryProject := range projects {
		all = append(all, memoryProject.Files...)
	}
	for _, memoryFile := range all {
		text, err := memRead(memoryFile.Key)
		if err != nil {
			continue
		}
		for _, textLine := range strings.Split(text, "\n") {
			if strings.Contains(strings.ToLower(textLine), query) {
				hits[memoryFile.Key] = cut(strings.TrimSpace(textLine), memExcerptMax)
				break
			}
		}
	}
	return hits
}

// memToggle : désactive une règle globale (x.md devient x.md.off, Claude ne la charge plus) ou la réactive.
func memToggle(key string) (string, error) {
	if !strings.HasPrefix(key, "g/rules/") {
		return "", terr("srv.memmeta.seulesLesReglesGlobales")
	}
	base, rel, err := memResolve(key)
	if err != nil {
		return "", err
	}
	target := rel + ".off"
	if strings.HasSuffix(rel, ".off") {
		target = strings.TrimSuffix(rel, ".off")
	}
	rulesRoot, err := os.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer rulesRoot.Close()
	if _, statErr := rulesRoot.Stat(target); statErr == nil {
		return "", terr("srv.memmeta.existeDeja", "A1", target)
	}
	if renameErr := rulesRoot.Rename(rel, target); renameErr != nil {
		return "", renameErr
	}
	return "g/rules/" + target, nil
}

var fmScalar = regexp.MustCompile(`^(\s*[A-Za-z0-9_.-]+):[ \t]+(\S.*?)\s*$`)

// fixFrontMatter met entre guillemets les valeurs en texte brut qui rendent le YAML invalide (« : » ou « # » dans une description).
// Refuse si l'en-tête reste invalide après coup, ou s'il n'y avait rien à corriger.
func fixFrontMatter(text string) (string, error) {
	yamlText, body, hasFrontMatter := frontMatter(text)
	if !hasFrontMatter {
		return "", terr("srv.memmeta.pasDeFrontMatter")
	}
	if _, originalErr := parseMeta(text); originalErr == nil {
		return "", terr("srv.memmeta.leFrontMatterEst")
	}
	lines := strings.Split(strings.TrimRight(yamlText, "\n"), "\n")
	for lineIndex, textLine := range lines {
		match := fmScalar.FindStringSubmatch(textLine)
		if match == nil || strings.ContainsRune("\"'[{|>&*!%@`", rune(match[2][0])) {
			continue
		}
		breaksYAML := strings.Contains(match[2], ": ") || strings.Contains(match[2], " #") || strings.HasSuffix(match[2], ":")
		if breaksYAML {
			backslashEscaped := strings.ReplaceAll(match[2], `\`, `\\`)
			quoted := strings.ReplaceAll(backslashEscaped, `"`, `\"`)
			lines[lineIndex] = match[1] + `: "` + quoted + `"`
		}
	}
	fixed := "---\n" + strings.Join(lines, "\n") + "\n---\n" + body
	if _, fixedErr := parseMeta(fixed); fixedErr != nil {
		return "", terr("srv.memmeta.pasDeCorrectionAutomatique", "A1", fixedErr.Error())
	}
	return fixed, nil
}
