package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

// Journal des messages que les humains envoient directement à un agent : les boîtes de Claude Code ne les gardent pas.
type DirectMsg struct {
	Author, To, Text string
	At               int64
}

const (
	journalMax  = 4 << 20
	journalLast = 500
	journalDir  = "gld-teams-journal"
)

// Un seul verrou pour tout : le volume est celui de messages tapés à la main.
var teamsJournalMu sync.Mutex

// journalPaths valide le nom avant tout accès disque.
func journalPaths(cfgRoot, team string) (dir, file string, err error) {
	if !teamNameRe.MatchString(team) {
		return "", "", terr("srv.teams_journal.nomInvalide")
	}
	dir = filepath.Join(cfgRoot, journalDir)
	return dir, filepath.Join(dir, team+".jsonl"), nil
}

// lstatNoLink : (info, existe, err) ; un lien symbolique est refusé.
func lstatNoLink(p string) (os.FileInfo, bool, error) {
	st, err := os.Lstat(p)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return nil, false, terr("srv.teams_journal.lienRefuse")
	}
	return st, true, nil
}

func cleanField(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 64 {
		s = string(r[:64])
	}
	return s
}

func journalAppend(cfgRoot, team string, m DirectMsg) error {
	dir, file, err := journalPaths(cfgRoot, team)
	if err != nil {
		return err
	}
	m.Author, m.To = cleanField(m.Author), cleanField(m.To)
	if len(m.Text) > 8000 {
		m.Text = m.Text[:8000]
		for !utf8.ValidString(m.Text) {
			m.Text = m.Text[:len(m.Text)-1]
		}
	}
	if m.Author == "" || strings.TrimSpace(m.Text) == "" {
		return terr("srv.teams_journal.messageInvalide")
	}
	if m.At == 0 {
		m.At = time.Now().UnixMilli()
	}
	line, err := json.Marshal(m)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	teamsJournalMu.Lock()
	defer teamsJournalMu.Unlock()
	st, ok, err := lstatNoLink(dir)
	if err != nil {
		return err
	}
	if !ok {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	} else if !st.IsDir() {
		return terr("srv.teams_journal.lienRefuse")
	}
	fst, ok, err := lstatNoLink(file)
	if err != nil {
		return err
	}
	if ok && fst.Size()+int64(len(line)) > journalMax {
		return terr("srv.teams_journal.journalPlein")
	}
	f, err := os.OpenFile(file, os.O_RDWR|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	// un fragment sans « \n » (arrêt brutal) ne doit pas absorber la ligne suivante
	if ok && fst.Size() > 0 {
		var last [1]byte
		if _, e := f.ReadAt(last[:], fst.Size()-1); e == nil && last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	_, err = f.Write(line)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func journalRead(cfgRoot, team string) ([]DirectMsg, error) {
	dir, file, err := journalPaths(cfgRoot, team)
	if err != nil {
		return nil, err
	}
	teamsJournalMu.Lock()
	defer teamsJournalMu.Unlock()
	if _, _, err := lstatNoLink(dir); err != nil {
		return nil, err
	}
	if _, ok, err := lstatNoLink(file); err != nil || !ok {
		return nil, err
	}
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, journalMax))
	if err != nil {
		return nil, err
	}
	out := []DirectMsg{}
	for _, l := range strings.Split(string(b), "\n") {
		var m DirectMsg
		// une ligne coupée par un arrêt brutal est ignorée
		if l != "" && json.Unmarshal([]byte(l), &m) == nil {
			out = append(out, m)
		}
	}
	if len(out) > journalLast {
		out = out[len(out)-journalLast:]
	}
	return out, nil
}

func journalDrop(cfgRoot, team string) error {
	dir, file, err := journalPaths(cfgRoot, team)
	if err != nil {
		return err
	}
	teamsJournalMu.Lock()
	defer teamsJournalMu.Unlock()
	if _, _, err := lstatNoLink(dir); err != nil {
		return err
	}
	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(dir) // ne réussit que si le dossier est vide
	return nil
}
