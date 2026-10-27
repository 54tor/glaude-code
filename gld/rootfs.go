package main

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Écritures confinées : sous GLD_RUN_AS, la passerelle (root) écrit dans des dossiers que le compte cible peut modifier. Ces
// fonctions passent par os.Root ouvert sur un dossier de confiance (cfgDir() le plus souvent) : un lien symbolique posé par la
// cible, à n'importe quel niveau du chemin, ne fait jamais sortir l'opération de ce dossier.

// relIn : path relatif à base ; path hors de base (ou base lui-même) est refusé.
func relIn(base, path string) (string, error) {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s : hors de %s", path, base)
	}
	return rel, nil
}

// confine ouvre base et rend le chemin de path relatif à base.
func confine(base, path string) (*os.Root, string, error) {
	rel, err := relIn(base, path)
	if err != nil {
		return nil, "", err
	}
	r, err := os.OpenRoot(base)
	return r, rel, err
}

func rOpenFile(base, path string, flag int, perm os.FileMode) (*os.File, error) {
	r, rel, err := confine(base, path)
	if err != nil {
		return nil, err
	}
	defer r.Close() // le fichier ouvert reste utilisable après la fermeture de la racine
	return r.OpenFile(rel, flag, perm)
}

func rWriteFile(base, path string, b []byte, perm os.FileMode) error {
	r, rel, err := confine(base, path)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.WriteFile(rel, b, perm)
}

func rMkdirAll(base, path string, perm os.FileMode) error {
	if filepath.Clean(path) == filepath.Clean(base) {
		return nil
	}
	r, rel, err := confine(base, path)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.MkdirAll(rel, perm)
}

func rRemove(base, path string) error {
	r, rel, err := confine(base, path)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Remove(rel)
}

func rRemoveAll(base, path string) error {
	r, rel, err := confine(base, path)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RemoveAll(rel)
}

func rRename(base, from, to string) error {
	r, a, err := confine(base, from)
	if err != nil {
		return err
	}
	defer r.Close()
	b, err := relIn(base, to)
	if err != nil {
		return err
	}
	return r.Rename(a, b)
}

// rWriteAtomic remplace path par renommage d'un temporaire créé en exclusivité à côté de lui, fsync et mode posés sur le
// descripteur (jamais un chmod par nom qu'un lien pourrait détourner).
func rWriteAtomic(base, path string, b []byte, mode os.FileMode) error {
	r, rel, err := confine(base, path)
	if err != nil {
		return err
	}
	defer r.Close()
	var f *os.File
	var tmp string
	for {
		tmp = filepath.Join(filepath.Dir(rel), ".gld-web-"+strconv.FormatUint(rand.Uint64(), 36)+".tmp")
		if f, err = r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); !os.IsExist(err) {
			break
		}
	}
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	if err := errors.Join(werr, f.Chmod(mode), f.Sync(), f.Close()); err != nil {
		r.Remove(tmp)
		return err
	}
	if err := r.Rename(tmp, rel); err != nil {
		r.Remove(tmp)
		return err
	}
	return nil
}

// mkdirTrusted : sous cfgDir(), création confinée ; ailleurs (dossier d'un projet, qui appartient déjà au compte), MkdirAll.
func mkdirTrusted(path string, perm os.FileMode) error {
	if _, err := relIn(cfgDir(), path); err == nil {
		return rMkdirAll(cfgDir(), path, perm)
	}
	return os.MkdirAll(path, perm)
}

// walkRoot : la racine d'un parcours, lien résolu (un ~/.claude/commands lié à un dépôt de dotfiles), car WalkDir ne descend
// pas dans une racine qui est un lien. Sous GLD_RUN_AS, jamais : ce lien peut avoir été posé par le compte cible.
func walkRoot(p string) string {
	if !runAsOn() {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
	}
	return p
}

// Lectures : sous GLD_RUN_AS seulement, un lien posé par le compte cible ne doit pas faire lire un fichier de root (affiché ensuite
// dans l'interface). Sous cfgDir(), tout le chemin est confiné ; ailleurs (dossier d'un projet), le dossier du fichier sert de
// racine. Hors GLD_RUN_AS, lecture ordinaire : un CLAUDE.md ou des commandes liés à un dépôt de dotfiles restent lisibles.
func trustBase(path string) string {
	if _, err := relIn(cfgDir(), path); err == nil {
		return cfgDir()
	}
	return filepath.Dir(path)
}

func readTrusted(path string) ([]byte, error) {
	if !runAsOn() {
		return os.ReadFile(path)
	}
	r, rel, err := confine(trustBase(path), path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.ReadFile(rel)
}

func openTrusted(path string) (*os.File, error) {
	if !runAsOn() {
		return os.Open(path)
	}
	return rOpenFile(trustBase(path), path, os.O_RDONLY, 0)
}

func readDirTrusted(path string) ([]os.DirEntry, error) {
	if !runAsOn() {
		return os.ReadDir(path)
	}
	base, rel := trustBase(path), "."
	if filepath.Clean(path) != filepath.Clean(base) {
		var err error
		if rel, err = relIn(base, path); err != nil {
			return nil, err
		}
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}
