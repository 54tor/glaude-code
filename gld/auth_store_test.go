package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenAuthDB(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub")
	db, err := openAuthDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode %v", st.Mode().Perm())
		}
		if st, _ := os.Stat(filepath.Join(dir, "auth.db")); st.Mode().Perm() != 0o600 {
			t.Fatalf("db mode %v", st.Mode().Perm())
		}
	}
	db, err = openAuthDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v, rows int
	if err := db.QueryRow(`SELECT MAX(version), COUNT(*) FROM schema_version`).Scan(&v, &rows); err != nil {
		t.Fatal(err)
	}
	entries, _ := authMigrations.ReadDir("auth_migrations")
	if v != len(entries) || rows != len(entries) {
		t.Fatalf("version %d rows %d, want %d", v, rows, len(entries))
	}
	for _, tb := range []string{"users", "totp", "webauthn_creds", "recovery_codes", "sessions", "home_acl", "auth_events", "throttle", "meta"} {
		if _, err := db.Exec("SELECT 1 FROM " + tb + " LIMIT 1"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenAuthDBHardening(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "we?ird#%41")
	db, err := openAuthDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var fk int
	var jm string
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("fk %d %v", fk, err)
	}
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&jm); err != nil || jm != "wal" {
		t.Fatalf("jm %q %v", jm, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for _, f := range []string{"auth.db", "auth.db-wal", "auth.db-shm"} {
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", f, st, err)
		}
	}
}

func TestOpenAuthDBRefuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	base := t.TempDir()
	real := filepath.Join(base, "real")
	os.Mkdir(real, 0o700)
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := openAuthDB(link); err == nil {
		t.Fatal("symlinked dir accepted")
	}
	d2 := filepath.Join(base, "d2")
	os.Mkdir(d2, 0o700)
	os.Symlink(filepath.Join(real, "x.db"), filepath.Join(d2, "auth.db"))
	if _, err := openAuthDB(d2); err == nil {
		t.Fatal("symlinked db accepted")
	}
}

func TestAuthDirNoTempFallback(t *testing.T) {
	t.Setenv("GLD_AUTH_DIR", "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if d, err := authDir(); err == nil {
		t.Fatalf("expected error, got %q", d)
	}
}

func TestAuthDirEnv(t *testing.T) {
	t.Setenv("GLD_AUTH_DIR", "/x/y")
	if d, err := authDir(); err != nil || d != "/x/y" {
		t.Fatal(d, err)
	}
	t.Setenv("GLD_AUTH_DIR", "")
	base, _ := os.UserConfigDir()
	if d, err := authDir(); err != nil || d != filepath.Join(base, "gld") {
		t.Fatal(d, err)
	}
}
