package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rosterTeam = "session-0123abcd"

func rosterFile(root string) string {
	return filepath.Join(root, journalDir, rosterTeam+".roster.json")
}

func TestRosterRoundTrip(t *testing.T) {
	root := t.TempDir()
	in := map[string]string{"marcus": "testeur", "ana": "dev-2"}
	if err := rosterWrite(root, rosterTeam, in); err != nil {
		t.Fatal(err)
	}
	out, err := rosterRead(root, rosterTeam)
	if err != nil || len(out) != 2 || out["marcus"] != "testeur" || out["ana"] != "dev-2" {
		t.Fatalf("%v %v", out, err)
	}
	if st, _ := os.Stat(rosterFile(root)); st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v", st.Mode())
	}
	if st, _ := os.Stat(filepath.Dir(rosterFile(root))); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", st.Mode())
	}
}

func TestRosterBadTeamName(t *testing.T) {
	root := t.TempDir()
	for _, team := range []string{"../x", "session-ZZ", ""} {
		if err := rosterWrite(root, team, map[string]string{"ana": "dev"}); err == nil {
			t.Fatalf("write %q accepted", team)
		}
		if _, err := rosterRead(root, team); err == nil {
			t.Fatalf("read %q accepted", team)
		}
		if err := rosterDrop(root, team); err == nil {
			t.Fatalf("drop %q accepted", team)
		}
	}
	if _, err := os.Stat(filepath.Join(root, journalDir)); !os.IsNotExist(err) {
		t.Fatal("directory created")
	}
}

func TestRosterMissingOrCorrupt(t *testing.T) {
	root := t.TempDir()
	if m, err := rosterRead(root, rosterTeam); err != nil || len(m) != 0 {
		t.Fatalf("absent: %v %v", m, err)
	}
	if err := os.MkdirAll(filepath.Dir(rosterFile(root)), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"truncated": `{"marcus":"test`,
		"badvalue":  `{"marcus":"../x"}`,
		"badkey":    `{"Marcus":"dev"}`,
		"oversized": `{"marcus":"` + strings.Repeat("a", 70<<10) + `"}`,
	} {
		if err := os.WriteFile(rosterFile(root), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if m, err := rosterRead(root, rosterTeam); err != nil || len(m) != 0 {
			t.Fatalf("%s: %v %v", name, m, err)
		}
	}
}

func TestRosterRejectsInvalidEntryOnWrite(t *testing.T) {
	root := t.TempDir()
	for _, m := range []map[string]string{
		{"Marcus": "dev"}, {"a": "dev"}, {"marcus1": "dev"}, {"marcus": "../x"}, {"marcus": ""}, {"marcus": "Dev"},
	} {
		if err := rosterWrite(root, rosterTeam, m); err == nil {
			t.Fatalf("accepted %v", m)
		}
	}
	big := map[string]string{}
	for i := 0; i < 65; i++ {
		big[string(rune('a'+i/26))+string(rune('a'+i%26))] = "dev"
	}
	if err := rosterWrite(root, rosterTeam, big); err == nil {
		t.Fatal("65 entries accepted")
	}
}

func TestRosterSymlinkRefused(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, journalDir)); err != nil {
		t.Fatal(err)
	}
	if err := rosterWrite(root, rosterTeam, map[string]string{"ana": "dev"}); err == nil {
		t.Fatal("write through dir symlink")
	}
	if err := rosterDrop(root, rosterTeam); err == nil {
		t.Fatal("drop through dir symlink")
	}
	if es, _ := os.ReadDir(target); len(es) != 0 {
		t.Fatal("target touched")
	}

	root2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root2, journalDir), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(secret, []byte(`{"ana":"dev"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, rosterFile(root2)); err != nil {
		t.Fatal(err)
	}
	if m, _ := rosterRead(root2, rosterTeam); len(m) != 0 {
		t.Fatalf("read through file symlink: %v", m)
	}
	if err := rosterWrite(root2, rosterTeam, map[string]string{"bob": "dev"}); err == nil {
		t.Fatal("write over file symlink")
	}
}

func TestRosterDropKeepsJournal(t *testing.T) {
	root := t.TempDir()
	if err := rosterWrite(root, rosterTeam, map[string]string{"ana": "dev"}); err != nil {
		t.Fatal(err)
	}
	j := filepath.Join(root, journalDir, rosterTeam+".jsonl")
	if err := os.WriteFile(j, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rosterDrop(root, rosterTeam); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(j); err != nil {
		t.Fatal("journal removed")
	}
	if _, err := os.Stat(rosterFile(root)); !os.IsNotExist(err) {
		t.Fatal("roster kept")
	}
}

func TestRosterDrop(t *testing.T) {
	root := t.TempDir()
	if err := rosterDrop(root, rosterTeam); err != nil {
		t.Fatalf("absent: %v", err)
	}
	if err := rosterWrite(root, rosterTeam, map[string]string{"ana": "dev"}); err != nil {
		t.Fatal(err)
	}
	if err := rosterDrop(root, rosterTeam); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, journalDir)); !os.IsNotExist(err) {
		t.Fatal("empty dir kept")
	}
}
