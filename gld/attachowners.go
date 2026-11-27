package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
)

const ownersFile = "owners.json"

var errOwnersCorrupt = terr("srv.attachowners.indexIllisible")

func ownersPath() string { return filepath.Join(imagesDir(), ownersFile) }

// loadOwners : nom de fichier déposé → id de la session qui le cite. Un index illisible n'est jamais écrasé par l'appelant.
func loadOwners() (map[string]string, error) {
	content, err := readTrusted(ownersPath())
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	owners := map[string]string{}
	if json.Unmarshal(content, &owners) != nil {
		return nil, errOwnersCorrupt
	}
	return owners, nil
}

func saveOwners(owners map[string]string) error {
	content, err := json.Marshal(owners)
	if err != nil {
		return err
	}
	if err := rMkdirAll(cfgDir(), imagesDir(), 0o700); err != nil {
		return err
	}
	return rWriteAtomic(cfgDir(), ownersPath(), content, 0o600)
}
