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

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	x, err1 := os.Stat(a)
	y, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(x, y)
}

// snippetRel valide un chemin : relatif, sans « .. », sans dossier ni fichier caché, et en .md (une commande).
func snippetRel(rel string) (string, error) {
	rel = filepath.FromSlash(strings.TrimSpace(rel))
	if !filepath.IsLocal(rel) || !strings.HasSuffix(rel, ".md") {
		return "", terr("srv.commands.cheminDeSnippetInvalide")
	}
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		if strings.HasPrefix(seg, ".") {
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
		d, r, ok := strings.Cut(key[2:], "/")
		if !ok || !safeDir(d) {
			return "", "", terr("srv.memmeta.projetInvalide")
		}
		c := projectCwd(d)
		if fi, err := os.Stat(c); c == "" || err != nil || !fi.IsDir() {
			return "", "", terr("srv.memory.dossierDuProjetIntrouvable")
		}
		base, rel = filepath.Join(c, ".claude", "commands"), r
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

func walkCommands(base, scope, project string, out []Snippet) []Snippet {
	prefix := "u/"
	if scope != "" {
		prefix = "p/" + scope + "/"
	}
	base = walkRoot(base)
	filepath.WalkDir(base, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasPrefix(e.Name(), ".") && p != base {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > snippetMax || !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		rel = filepath.ToSlash(rel)
		desc := ""
		if b, err := os.ReadFile(p); err == nil {
			desc = describe(string(b))
		}
		out = append(out, Snippet{prefix + rel, scope, project, rel, "/" + strings.ReplaceAll(strings.TrimSuffix(rel, ".md"), "/", ":"),
			desc, fi.Size() / 1024, float64(fi.ModTime().UnixNano()) / 1e9})
		return nil
	})
	return out
}

// listSnippets : les commandes personnelles, puis celles de chaque projet qui en a ; scopes : les projets possibles.
func listSnippets() (snips []Snippet, scopes []MemTarget) {
	snips = walkCommands(userCmdDir(), "", "", []Snippet{})
	scopes = []MemTarget{}
	dirs, _ := os.ReadDir(root)
	for _, d := range dirs {
		c := projectCwd(d.Name())
		if fi, err := os.Stat(c); !d.IsDir() || c == "" || err != nil || !fi.IsDir() {
			continue
		}
		base := filepath.Join(c, ".claude", "commands")
		if sameDir(base, userCmdDir()) {
			continue // projet « ~ » : ses commandes sont les commandes personnelles
		}
		scopes = append(scopes, MemTarget{d.Name(), c})
		snips = walkCommands(base, d.Name(), c, snips)
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].Project < scopes[j].Project })
	return snips, scopes
}

func readSnippet(key string) (string, error) {
	base, rel, err := cmdBase(key)
	if err != nil {
		return "", err
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer r.Close()
	b, err := r.ReadFile(rel)
	if err == nil && (len(b) > snippetMax || !utf8.Valid(b)) {
		err = terr("srv.memory.fichierTropGrosOu")
	}
	return string(b), err
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
	if old, err := readSnippet(key); err == nil {
		if create {
			return terr("srv.commands.ceSnippetExisteDeja", "A1", filepath.ToSlash(rel))
		}
		if old != text {
			if err := saveTrash("s/"+key, filepath.Join(base, rel), "remplacé", old); err != nil {
				return err
			}
		}
	}
	if err := mkdirTrusted(base, 0o755); err != nil {
		return err
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer r.Close()
	if d := filepath.Dir(rel); d != "." {
		if err := r.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	tmp := rel + ".gld-web.tmp"
	if err := r.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if err := r.Rename(tmp, rel); err != nil {
		r.Remove(tmp)
		return err
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
	if err := saveTrash("s/"+key, filepath.Join(base, rel), "supprimé", text); err != nil {
		return err
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Remove(rel)
}

// exportSnippets : les snippets d'un scope ("" : personnels) dans une archive zip, chemins relatifs conservés.
func exportSnippets(scope string) ([]byte, int, error) {
	all, _ := listSnippets()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	n := 0
	for _, s := range all {
		if s.Scope != scope {
			continue
		}
		t, err := readSnippet(s.Key)
		if err != nil {
			return nil, 0, err
		}
		w, err := z.Create(s.Path)
		if err != nil {
			return nil, 0, err
		}
		w.Write([]byte(t))
		n++
	}
	if err := z.Close(); err != nil { // écrit l'index de l'archive : avant de lire le tampon
		return nil, 0, err
	}
	return buf.Bytes(), n, nil
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
	put := func(name string, data []byte) error {
		name = strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(name)), "/")
		e := saveSnippet(prefix+name, string(data), !overwrite)
		if e != nil && isMsg(e, "srv.commands.ceSnippetExisteDeja") {
			skipped = append(skipped, name)
			return nil
		}
		if e == nil {
			done = append(done, name)
		}
		return e
	}
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f.Name), ".zip") {
			z, e := zip.NewReader(bytes.NewReader(f.Data), int64(len(f.Data)))
			if e != nil {
				return done, skipped, terr("srv.commands.archiveIllisible", "A1", f.Name)
			}
			for _, zf := range z.File {
				if zf.FileInfo().IsDir() || !strings.HasSuffix(zf.Name, ".md") || zf.UncompressedSize64 > snippetMax {
					continue
				}
				rc, e := zf.Open()
				if e != nil {
					return done, skipped, e
				}
				b, e := io.ReadAll(io.LimitReader(rc, snippetMax+1))
				rc.Close()
				if e == nil {
					e = put(zf.Name, b)
				}
				if e != nil {
					return done, skipped, errors.New(zf.Name + " : " + e.Error())
				}
			}
			continue
		}
		if e := put(filepath.Base(f.Name), f.Data); e != nil {
			return done, skipped, errors.New(f.Name + " : " + e.Error())
		}
	}
	return done, skipped, nil
}
