// Snippets = commandes personnalisées de Claude Code : des fichiers Markdown de prompt, utilisables aussi en « /nom ».
// Personnels dans ~/.claude/commands (ou GLD_WEB_SNIPPETS), d'un projet dans <projet>/.claude/commands.
// Clés : u/<chemin> (personnels), p/<dossier de transcripts>/<chemin> (d'un projet). Tout passe par os.Root.
package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

type Snippet struct {
	Key         string  `json:"key"`
	Scope       string  `json:"scope"`   // "" : personnels ; sinon le dossier de transcripts du projet
	Project     string  `json:"project"` // dossier du projet (vide pour les personnels)
	Path        string  `json:"path"`    // relatif au dossier des commandes, séparateur « / »
	Command     string  `json:"command"` // ce qu'on tape dans Claude Code : /nom ou /dossier:nom
	Description string  `json:"description"`
	KB          int64   `json:"kb"`
	Mtime       float64 `json:"mtime"`
}

const snippetMax = 256 << 10

// snipDir : GLD_WEB_SNIPPETS, qui remplace ~/.claude/commands pour les snippets personnels.
var snipDir string

func userCmdDir() string {
	if snipDir != "" {
		return snipDir
	}
	return filepath.Join(cfgDir(), "commands")
}

func sameDir(first, second string) bool {
	if filepath.Clean(first) == filepath.Clean(second) {
		return true
	}
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	return firstErr == nil && secondErr == nil && os.SameFile(firstInfo, secondInfo)
}

// snippetRel valide un chemin : relatif, sans « .. », sans dossier ni fichier caché, et en .md (une commande).
func snippetRel(rel string) (string, error) {
	rel = filepath.FromSlash(strings.TrimSpace(rel))
	if !filepath.IsLocal(rel) || !strings.HasSuffix(rel, ".md") {
		return "", terr("srv.commands.cheminDeSnippetInvalide")
	}
	for _, segment := range strings.Split(rel, string(filepath.Separator)) {
		if strings.HasPrefix(segment, ".") {
			return "", terr("srv.commands.cheminDeSnippetInvalide2")
		}
	}
	return rel, nil
}

// cmdBase traduit une clé en (dossier des commandes, chemin relatif).
func cmdBase(key string) (string, string, error) {
	var base, rel string
	switch {
	case strings.HasPrefix(key, "u/"):
		base, rel = userCmdDir(), key[2:]
	case strings.HasPrefix(key, "p/"):
		transcriptDir, relPath, found := strings.Cut(key[2:], "/")
		if !found || !safeDir(transcriptDir) {
			return "", "", terr("srv.memmeta.projetInvalide")
		}
		projectDir, found := knownProjectCwd(transcriptDir)
		if !found {
			return "", "", terr("srv.memory.dossierDuProjetIntrouvable")
		}
		base, rel = filepath.Join(projectDir, ".claude", "commands"), relPath
		if sameDir(base, userCmdDir()) {
			return "", "", terr("srv.commands.ceSontLesCommandes")
		}
	default:
		return "", "", terr("srv.commands.snippetInvalide")
	}
	rel, err := snippetRel(rel)
	return base, rel, err
}

// describe : la description de l'en-tête YAML, s'il y en a un.
func describe(text string) string { return metaString(text, "description") }

func walkCommands(base, scope, project string, snippets []Snippet) []Snippet {
	prefix := "u/"
	if scope != "" {
		prefix = "p/" + scope + "/"
	}
	base = walkRoot(base)
	filepath.WalkDir(base, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		isHidden := strings.HasPrefix(entry.Name(), ".") && filePath != base
		if isHidden {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		fileInfo, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		isSnippetFile := fileInfo.Mode().IsRegular() && fileInfo.Size() <= snippetMax && strings.HasSuffix(filePath, ".md")
		if !isSnippetFile {
			return nil
		}
		rel, _ := filepath.Rel(base, filePath)
		rel = filepath.ToSlash(rel)
		description := ""
		if content, readErr := readTrusted(filePath); readErr == nil {
			description = describe(string(content))
		}
		command := "/" + strings.ReplaceAll(strings.TrimSuffix(rel, ".md"), "/", ":")
		snippets = append(snippets, Snippet{prefix + rel, scope, project, rel, command,
			description, fileInfo.Size() / 1024, float64(fileInfo.ModTime().UnixNano()) / 1e9})
		return nil
	})
	return snippets
}

// listSnippets : les commandes personnelles, puis celles de chaque projet qui en a ; scopes : les projets possibles.
func listSnippets() (snippets []Snippet, scopes []MemTarget) {
	snippets = walkCommands(userCmdDir(), "", "", []Snippet{})
	scopes = []MemTarget{}
	dirs, _ := readDirTrusted(root)
	for _, dirEntry := range dirs {
		projectDir := projectCwd(dirEntry.Name())
		dirInfo, err := os.Stat(projectDir)
		isProjectDir := dirEntry.IsDir() && projectDir != "" && err == nil && dirInfo.IsDir()
		if !isProjectDir {
			continue
		}
		base := filepath.Join(projectDir, ".claude", "commands")
		if sameDir(base, userCmdDir()) {
			continue // projet « ~ » : ses commandes sont les commandes personnelles
		}
		scopes = append(scopes, MemTarget{dirEntry.Name(), projectDir})
		snippets = walkCommands(base, dirEntry.Name(), projectDir, snippets)
	}
	sort.Slice(scopes, func(left, right int) bool {
		return scopes[left].Project < scopes[right].Project
	})
	return snippets, scopes
}

// snippetMergePrompt : la demande de fusion de snippets faite à Claude, et le dossier où la lancer (le projet d'une clé p/, sinon ~/.claude).
func snippetMergePrompt(keys []string, outputKey string) (string, string, error) {
	if len(keys) < 2 || len(keys) > mergeMax {
		return "", "", terr("srv.memory.choisisDeAFichiers", "A1", mergeMax)
	}
	if outputKey == "" {
		outputKey = keys[0]
	}
	if !slices.Contains(keys, outputKey) {
		return "", "", terr("srv.memory.documentDeSortieInvalide")
	}
	cwd, outPath := "", ""
	var prompt strings.Builder
	prompt.WriteString(L("prompt.snippetMergeHeader") + "\n")
	for _, key := range keys {
		base, rel, err := cmdBase(key)
		if err != nil {
			return "", "", err
		}
		snippetPath := filepath.Join(base, rel)
		if _, statErr := os.Stat(snippetPath); statErr != nil {
			return "", "", terr("srv.memory.introuvable", "A1", snippetPath)
		}
		prompt.WriteString("- " + snippetPath + "\n")
		if key == outputKey {
			outPath = snippetPath
		}
		transcriptDir, _, hasSeparator := strings.Cut(key[2:], "/")
		isFirstProjectKey := cwd == "" && key[0] == 'p' && hasSeparator
		if isFirstProjectKey {
			cwd = projectCwd(transcriptDir)
		}
	}
	prompt.WriteString("\n" + L("prompt.snippetMergeRules", "A1", outPath) + "\n")
	if cwd == "" {
		cwd = cfgDir()
	}
	return prompt.String(), cwd, nil
}

func readSnippet(key string) (string, error) {
	base, rel, err := cmdBase(key)
	if err != nil {
		return "", err
	}
	snippetRoot, err := os.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer snippetRoot.Close()
	content, err := snippetRoot.ReadFile(rel)
	isUnusable := len(content) > snippetMax || !utf8.Valid(content)
	if err == nil && isUnusable {
		err = terr("srv.memory.fichierTropGrosOu")
	}
	return string(content), err
}

// saveSnippet écrit un snippet (création refusée s'il existe déjà) ; la version remplacée va à la corbeille.
func saveSnippet(key, text string, create bool) error {
	if len(text) > snippetMax || !utf8.ValidString(text) {
		return terr("srv.memory.texteTropLongOu")
	}
	base, rel, err := cmdBase(key)
	if err != nil {
		return err
	}
	previous, err := readSnippet(key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err // trop gros ou pas en UTF-8 : le remplacer le perdrait sans passer par la corbeille
	}
	if err == nil {
		if create {
			return terr("srv.commands.ceSnippetExisteDeja", "A1", filepath.ToSlash(rel))
		}
		if previous != text {
			if trashErr := saveTrash("s/"+key, filepath.Join(base, rel), "remplacé", previous); trashErr != nil {
				return trashErr
			}
		}
	}
	if mkdirErr := mkdirTrusted(base, 0o755); mkdirErr != nil {
		return mkdirErr
	}
	snippetRoot, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer snippetRoot.Close()
	if subDir := filepath.Dir(rel); subDir != "." {
		if mkdirErr := snippetRoot.MkdirAll(subDir, 0o755); mkdirErr != nil {
			return mkdirErr
		}
	}
	tempName := rel + ".gld-web.tmp"
	if writeErr := snippetRoot.WriteFile(tempName, []byte(text), 0o644); writeErr != nil {
		return writeErr
	}
	if renameErr := snippetRoot.Rename(tempName, rel); renameErr != nil {
		snippetRoot.Remove(tempName)
		return renameErr
	}
	return nil
}

// removeSnippet met le snippet à la corbeille puis le supprime.
func removeSnippet(key string) error {
	base, rel, err := cmdBase(key)
	if err != nil {
		return err
	}
	text, err := readSnippet(key)
	if err != nil {
		return err
	}
	if trashErr := saveTrash("s/"+key, filepath.Join(base, rel), "supprimé", text); trashErr != nil {
		return trashErr
	}
	snippetRoot, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer snippetRoot.Close()
	return snippetRoot.Remove(rel)
}

// renameSnippet renomme un snippet dans son emplacement (personnel ou projet) ; refusé si la cible existe.
func renameSnippet(fromKey, toKey string) error {
	sourceBase, sourceRel, err := cmdBase(fromKey)
	if err != nil {
		return err
	}
	targetBase, targetRel, err := cmdBase(toKey)
	if err != nil {
		return err
	}
	if !sameDir(sourceBase, targetBase) {
		return terr("srv.commands.snippetInvalide")
	}
	snippetRoot, err := os.OpenRoot(sourceBase)
	if err != nil {
		return err
	}
	defer snippetRoot.Close()
	if _, sourceErr := snippetRoot.Lstat(sourceRel); sourceErr != nil {
		return sourceErr
	}
	if _, targetErr := snippetRoot.Lstat(targetRel); targetErr == nil {
		return terr("srv.commands.ceSnippetExisteDeja", "A1", filepath.ToSlash(targetRel))
	}
	if targetDir := filepath.Dir(targetRel); targetDir != "." {
		if mkdirErr := snippetRoot.MkdirAll(targetDir, 0o755); mkdirErr != nil {
			return mkdirErr
		}
	}
	return snippetRoot.Rename(sourceRel, targetRel)
}

// exportSnippets : les snippets d'un scope ("" : personnels) dans une archive zip, chemins relatifs conservés.
func exportSnippets(scope string) ([]byte, int, error) {
	snippets, _ := listSnippets()
	var archiveBuffer bytes.Buffer
	zipWriter := zip.NewWriter(&archiveBuffer)
	count := 0
	for _, listedSnippet := range snippets {
		if listedSnippet.Scope != scope {
			continue
		}
		text, err := readSnippet(listedSnippet.Key)
		if err != nil {
			return nil, 0, err
		}
		entryWriter, err := zipWriter.Create(listedSnippet.Path)
		if err != nil {
			return nil, 0, err
		}
		entryWriter.Write([]byte(text))
		count++
	}
	if err := zipWriter.Close(); err != nil { // écrit l'index de l'archive : avant de lire le tampon
		return nil, 0, err
	}
	return archiveBuffer.Bytes(), count, nil
}

// ImportFile : un fichier reçu (un .md, ou une archive .zip de .md).
type ImportFile struct {
	Name string
	Data []byte
}

// importSnippets range les fichiers dans un scope ; sans overwrite, un snippet existant est laissé tel quel.
func importSnippets(scope string, files []ImportFile, overwrite bool) (done, skipped []string, err error) {
	prefix := "u/"
	if scope != "" {
		prefix = "p/" + scope + "/"
	}
	saveImported := func(name string, data []byte) error {
		name = strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(name)), "/")
		saveErr := saveSnippet(prefix+name, string(data), !overwrite)
		if saveErr != nil && isMsg(saveErr, "srv.commands.ceSnippetExisteDeja") {
			skipped = append(skipped, name)
			return nil
		}
		if saveErr == nil {
			done = append(done, name)
		}
		return saveErr
	}
	for _, file := range files {
		isArchive := strings.HasSuffix(strings.ToLower(file.Name), ".zip")
		if isArchive {
			zipReader, openErr := zip.NewReader(bytes.NewReader(file.Data), int64(len(file.Data)))
			if openErr != nil {
				return done, skipped, terr("srv.commands.archiveIllisible", "A1", file.Name)
			}
			for _, zipEntry := range zipReader.File {
				isSnippetEntry := !zipEntry.FileInfo().IsDir() && strings.HasSuffix(zipEntry.Name, ".md") && zipEntry.UncompressedSize64 <= snippetMax
				if !isSnippetEntry {
					continue
				}
				entryReader, entryErr := zipEntry.Open()
				if entryErr != nil {
					return done, skipped, entryErr
				}
				content, entryErr := io.ReadAll(io.LimitReader(entryReader, snippetMax+1))
				entryReader.Close()
				if entryErr == nil {
					entryErr = saveImported(zipEntry.Name, content)
				}
				if entryErr != nil {
					return done, skipped, errors.New(zipEntry.Name + " : " + entryErr.Error())
				}
			}
			continue
		}
		if importErr := saveImported(filepath.Base(file.Name), file.Data); importErr != nil {
			return done, skipped, errors.New(file.Name + " : " + importErr.Error())
		}
	}
	return done, skipped, nil
}
