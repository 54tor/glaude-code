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

// pathUnder : candidate est ancestor ou un descendant d'ancestor, par segments (jamais par préfixe de chaîne).
func pathUnder(candidate, ancestor string) bool {
	relative, err := filepath.Rel(ancestor, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// ideForbiddenRoot écarte comme racine le home (et ses ancêtres), la config Claude de chaque home et le dossier
// d'auth de gld : un compte fs_read y lirait auth.db, auth.key et les identifiants Claude d'un autre home.
func ideForbiddenRoot(dir string) bool {
	forms := func(candidate string) []string {
		candidate = filepath.Clean(candidate)
		if resolved, evalErr := filepath.EvalSymlinks(candidate); evalErr == nil && resolved != candidate {
			return []string{candidate, resolved}
		}
		return []string{candidate}
	}
	// Sans $HOME résolu, rien ne prouve qu'une racine n'en est pas un ancêtre : tout est refusé.
	userHome, homeErr := os.UserHomeDir()
	if homeErr != nil || userHome == "" {
		return true
	}
	guarded := forms(userHome)
	for _, dirForm := range forms(dir) {
		for _, guardedForm := range guarded {
			if pathUnder(guardedForm, dirForm) {
				return true
			}
		}
	}
	cfgs := []string{cfgDir()}
	homes := readHomes()
	// Un enfant lit son propre fichier de homes : la config principale et ses homes viennent de la passerelle.
	if mainConfig := os.Getenv(ideMainCfgEnv); mainConfig != "" && filepath.IsAbs(mainConfig) {
		cfgs = append(cfgs, mainConfig)
		homes = append(homes, readHomesFile(filepath.Join(mainConfig, homesFileName))...)
	}
	for _, home := range homes {
		if filepath.IsAbs(home.Dir) {
			cfgs = append(cfgs, home.Dir)
		}
	}
	var protected []string
	for _, configRoot := range cfgs {
		protected = append(protected, forms(configRoot)...)
	}
	if authDirectory, authDirErr := authDir(); authDirErr == nil && authDirectory != "" {
		protected = append(protected, forms(authDirectory)...)
	}
	for _, dirForm := range forms(dir) {
		for _, protectedPath := range protected {
			if pathUnder(dirForm, protectedPath) || pathUnder(protectedPath, dirForm) {
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
	store := loadStore()
	for _, storedDir := range store.Dirs {
		if storedDir == dir {
			return dir, true
		}
	}
	for _, storedProject := range store.Projects {
		for _, storedDir := range storedProject.Dirs {
			if storedDir == dir {
				return dir, true
			}
		}
	}
	return "", false
}

func ideOpenRoot(dir string) (*os.Root, error) {
	knownDir, known := ideKnownDir(dir)
	if !known {
		return nil, errIdeDir
	}
	openedRoot, err := os.OpenRoot(knownDir)
	if err != nil {
		return nil, errIdeDir
	}
	return openedRoot, nil
}

func ideRel(requestedPath string, allowRoot bool) (string, error) {
	if requestedPath == "" || strings.ContainsAny(requestedPath, "\x00\\") || strings.HasPrefix(requestedPath, "/") {
		if requestedPath == "" && allowRoot {
			return ".", nil
		}
		return "", errIdePath
	}
	for _, segment := range strings.Split(requestedPath, "/") {
		if segment == ".." {
			return "", errIdePath
		}
	}
	cleaned := path.Clean(requestedPath)
	if cleaned == "." && !allowRoot {
		return "", errIdePath
	}
	return cleaned, nil
}

// ideLstat refuse tout lien, même interne à la racine : os.Root ne bloque que les évasions.
func ideLstat(root *os.Root, rel string) (fs.FileInfo, error) {
	if rel == "." {
		return root.Lstat(".")
	}
	var fileInfo fs.FileInfo
	parts := strings.Split(rel, "/")
	for partIndex := range parts {
		var err error
		fileInfo, err = root.Lstat(strings.Join(parts[:partIndex+1], "/"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, errIdeNotFound
			}
			return nil, err
		}
		if fileInfo.Mode()&fs.ModeSymlink != 0 {
			return nil, errIdeSymlink
		}
	}
	return fileInfo, nil
}

func ideFail(responseWriter http.ResponseWriter, err error) {
	code, slug, message := ideErr(err)
	writeErr(responseWriter, code, slug, message)
}

// ideErr : statut, code et texte d'une erreur de l'IDE.
func ideErr(err error) (int, string, string) {
	code, slug, catalogErr := http.StatusInternalServerError, "internal", errIdeNotFound
	switch {
	case errors.Is(err, errIdeDir):
		code, slug, catalogErr = 404, "not_found", errIdeDir
	case errors.Is(err, errIdePath):
		code, slug, catalogErr = 400, "bad_request", errIdePath
	case errors.Is(err, errIdeSymlink), strings.Contains(err.Error(), "path escapes from parent"):
		code, slug, catalogErr = 403, "forbidden", errIdeSymlink
	case errors.Is(err, errIdeNotFound):
		code, slug, catalogErr = 404, "not_found", errIdeNotFound
	case errors.Is(err, errIdeTooBig):
		code, slug, catalogErr = 413, "too_large", errIdeTooBig
	case errors.Is(err, errIdeBinary):
		code, slug, catalogErr = 415, "unsupported", errIdeBinary
	case errors.Is(err, errIdeConflict):
		code, slug, catalogErr = 409, "conflict", errIdeConflict
	case errors.Is(err, errIdeExists):
		code, slug, catalogErr = 409, "exists", errIdeExists
	}
	message := catalogErr.Error()
	if code == 500 {
		message = L("srv.ide.erreur")
	}
	return code, slug, message
}
