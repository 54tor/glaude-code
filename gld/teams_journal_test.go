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
	for _, n := range []string{"../x", "session-ZZ", ""} {
		if journalAppend(root, n, DirectMsg{Author: "a", Text: "b"}) == nil {
			t.Fatal(n)
		}
		if _, err := journalRead(root, n); err == nil {
			t.Fatal(n)
		}
		if journalDrop(root, n) == nil {
			t.Fatal(n)
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
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := journalAppend(root, jTeam, DirectMsg{Author: "a", Text: fmt.Sprint(i)}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	b, _ := os.ReadFile(filepath.Join(root, "gld-teams-journal", jTeam+".jsonl"))
	if n := strings.Count(string(b), "\n"); n != 50 {
		t.Fatal(n)
	}
	ms, _ := journalRead(root, jTeam)
	if len(ms) != 50 {
		t.Fatal(len(ms))
	}
}

func TestJournalSkipsMalformedLine(t *testing.T) {
	root := t.TempDir()
	journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "un"})
	p := filepath.Join(root, "gld-teams-journal", jTeam+".jsonl")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("{\"Author\":\"x\",\"Te\n")
	f.Close()
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
	m := mustOne(t, root)
	if strings.ContainsAny(m.Author+m.To, "\x00\n\x07") || []rune(m.Author)[0] != 'a' {
		t.Fatalf("%q %q", m.Author, m.To)
	}
	if len([]rune(m.Author)) != 64 || m.To != "bob" || len(m.Text) != 8000 {
		t.Fatal(len([]rune(m.Author)), m.To, len(m.Text))
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
	p := filepath.Join(root, "gld-teams-journal", jTeam+".jsonl")
	if err := os.Truncate(p, journalMax); err != nil {
		t.Fatal(err)
	}
	if !isMsg(journalAppend(root, jTeam, DirectMsg{Author: "a", Text: "y"}), "srv.teams_journal.journalPlein") {
		t.Fatal("pas d'erreur")
	}
	if st, _ := os.Stat(p); st.Size() != journalMax {
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
	if b, _ := os.ReadFile(target); len(b) != 0 {
		t.Fatal("cible écrite")
	}
}

func TestJournalReadLast500(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 520; i++ {
		journalAppend(root, jTeam, DirectMsg{Author: "a", Text: fmt.Sprint(i)})
	}
	ms, _ := journalRead(root, jTeam)
	if len(ms) != 500 || ms[0].Text != "20" || ms[499].Text != "519" {
		t.Fatal(len(ms), ms[0].Text)
	}
}
