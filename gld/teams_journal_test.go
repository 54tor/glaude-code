package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const jTeam = "session-0123abcd"

func TestJournalRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := journalAppend(root, jTeam, DirectMsg{Author: "alice", To: "bob", Text: "salut"}); err != nil {
		t.Fatal(err)
	}
	if err := journalAppend(root, jTeam, DirectMsg{Author: "bob", To: "alice", Text: "yo", At: 5}); err != nil {
		t.Fatal(err)
	}
	ms, err := journalRead(root, jTeam)
	if err != nil || len(ms) != 2 {
		t.Fatalf("%v %v", ms, err)
	}
	if ms[0].Text != "salut" || ms[0].At == 0 || ms[1].At != 5 {
		t.Fatalf("%+v", ms)
	}
	st, _ := os.Stat(filepath.Join(root, "gld-teams-journal", jTeam+".jsonl"))
	if st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
}

func TestJournalBadTeamName(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../x", "session-ZZ", ""} {
		if journalAppend(root, name, DirectMsg{Author: "a", Text: "b"}) == nil {
			t.Fatal(name)
		}
		if _, err := journalRead(root, name); err == nil {
			t.Fatal(name)
		}
		if journalDrop(root, name) == nil {
			t.Fatal(name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "gld-teams-journal")); !os.IsNotExist(err) {
		t.Fatal("dossier créé")
	}
}

func TestJournalDrop(t *testing.T) {
	root := t.TempDir()
	if err := journalDrop(root, jTeam); err != nil {
		t.Fatal(err)
	}
	journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "b"})
	if err := journalDrop(root, jTeam); err != nil {
		t.Fatal(err)
	}
	ms, err := journalRead(root, jTeam)
	if err != nil || len(ms) != 0 {
		t.Fatal(ms, err)
	}
	if _, err := os.Stat(filepath.Join(root, "gld-teams-journal")); !os.IsNotExist(err) {
		t.Fatal("dossier vide non retiré")
	}
}

func TestJournalConcurrentAppend(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for index := 0; index < 50; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if err := journalAppend(root, jTeam, DirectMsg{Author: "a", Text: fmt.Sprint(index)}); err != nil {
				t.Error(err)
			}
		}(index)
	}
	wg.Wait()
	data, _ := os.ReadFile(filepath.Join(root, "gld-teams-journal", jTeam+".jsonl"))
	if count := strings.Count(string(data), "\n"); count != 50 {
		t.Fatal(count)
	}
	ms, _ := journalRead(root, jTeam)
	if len(ms) != 50 {
		t.Fatal(len(ms))
	}
}

func TestJournalSkipsMalformedLine(t *testing.T) {
	root := t.TempDir()
	journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "un"})
	path := filepath.Join(root, "gld-teams-journal", jTeam+".jsonl")
	file, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	file.WriteString("{\"Author\":\"x\",\"Te\n")
	file.Close()
	journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "deux"})
	ms, err := journalRead(root, jTeam)
	if err != nil || len(ms) != 2 {
		t.Fatal(ms, err)
	}
}

func TestJournalSanitises(t *testing.T) {
	root := t.TempDir()
	err := journalAppend(root, jTeam, DirectMsg{Author: "  al\x00i\nce" + strings.Repeat("é", 100), To: "b\x07ob", Text: strings.Repeat("x", 9000)})
	if err != nil {
		t.Fatal(err)
	}
	directMsg := mustOne(t, root)
	if strings.ContainsAny(directMsg.Author+directMsg.To, "\x00\n\x07") || []rune(directMsg.Author)[0] != 'a' {
		t.Fatalf("%q %q", directMsg.Author, directMsg.To)
	}
	if len([]rune(directMsg.Author)) != 64 || directMsg.To != "bob" || len(directMsg.Text) != 8000 {
		t.Fatal(len([]rune(directMsg.Author)), directMsg.To, len(directMsg.Text))
	}
	for _, bad := range []DirectMsg{{Author: " ", Text: "x"}, {Author: "a"}} {
		if !isMsg(journalAppend(root, jTeam, bad), "srv.teams_journal.messageInvalide") {
			t.Fatal(bad)
		}
	}
}

func mustOne(t *testing.T, root string) DirectMsg {
	ms, err := journalRead(root, jTeam)
	if err != nil || len(ms) != 1 {
		t.Fatal(ms, err)
	}
	return ms[0]
}

func TestJournalFull(t *testing.T) {
	root := t.TempDir()
	journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "x"})
	path := filepath.Join(root, "gld-teams-journal", jTeam+".jsonl")
	if err := os.Truncate(path, journalMax); err != nil {
		t.Fatal(err)
	}
	if !isMsg(journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "y"}), "srv.teams_journal.journalPlein") {
		t.Fatal("pas d'erreur")
	}
	if st, _ := os.Stat(path); st.Size() != journalMax {
		t.Fatal("modifié")
	}
}

func TestJournalSymlinkRefused(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	dir := filepath.Join(root, "gld-teams-journal")
	os.Symlink(other, dir)
	if !isMsg(journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "x"}), "srv.teams_journal.lienRefuse") {
		t.Fatal("dossier lien suivi")
	}
	os.Remove(dir)
	os.Mkdir(dir, 0o700)
	target := filepath.Join(other, "t")
	os.WriteFile(target, nil, 0o600)
	os.Symlink(target, filepath.Join(dir, jTeam+".jsonl"))
	if !isMsg(journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "x"}), "srv.teams_journal.lienRefuse") {
		t.Fatal("fichier lien suivi")
	}
	if _, err := journalRead(root, jTeam); !isMsg(err, "srv.teams_journal.lienRefuse") {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); len(data) != 0 {
		t.Fatal("cible écrite")
	}
}

func TestJournalReadLast500(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 520; index++ {
		journalAppend(root, jTeam, DirectMsg{Author: "a", Text: fmt.Sprint(index)})
	}
	ms, _ := journalRead(root, jTeam)
	if len(ms) != 500 || ms[0].Text != "20" || ms[499].Text != "519" {
		t.Fatal(len(ms), ms[0].Text)
	}
}

func TestJournalRecoversAfterPartialLine(t *testing.T) {
	root := t.TempDir()
	journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "un"})
	path := filepath.Join(root, "gld-teams-journal", jTeam+".jsonl")
	file, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	file.WriteString(`{"Author":"x","Te`)
	file.Close()
	journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "deux"})
	ms, err := journalRead(root, jTeam)
	if err != nil || len(ms) != 2 || ms[1].Text != "deux" {
		t.Fatal(ms, err)
	}
}

func TestJournalSymlinkedDirDropAndRead(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	target := filepath.Join(other, jTeam+".jsonl")
	os.WriteFile(target, []byte("{}\n"), 0o600)
	os.Symlink(other, filepath.Join(root, "gld-teams-journal"))
	if !isMsg(journalDrop(root, jTeam), "srv.teams_journal.lienRefuse") {
		t.Fatal("drop")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("cible supprimée")
	}
	if _, err := journalRead(root, jTeam); !isMsg(err, "srv.teams_journal.lienRefuse") {
		t.Fatal(err)
	}
}
