package main

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	errIdeDir      = terr("srv.ide.dossierInconnu")
	errIdePath     = terr("srv.ide.cheminInvalide")
	errIdeSymlink  = terr("srv.ide.lienSymbolique")
	errIdeNotFound = terr("srv.ide.introuvable")
	errIdeTooBig   = terr("srv.ide.tropGros")
	errIdeBinary   = terr("srv.ide.binaire")
	errIdeConflict = terr("srv.ide.conflit")
	errIdeExists   = terr("srv.ide.existeDeja")
)

// pathUnder : a est b ou un descendant de b, par segments (jamais par préfixe de chaîne).
func pathUnder(a, b string) bool {
	r, err := filepath.Rel(b, a)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// ideForbiddenRoot écarte comme racine le home (et ses ancêtres), la config Claude de chaque home et le dossier
// d'auth de gld : un compte fs_read y lirait auth.db, auth.key et les identifiants Claude d'un autre home.
func ideForbiddenRoot(dir string) bool {
	forms := func(p string) []string {
		p = filepath.Clean(p)
		if r, err := filepath.EvalSymlinks(p); err == nil && r != p {
			return []string{p, r}
		}
		return []string{p}
	}
	// Sans $HOME résolu, rien ne prouve qu'une racine n'en est pas un ancêtre : tout est refusé.
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return true
	}
	guarded := forms(h)
	for _, d := range forms(dir) {
		for _, h := range guarded {
			if pathUnder(h, d) {
				return true
			}
		}
	}
	cfgs := []string{cfgDir()}
	homes := readHomes()
	// Un enfant lit son propre fichier de homes : la config principale et ses homes viennent de la passerelle.
	if m := os.Getenv(ideMainCfgEnv); m != "" && filepath.IsAbs(m) {
		cfgs = append(cfgs, m)
		homes = append(homes, readHomesFile(filepath.Join(m, homesFileName))...)
	}
	for _, hm := range homes {
		if filepath.IsAbs(hm.Dir) {
			cfgs = append(cfgs, hm.Dir)
		}
	}
	var prot []string
	for _, c := range cfgs {
		prot = append(prot, forms(c)...)
	}
	if a, err := authDir(); err == nil && a != "" {
		prot = append(prot, forms(a)...)
	}
	for _, d := range forms(dir) {
		for _, p := range prot {
			if pathUnder(d, p) || pathUnder(p, d) {
				return true
			}
		}
	}
	return false
}

// ideKnownDir n'accepte qu'un dossier déjà connu de gld, jamais un chemin arbitraire du disque.
func ideKnownDir(dir string) (string, bool) {
	if dir == "" || strings.ContainsRune(dir, 0) || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return "", false
	}
	if ideForbiddenRoot(dir) {
		return "", false
	}
	if knownRepo(dir) {
		return dir, true
	}
	st := loadStore()
	for _, d := range st.Dirs {
		if d == dir {
			return dir, true
		}
	}
	for _, p := range st.Projects {
		for _, d := range p.Dirs {
			if d == dir {
				return dir, true
			}
		}
	}
	return "", false
}

func ideOpenRoot(dir string) (*os.Root, error) {
	d, ok := ideKnownDir(dir)
	if !ok {
		return nil, errIdeDir
	}
	r, err := os.OpenRoot(d)
	if err != nil {
		return nil, errIdeDir
	}
	return r, nil
}

func ideRel(p string, allowRoot bool) (string, error) {
	if p == "" || strings.ContainsAny(p, "\x00\\") || strings.HasPrefix(p, "/") {
		if p == "" && allowRoot {
			return ".", nil
		}
		return "", errIdePath
	}
	for _, s := range strings.Split(p, "/") {
		if s == ".." {
			return "", errIdePath
		}
	}
	c := path.Clean(p)
	if c == "." && !allowRoot {
		return "", errIdePath
	}
	return c, nil
}

// ideLstat refuse tout lien, même interne à la racine : os.Root ne bloque que les évasions.
func ideLstat(root *os.Root, rel string) (fs.FileInfo, error) {
	if rel == "." {
		return root.Lstat(".")
	}
	var fi fs.FileInfo
	parts := strings.Split(rel, "/")
	for i := range parts {
		var err error
		fi, err = root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, errIdeNotFound
			}
			return nil, err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return nil, errIdeSymlink
		}
	}
	return fi, nil
}

func ideFail(w http.ResponseWriter, err error) {
	code, slug, out := ideErr(err)
	writeErr(w, code, slug, out)
}

// ideErr : statut, code et texte d'une erreur de l'IDE.
func ideErr(err error) (int, string, string) {
	code, slug, e := http.StatusInternalServerError, "internal", errIdeNotFound
	switch {
	case errors.Is(err, errIdeDir):
		code, slug, e = 404, "not_found", errIdeDir
	case errors.Is(err, errIdePath):
		code, slug, e = 400, "bad_request", errIdePath
	case errors.Is(err, errIdeSymlink), strings.Contains(err.Error(), "path escapes from parent"):
		code, slug, e = 403, "forbidden", errIdeSymlink
	case errors.Is(err, errIdeNotFound):
		code, slug, e = 404, "not_found", errIdeNotFound
	case errors.Is(err, errIdeTooBig):
		code, slug, e = 413, "too_large", errIdeTooBig
	case errors.Is(err, errIdeBinary):
		code, slug, e = 415, "unsupported", errIdeBinary
	case errors.Is(err, errIdeConflict):
		code, slug, e = 409, "conflict", errIdeConflict
	case errors.Is(err, errIdeExists):
		code, slug, e = 409, "exists", errIdeExists
	}
	out := e.Error()
	if code == 500 {
		out = L("srv.ide.erreur")
	}
	return code, slug, out
}
