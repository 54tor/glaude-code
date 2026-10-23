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
	db.QueryRow(`SELECT MAX(version), COUNT(*) FROM schema_version`).Scan(&v, &rows)
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

func TestAuthDirEnv(t *testing.T) {
	t.Setenv("GLD_AUTH_DIR", "/x/y")
	if authDir() != "/x/y" {
		t.Fatal(authDir())
	}
	t.Setenv("GLD_AUTH_DIR", "")
	base, _ := os.UserConfigDir()
	if authDir() != filepath.Join(base, "gld") {
		t.Fatal(authDir())
	}
}
