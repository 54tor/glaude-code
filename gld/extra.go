// Recherche plein texte, renommage et groupes virtuels de sessions.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	snippetContextBefore = 60
	snippetContextAfter  = 100
	maxStoreNameRunes    = 80
	maxStoredDirs        = 200
)

type Hit struct {
	Dir  string `json:"dir"`
	ID   string `json:"id"`
	N    int    `json:"n"`    // nombre d'occurrences dans les messages
	Snip string `json:"snip"` // extrait autour de la première
}

// snippet : la première occurrence de query (en minuscules) dans text, avec un peu de contexte. -1 si absente.
func snippet(text, query string) (int, string) {
	runes := []rune(text)
	lowered := make([]rune, len(runes)) // unicode.ToLower rune à rune : mêmes positions que text
	for runeIndex, character := range runes {
		lowered[runeIndex] = unicode.ToLower(character)
	}
	loweredText := string(lowered)
	byteIndex := strings.Index(loweredText, query)
	if byteIndex < 0 {
		return 0, ""
	}
	matchRune := utf8.RuneCountInString(loweredText[:byteIndex])
	windowStart := max(0, matchRune-snippetContextBefore)
	windowEnd := min(len(runes), matchRune+utf8.RuneCountInString(query)+snippetContextAfter)
	excerpt := strings.TrimSpace(spaceRe.ReplaceAllString(string(runes[windowStart:windowEnd]), " "))
	if windowStart > 0 {
		excerpt = "…" + excerpt
	}
	if windowEnd < len(runes) {
		excerpt += "…"
	}
	return strings.Count(loweredText, query), excerpt
}

// Cache du texte des messages par transcript, valable tant que sa date et sa taille ne changent pas :
// seule la première recherche (et un transcript modifié) relit le JSON.
type texts struct {
	mod  time.Time
	size int64
	t    []string
}

var textCache sync.Map // chemin -> texts

// textCacheMax : octets de texte gardés au total. Sans borne, la recherche de repli (index FTS inutilisable) retenait le texte
// de tous les transcripts à la fois ; au-delà, le cache est vidé et ne se reconstruit que par les transcripts réellement cherchés.
const textCacheMax = 256 << 20

var textBytes atomic.Int64

func textsSize(messages []string) (total int64) {
	for _, message := range messages {
		total += int64(len(message))
	}
	return total
}

func messageTexts(path string) []string {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if cachedValue, found := textCache.Load(path); found {
		cached := cachedValue.(texts)
		isCurrent := cached.mod.Equal(fileInfo.ModTime()) && cached.size == fileInfo.Size()
		if isCurrent {
			return cached.t
		}
	}
	var collected []string
	_ = scan(path, func(rawLine []byte) {
		for _, message := range lineMsgs(rawLine, false) {
			collected = append(collected, message.T)
		}
	})
	addedBytes := textsSize(collected)
	if textBytes.Add(addedBytes) > textCacheMax {
		textCache.Clear()
		textBytes.Store(addedBytes)
	}
	textCache.Store(path, texts{fileInfo.ModTime(), fileInfo.Size(), collected})
	return collected
}

// searchFile cherche q (en minuscules) dans les messages échangés (pas dans les sorties d'outils).
func searchFile(path, query string) (count int, snip string) {
	for _, text := range messageTexts(path) {
		if !strings.Contains(strings.ToLower(text), query) {
			continue
		}
		occurrences, excerpt := snippet(text, query)
		count += occurrences
		if snip == "" {
			snip = excerpt
		}
	}
	return
}

func search(query string) []Hit {
	query = strings.ToLower(strings.TrimSpace(query))
	files, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	known := map[string]bool{}
	for _, transcriptPath := range files {
		known[transcriptPath] = true
	}
	textCache.Range(func(cacheKey, _ any) bool { // transcripts supprimés ou déplacés
		if !known[cacheKey.(string)] {
			textCache.Delete(cacheKey)
		}
		return true
	})
	hits := make([]Hit, len(files))
	var waitGroup sync.WaitGroup
	slots := make(chan struct{}, runtime.NumCPU())
	for fileIndex, transcriptPath := range files {
		waitGroup.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() {
				<-slots
				waitGroup.Done()
			}()
			count, excerpt := searchFile(transcriptPath, query)
			if count > 0 {
				sessionDir := filepath.Base(filepath.Dir(transcriptPath))
				sessionID := strings.TrimSuffix(filepath.Base(transcriptPath), ".jsonl")
				hits[fileIndex] = Hit{sessionDir, sessionID, count, excerpt}
			}
		}()
	}
	waitGroup.Wait()
	matches := []Hit{}
	for _, hit := range hits {
		if hit.N > 0 {
			matches = append(matches, hit)
		}
	}
	return matches
}

// hasTitle dit si le transcript porte déjà un titre posé (custom-title).
func hasTitle(path string) bool {
	configRoot, rel, err := confine(cfgDir(), path)
	if err != nil {
		return false
	}
	defer configRoot.Close()
	transcriptFile, err := configRoot.Open(rel)
	if err != nil {
		return false
	}
	defer transcriptFile.Close()
	content, _ := io.ReadAll(transcriptFile)
	return bytes.Contains(content, []byte(`"custom-title"`))
}

// setTitle ajoute une ligne custom-title au transcript (la dernière fait foi), sans changer sa date.
func setTitle(path, id, title string) error {
	configRoot, rel, err := confine(cfgDir(), path)
	if err != nil {
		return err
	}
	defer configRoot.Close()
	transcriptFile, err := configRoot.OpenFile(rel, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	fileInfo, err := transcriptFile.Stat()
	if err != nil {
		transcriptFile.Close()
		return err
	}
	line, _ := json.Marshal(struct {
		Type        string `json:"type"`
		CustomTitle string `json:"customTitle"`
		SessionID   string `json:"sessionId"`
	}{"custom-title", title, id})
	line = append(line, '\n')
	last := make([]byte, 1)
	if fileInfo.Size() > 0 { // fichier sans saut de ligne final : on n'accole pas deux objets JSON
		if _, readErr := transcriptFile.ReadAt(last, fileInfo.Size()-1); readErr == nil && last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	_, err = transcriptFile.WriteAt(line, fileInfo.Size())
	if closeErr := transcriptFile.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = configRoot.Chtimes(rel, fileInfo.ModTime(), fileInfo.ModTime())
	}
	return err
}

// --- groupes virtuels et favoris ---
// Un groupe est un sous-dossier virtuel d'un dossier de travail : il ne rassemble que des sessions de ce projet.
// Les sessions sont désignées par leur id seul : il ne change pas quand on les déplace de dossier.

type VGroup struct {
	Name    string   `json:"name"`
	Project string   `json:"project"` // dossier de travail (clé de groupe de sessions())
	IDs     []string `json:"ids"`
	Fav     bool     `json:"fav,omitempty"`
}

// Project : un ensemble nommé de dossiers de travail (chemins absolus, clés de groupe de sessions()).
type Project struct {
	Name string   `json:"name"`
	Dirs []string `json:"dirs"`
}

// Store : contenu de ~/.claude/gld-web-groups.json.
type Store struct {
	Groups   []VGroup  `json:"groups"`
	Favs     []string  `json:"favs"` // sessions favorites
	Projects []Project `json:"projects"`
	// Dirs : dossiers de travail ajoutés à la main, qui n'ont pas (encore) de session : on y lance un prompt sans mémoire Claude.
	Dirs []string `json:"dirs"`
}

var groupsMu sync.Mutex

func groupsFile() string { return filepath.Join(cfgDir(), "gld-web-groups.json") }

func loadStore() Store {
	store := Store{Groups: []VGroup{}, Favs: []string{}, Projects: []Project{}, Dirs: []string{}}
	content, err := readTrusted(groupsFile())
	if err != nil {
		return store
	}
	if json.Unmarshal(content, &store) == nil {
		if store.Groups == nil {
			store.Groups = []VGroup{}
		}
		if store.Favs == nil {
			store.Favs = []string{}
		}
		if store.Projects == nil {
			store.Projects = []Project{}
		}
		if store.Dirs == nil {
			store.Dirs = []string{}
		}
		return store
	}
	// Ancien format : une liste de groupes sans projet. Chaque groupe est réparti par dossier de travail.
	var legacyGroups []VGroup
	if json.Unmarshal(content, &legacyGroups) != nil {
		return store
	}
	projectByID := map[string]string{}
	for _, group := range sessions() {
		for _, session := range group.Sessions {
			projectByID[session.ID] = group.Project
		}
	}
	for _, legacyGroup := range legacyGroups {
		idsByProject := map[string][]string{}
		var order []string
		for _, id := range legacyGroup.IDs {
			projectDir := projectByID[id]
			if projectDir == "" {
				continue // session disparue
			}
			if idsByProject[projectDir] == nil {
				order = append(order, projectDir)
			}
			idsByProject[projectDir] = append(idsByProject[projectDir], id)
		}
		for _, projectDir := range order {
			store.Groups = append(store.Groups, VGroup{Name: legacyGroup.Name, Project: projectDir, IDs: idsByProject[projectDir]})
		}
	}
	saveStore(store)
	return store
}

// saveStore valide puis remplace le fichier en entier (écriture atomique).
func saveStore(store Store) error {
	seen := map[[2]string]bool{}
	for groupIndex := range store.Groups {
		group := &store.Groups[groupIndex]
		group.Name = strings.TrimSpace(spaceRe.ReplaceAllString(group.Name, " "))
		groupKey := [2]string{group.Project, group.Name}
		hasValidName := group.Name != "" && utf8.RuneCountInString(group.Name) <= maxStoreNameRunes
		if !hasValidName || group.Project == "" || seen[groupKey] {
			return terr("srv.extra.groupeSansNomNi")
		}
		seen[groupKey] = true
		for _, id := range group.IDs {
			if !idRe.MatchString(id) {
				return terr("srv.chat.sessionInvalide")
			}
		}
	}
	seenProjects := map[string]bool{}
	for projectIndex := range store.Projects {
		storedProject := &store.Projects[projectIndex]
		storedProject.Name = strings.TrimSpace(spaceRe.ReplaceAllString(storedProject.Name, " "))
		hasValidName := storedProject.Name != "" && utf8.RuneCountInString(storedProject.Name) <= maxStoreNameRunes
		if !hasValidName || seenProjects[storedProject.Name] || len(storedProject.Dirs) > maxStoredDirs {
			return terr("srv.extra.projetSansNomNom")
		}
		seenProjects[storedProject.Name] = true
		seenDirs := map[string]bool{}
		for dirIndex, dir := range storedProject.Dirs {
			dir = filepath.Clean(dir)
			if !filepath.IsAbs(dir) || seenDirs[dir] {
				return terr("srv.extra.dossierDeProjetInvalide")
			}
			seenDirs[dir] = true
			storedProject.Dirs[dirIndex] = dir
		}
	}
	seenSavedDirs := map[string]bool{}
	if len(store.Dirs) > maxStoredDirs {
		return terr("srv.extra.tropDeDossiersEnregistres")
	}
	for dirIndex, dir := range store.Dirs {
		dir = filepath.Clean(dir)
		if !filepath.IsAbs(dir) || strings.ContainsRune(dir, 0) || seenSavedDirs[dir] {
			return terr("srv.extra.dossierEnregistreInvalideOu")
		}
		seenSavedDirs[dir] = true
		store.Dirs[dirIndex] = dir
	}
	for _, id := range store.Favs {
		if !idRe.MatchString(id) {
			return terr("srv.extra.favoriInvalide")
		}
	}
	content, _ := json.MarshalIndent(store, "", "  ")
	return rWriteAtomic(cfgDir(), groupsFile(), content, 0o600)
}
