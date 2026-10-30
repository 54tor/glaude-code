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

func rosterValid(m map[string]string) bool {
	if len(m) > rosterMaxEntries {
		return false
	}
	for k, v := range m {
		if !rosterKeyRe.MatchString(k) || !rosterValRe.MatchString(v) {
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
	b, err := json.Marshal(roster)
	if err != nil {
		return err
	}
	teamsJournalMu.Lock()
	defer teamsJournalMu.Unlock()
	st, ok, err := lstatNoLink(dir)
	if err != nil {
		return err
	}
	if !ok {
		if err := rMkdirAll(cfgRoot, dir, 0o700); err != nil {
			return err
		}
	} else if !st.IsDir() {
		return terr("srv.teams_journal.lienRefuse")
	}
	if _, _, err := lstatNoLink(file); err != nil {
		return err
	}
	return rWriteAtomic(cfgRoot, file, b, 0o600)
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
	if _, _, err := lstatNoLink(dir); err != nil {
		return empty, nil
	}
	if _, ok, err := lstatNoLink(file); err != nil || !ok {
		return empty, nil
	}
	f, err := os.OpenFile(file, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return empty, nil
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, rosterMaxBytes+1))
	if err != nil || len(b) > rosterMaxBytes {
		return empty, nil
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil || !rosterValid(m) {
		return empty, nil
	}
	if m == nil {
		m = empty
	}
	return m, nil
}

func rosterDrop(cfgRoot, team string) error {
	dir, file, err := rosterPaths(cfgRoot, team)
	if err != nil {
		return err
	}
	teamsJournalMu.Lock()
	defer teamsJournalMu.Unlock()
	if _, _, err := lstatNoLink(dir); err != nil {
		return err
	}
	if err := rRemove(cfgRoot, file); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = rRemove(cfgRoot, dir) // ne réussit que si le dossier est vide
	return nil
}
