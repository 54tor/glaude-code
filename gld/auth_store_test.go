package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

// Base créée par le schéma 0005 : à l'ouverture, les sessions partent, les comptes à l'ancienne signature valide sont
// re-signés avec mfa_enrolled déduit de leurs facteurs, une ligne altérée le reste et le marqueur des comptes est posé.
func TestUpgradeFromSchema5(t *testing.T) {
	stubKeyring(t)
	dir := t.TempDir()
	t.Setenv("GLD_AUTH_DIR", dir)
	t.Setenv("GLD_AUTH_KEY", strings.Repeat("cd", 32))
	key := bytes.Repeat([]byte{0xcd}, 32)
	db, err := sql.Open("sqlite", filepath.Join(dir, "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	for v := 1; v <= 5; v++ {
		names, _ := fs.Glob(authMigrations, fmt.Sprintf("auth_migrations/%04d_*.sql", v))
		body, _ := authMigrations.ReadFile(names[0])
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	oldSig := func(id int64, name string) []byte { return rowSig(key, "users", strconv.FormatInt(id, 10), name, "h", "0", "0") }
	tr := &totpRow{uid: 1, nonce: []byte("n"), enc: []byte("e"), confirmed: sql.NullInt64{Int64: 5, Valid: true}}
	s0 := &authStore{key: key}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`CREATE TABLE schema_version (version INTEGER NOT NULL)`, nil},
		{`INSERT INTO schema_version VALUES (1),(2),(3),(4),(5)`, nil},
		{`INSERT INTO meta(key,value) VALUES('key_witness',?)`, []any{seal(key, []byte(witnessPlain))}},
		{`INSERT INTO users(id,name,pw_hash,is_admin,disabled,created_at,pw_changed_at,sig) VALUES(1,'a','h',0,0,0,0,?),(2,'b','h',0,0,0,0,?),(3,'c','h',0,0,0,0,x'00')`, []any{oldSig(1, "a"), oldSig(2, "b")}},
		{`INSERT INTO totp(user_id,secret_enc,nonce,confirmed_at,last_step,sig) VALUES(1,?,?,5,0,?)`, []any{tr.enc, tr.nonce, s0.totpSig(tr)}},
		{`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at,mfa_at) VALUES(x'01',1,0,0,99999999999,0)`, nil},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(q.sql, err)
		}
	}
	db.Close()
	os.Chmod(filepath.Join(dir, "auth.db"), 0o600)

	s, err := openAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	if n != 0 {
		t.Fatal("sessions conservées")
	}
	for id, want := range map[int64]bool{1: true, 2: false} {
		u, err := s.userByID(s.db, id)
		if err != nil || u.MFAEnrolled != want {
			t.Fatalf("compte %d : %+v %v", id, u, err)
		}
	}
	if _, err := s.userByID(s.db, 3); !errors.Is(err, errTamper) {
		t.Fatalf("ligne altérée re-signée : %v", err)
	}
	if on, err := s.AccountsOn(); !on || err != nil {
		t.Fatalf("marqueur : %v %v", on, err)
	}
	s.db.Exec(`DELETE FROM users`)
	if _, err := s.AccountsOn(); !errors.Is(err, errAccountsGone) {
		t.Fatalf("comptes supprimés : %v", err)
	}
}

func TestAccountsMarker(t *testing.T) {
	s := testStore(t)
	if on, err := s.AccountsOn(); on || err != nil {
		t.Fatalf("base neuve : %v %v", on, err)
	}
	if _, err := s.AddUser("alice", testPw, true); err != nil {
		t.Fatal(err)
	}
	if on, err := s.AccountsOn(); !on || err != nil {
		t.Fatalf("après un compte : %v %v", on, err)
	}
	s.db.Exec(`UPDATE meta SET value=x'01' WHERE key=?`, accountsKey)
	if _, err := s.AccountsOn(); !errors.Is(err, errTamper) {
		t.Fatalf("marqueur altéré : %v", err)
	}
}
