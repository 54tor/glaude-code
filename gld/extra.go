// Recherche plein texte, renommage et groupes virtuels de sessions.
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type Hit struct {
	Dir  string `json:"dir"`
	ID   string `json:"id"`
	N    int    `json:"n"`    // nombre d'occurrences dans les messages
	Snip string `json:"snip"` // extrait autour de la première
}

// snippet : la première occurrence de q (en minuscules) dans t, avec un peu de contexte. -1 si absente.
func snippet(t, q string) (int, string) {
	rs := []rune(t)
	low := make([]rune, len(rs)) // unicode.ToLower rune à rune : mêmes positions que t
	for i, r := range rs {
		low[i] = unicode.ToLower(r)
	}
	ls := string(low)
	i := strings.Index(ls, q)
	if i < 0 {
		return 0, ""
	}
	at := utf8.RuneCountInString(ls[:i])
	a, b := max(0, at-60), min(len(rs), at+utf8.RuneCountInString(q)+100)
	s := strings.TrimSpace(spaceRe.ReplaceAllString(string(rs[a:b]), " "))
	if a > 0 {
		s = "…" + s
	}
	if b < len(rs) {
		s += "…"
	}
	return strings.Count(ls, q), s
}

// Cache du texte des messages par transcript, valable tant que sa date et sa taille ne changent pas :
// seule la première recherche (et un transcript modifié) relit le JSON.
type texts struct {
	mod  time.Time
	size int64
	t    []string
}

var textCache sync.Map // chemin -> texts

func messageTexts(path string) []string {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if c, ok := textCache.Load(path); ok {
		if c := c.(texts); c.mod.Equal(st.ModTime()) && c.size == st.Size() {
			return c.t
		}
	}
	var t []string
	_ = scan(path, func(b []byte) {
		for _, m := range lineMsgs(b, false) {
			t = append(t, m.T)
		}
	})
	textCache.Store(path, texts{st.ModTime(), st.Size(), t})
	return t
}

// searchFile cherche q (en minuscules) dans les messages échangés (pas dans les sorties d'outils).
func searchFile(path, q string) (n int, snip string) {
	for _, t := range messageTexts(path) {
		if !strings.Contains(strings.ToLower(t), q) {
			continue
		}
		c, s := snippet(t, q)
		n += c
		if snip == "" {
			snip = s
		}
	}
	return
}

func search(q string) []Hit {
	q = strings.ToLower(strings.TrimSpace(q))
	files, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	known := map[string]bool{}
	for _, p := range files {
		known[p] = true
	}
	textCache.Range(func(k, _ any) bool { // transcripts supprimés ou déplacés
		if !known[k.(string)] {
			textCache.Delete(k)
		}
		return true
	})
	hits := make([]Hit, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i, p := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			if n, s := searchFile(p, q); n > 0 {
				hits[i] = Hit{filepath.Base(filepath.Dir(p)), strings.TrimSuffix(filepath.Base(p), ".jsonl"), n, s}
			}
		}()
	}
	wg.Wait()
	out := []Hit{}
	for _, h := range hits {
		if h.N > 0 {
			out = append(out, h)
		}
	}
	return out
}

// setTitle ajoute une ligne custom-title au transcript (la dernière fait foi), sans changer sa date.
func setTitle(path, id, title string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	line, _ := json.Marshal(struct {
		Type        string `json:"type"`
		CustomTitle string `json:"customTitle"`
		SessionID   string `json:"sessionId"`
	}{"custom-title", title, id})
	line = append(line, '\n')
	last := make([]byte, 1)
	if st.Size() > 0 { // fichier sans saut de ligne final : on n'accole pas deux objets JSON
		if _, err := f.ReadAt(last, st.Size()-1); err == nil && last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	_, err = f.WriteAt(line, st.Size())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chtimes(path, st.ModTime(), st.ModTime())
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
	Groups []VGroup `json:"groups"`
	Favs   []string `json:"favs"` // sessions favorites
	// Tags : étiquettes libres par session (id → étiquettes).
	Tags     map[string][]string `json:"tags"`
	Projects []Project           `json:"projects"`
	// Dirs : dossiers de travail ajoutés à la main, qui n'ont pas (encore) de session : on y lance un prompt sans mémoire Claude.
	Dirs []string `json:"dirs"`
}

const maxTags, maxTagLen = 12, 30

var groupsMu sync.Mutex

func groupsFile() string { return filepath.Join(cfgDir(), "gld-web-groups.json") }

func loadStore() Store {
	st := Store{Groups: []VGroup{}, Favs: []string{}, Tags: map[string][]string{}, Projects: []Project{}, Dirs: []string{}}
	b, err := os.ReadFile(groupsFile())
	if err != nil {
		return st
	}
	if json.Unmarshal(b, &st) == nil {
		if st.Groups == nil {
			st.Groups = []VGroup{}
		}
		if st.Favs == nil {
			st.Favs = []string{}
		}
		if st.Tags == nil {
			st.Tags = map[string][]string{}
		}
		if st.Projects == nil {
			st.Projects = []Project{}
		}
		if st.Dirs == nil {
			st.Dirs = []string{}
		}
		return st
	}
	// Ancien format : une liste de groupes sans projet. Chaque groupe est réparti par dossier de travail.
	var old []VGroup
	if json.Unmarshal(b, &old) != nil {
		return st
	}
	project := map[string]string{}
	for _, g := range sessions() {
		for _, s := range g.Sessions {
			project[s.ID] = g.Project
		}
	}
	for _, g := range old {
		by := map[string][]string{}
		var order []string
		for _, id := range g.IDs {
			p := project[id]
			if p == "" {
				continue // session disparue
			}
			if by[p] == nil {
				order = append(order, p)
			}
			by[p] = append(by[p], id)
		}
		for _, p := range order {
			st.Groups = append(st.Groups, VGroup{Name: g.Name, Project: p, IDs: by[p]})
		}
	}
	saveStore(st)
	return st
}

// saveStore valide puis remplace le fichier en entier (écriture atomique).
func saveStore(st Store) error {
	seen := map[[2]string]bool{}
	for i := range st.Groups {
		g := &st.Groups[i]
		g.Name = strings.TrimSpace(spaceRe.ReplaceAllString(g.Name, " "))
		k := [2]string{g.Project, g.Name}
		if g.Name == "" || utf8.RuneCountInString(g.Name) > 80 || g.Project == "" || seen[k] {
			return errors.New("groupe sans nom ni projet, nom trop long, ou en double dans ce projet")
		}
		seen[k] = true
		for _, id := range g.IDs {
			if !idRe.MatchString(id) {
				return errors.New("session invalide")
			}
		}
	}
	seenP := map[string]bool{}
	for i := range st.Projects {
		p := &st.Projects[i]
		p.Name = strings.TrimSpace(spaceRe.ReplaceAllString(p.Name, " "))
		if p.Name == "" || utf8.RuneCountInString(p.Name) > 80 || seenP[p.Name] || len(p.Dirs) > 200 {
			return errors.New("projet sans nom, nom trop long ou en double")
		}
		seenP[p.Name] = true
		seenD := map[string]bool{}
		for j, d := range p.Dirs {
			d = filepath.Clean(d)
			if !filepath.IsAbs(d) || seenD[d] {
				return errors.New("dossier de projet invalide ou en double")
			}
			seenD[d] = true
			p.Dirs[j] = d
		}
	}
	seenDir := map[string]bool{}
	if len(st.Dirs) > 200 {
		return errors.New("trop de dossiers enregistrés")
	}
	for i, d := range st.Dirs {
		d = filepath.Clean(d)
		if !filepath.IsAbs(d) || strings.ContainsRune(d, 0) || seenDir[d] {
			return errors.New("dossier enregistré invalide ou en double")
		}
		seenDir[d] = true
		st.Dirs[i] = d
	}
	for _, id := range st.Favs {
		if !idRe.MatchString(id) {
			return errors.New("favori invalide")
		}
	}
	for id, ts := range st.Tags {
		if !idRe.MatchString(id) || len(ts) > maxTags {
			return errors.New("étiquettes invalides")
		}
		seenTag := map[string]bool{}
		for i, t := range ts {
			t = strings.TrimSpace(spaceRe.ReplaceAllString(t, " "))
			if t == "" || utf8.RuneCountInString(t) > maxTagLen || seenTag[strings.ToLower(t)] {
				return errors.New("étiquette vide, trop longue ou en double")
			}
			seenTag[strings.ToLower(t)] = true
			ts[i] = t
		}
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	tmp := groupsFile() + ".tmp"
	if err := writeSynced(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, groupsFile())
}
