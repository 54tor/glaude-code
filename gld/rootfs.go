package main

import (
	"fmt"
	"os"
	"path/filepath"
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
