package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const (
	rosterMaxEntries = 64
	rosterMaxBytes   = 64 << 10
)

var (
	rosterKeyRe = firstNameRe
	rosterValRe = regexp.MustCompile("^[a-z0-9-]+$")
)

func rosterPaths(cfgRoot, team string) (dir, file string, err error) {
	if !teamNameRe.MatchString(team) {
		return "", "", terr("srv.teams_roster.nomInvalide")
	}
	dir = filepath.Join(cfgRoot, journalDir)
	return dir, filepath.Join(dir, team+".roster.json"), nil
}

func rosterValid(roster map[string]string) bool {
	if len(roster) > rosterMaxEntries {
		return false
	}
	for firstName, roleSlug := range roster {
		if !rosterKeyRe.MatchString(firstName) || !rosterValRe.MatchString(roleSlug) {
			return false
		}
	}
	return true
}

// rosterWrite écrit prénom -> slug de rôle, de façon atomique.
func rosterWrite(cfgRoot, team string, roster map[string]string) error {
	dir, file, err := rosterPaths(cfgRoot, team)
	if err != nil {
		return err
	}
	if len(roster) > rosterMaxEntries {
		return terr("srv.teams_roster.trop")
	}
	if !rosterValid(roster) {
		return terr("srv.teams_roster.entreeInvalide")
	}
	content, err := json.Marshal(roster)
	if err != nil {
		return err
	}
	teamsJournalMu.Lock()
	defer teamsJournalMu.Unlock()
	dirInfo, exists, err := lstatNoLink(dir)
	if err != nil {
		return err
	}
	if !exists {
		if mkdirErr := rMkdirAll(cfgRoot, dir, 0o700); mkdirErr != nil {
			return mkdirErr
		}
	} else if !dirInfo.IsDir() {
		return terr("srv.teams_journal.lienRefuse")
	}
	if _, _, fileErr := lstatNoLink(file); fileErr != nil {
		return fileErr
	}
	return rWriteAtomic(cfgRoot, file, content, 0o600)
}

// rosterRead ne renvoie jamais d'erreur sur le contenu : la vue doit rester utilisable.
func rosterRead(cfgRoot, team string) (map[string]string, error) {
	dir, file, err := rosterPaths(cfgRoot, team)
	if err != nil {
		return nil, err
	}
	empty := map[string]string{}
	teamsJournalMu.Lock()
	defer teamsJournalMu.Unlock()
	if _, _, dirErr := lstatNoLink(dir); dirErr != nil {
		return empty, nil
	}
	if _, exists, fileErr := lstatNoLink(file); fileErr != nil || !exists {
		return empty, nil
	}
	rosterFile, err := os.OpenFile(file, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return empty, nil
	}
	defer rosterFile.Close()
	content, err := io.ReadAll(io.LimitReader(rosterFile, rosterMaxBytes+1))
	if err != nil || len(content) > rosterMaxBytes {
		return empty, nil
	}
	var roster map[string]string
	if json.Unmarshal(content, &roster) != nil || !rosterValid(roster) {
		return empty, nil
	}
	if roster == nil {
		roster = empty
	}
	return roster, nil
}

func rosterDrop(cfgRoot, team string) error {
	dir, file, err := rosterPaths(cfgRoot, team)
	if err != nil {
		return err
	}
	teamsJournalMu.Lock()
	defer teamsJournalMu.Unlock()
	if _, _, dirErr := lstatNoLink(dir); dirErr != nil {
		return dirErr
	}
	if removeErr := rRemove(cfgRoot, file); removeErr != nil && !os.IsNotExist(removeErr) {
		return removeErr
	}
	_ = rRemove(cfgRoot, dir) // ne réussit que si le dossier est vide
	return nil
}
