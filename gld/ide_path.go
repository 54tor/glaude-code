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

// ideKnownDir n'accepte qu'un dossier déjà connu de gld, jamais un chemin arbitraire du disque.
func ideKnownDir(dir string) (string, bool) {
	if dir == "" || strings.ContainsRune(dir, 0) || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
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
	code, e := http.StatusInternalServerError, errIdeNotFound
	switch {
	case errors.Is(err, errIdeDir):
		code, e = 404, errIdeDir
	case errors.Is(err, errIdePath):
		code, e = 400, errIdePath
	case errors.Is(err, errIdeSymlink), strings.Contains(err.Error(), "path escapes from parent"):
		code, e = 403, errIdeSymlink
	case errors.Is(err, errIdeNotFound):
		code, e = 404, errIdeNotFound
	case errors.Is(err, errIdeTooBig):
		code, e = 413, errIdeTooBig
	case errors.Is(err, errIdeBinary):
		code, e = 415, errIdeBinary
	case errors.Is(err, errIdeConflict):
		code, e = 409, errIdeConflict
	case errors.Is(err, errIdeExists):
		code, e = 409, errIdeExists
	}
	out := e.Error()
	if code == 500 {
		out = L("srv.ide.erreur")
	}
	writeJSON(w, code, map[string]any{"ok": false, "out": out})
}
