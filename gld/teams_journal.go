package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	journalFieldMax = 64   // auteur et destinataire, en caractères
	journalTextMax  = 8000 // texte d'un message, en octets
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
func lstatNoLink(filePath string) (os.FileInfo, bool, error) {
	fileInfo, err := os.Lstat(filePath)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return nil, false, terr("srv.teams_journal.lienRefuse")
	}
	return fileInfo, true, nil
}

func cleanField(text string) string {
	text = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, text)
	text = strings.TrimSpace(text)
	if runes := []rune(text); len(runes) > journalFieldMax {
		text = string(runes[:journalFieldMax])
	}
	return text
}

func journalAppend(cfgRoot, team string, message DirectMsg) error {
	dir, file, err := journalPaths(cfgRoot, team)
	if err != nil {
		return err
	}
	message.Author, message.To = cleanField(message.Author), cleanField(message.To)
	if len(message.Text) > journalTextMax {
		message.Text = message.Text[:journalTextMax]
		for !utf8.ValidString(message.Text) {
			message.Text = message.Text[:len(message.Text)-1]
		}
	}
	if message.Author == "" || strings.TrimSpace(message.Text) == "" {
		return terr("srv.teams_journal.messageInvalide")
	}
	if message.At == 0 {
		message.At = time.Now().UnixMilli()
	}
	journalLine, err := json.Marshal(message)
	if err != nil {
		return err
	}
	journalLine = append(journalLine, '\n')

	teamsJournalMu.Lock()
	defer teamsJournalMu.Unlock()
	dirInfo, exists, err := lstatNoLink(dir)
	if err != nil {
		return err
	}
	if !exists {
		if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
			return mkdirErr
		}
	} else if !dirInfo.IsDir() {
		return terr("srv.teams_journal.lienRefuse")
	}
	fileInfo, exists, err := lstatNoLink(file)
	if err != nil {
		return err
	}
	if exists && fileInfo.Size()+int64(len(journalLine)) > journalMax {
		return terr("srv.teams_journal.journalPlein")
	}
	journalFile, err := os.OpenFile(file, os.O_RDWR|os.O_APPEND|os.O_CREATE|oNoFollow, 0o600)
	if err != nil {
		return err
	}
	// un fragment sans « \n » (arrêt brutal) ne doit pas absorber la ligne suivante
	if exists && fileInfo.Size() > 0 {
		var lastByte [1]byte
		if _, readErr := journalFile.ReadAt(lastByte[:], fileInfo.Size()-1); readErr == nil && lastByte[0] != '\n' {
			journalLine = append([]byte{'\n'}, journalLine...)
		}
	}
	_, err = journalFile.Write(journalLine)
	if closeErr := journalFile.Close(); err == nil {
		err = closeErr
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
	if _, _, dirErr := lstatNoLink(dir); dirErr != nil {
		return nil, dirErr
	}
	if _, exists, fileErr := lstatNoLink(file); fileErr != nil || !exists {
		return nil, fileErr
	}
	journalFile, err := os.OpenFile(file, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return nil, err
	}
	defer journalFile.Close()
	content, err := io.ReadAll(io.LimitReader(journalFile, journalMax))
	if err != nil {
		return nil, err
	}
	messages := []DirectMsg{}
	for _, journalLine := range strings.Split(string(content), "\n") {
		var message DirectMsg
		// une ligne coupée par un arrêt brutal est ignorée
		if journalLine != "" && json.Unmarshal([]byte(journalLine), &message) == nil {
			messages = append(messages, message)
		}
	}
	if len(messages) > journalLast {
		messages = messages[len(messages)-journalLast:]
	}
	return messages, nil
}

func journalDrop(cfgRoot, team string) error {
	dir, file, err := journalPaths(cfgRoot, team)
	if err != nil {
		return err
	}
	teamsJournalMu.Lock()
	defer teamsJournalMu.Unlock()
	if _, _, dirErr := lstatNoLink(dir); dirErr != nil {
		return dirErr
	}
	if removeErr := os.Remove(file); removeErr != nil && !os.IsNotExist(removeErr) {
		return removeErr
	}
	_ = os.Remove(dir) // ne réussit que si le dossier est vide
	return nil
}
