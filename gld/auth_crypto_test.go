package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func stubKeyring(t *testing.T) {
	g, s := keyringGet, keyringSet
	keyringGet = func() (string, error) { return "", errors.New("no keyring") }
	keyringSet = func(string) error { return errors.New("no keyring") }
	t.Cleanup(func() { keyringGet, keyringSet = g, s })
}

func TestSealOpen(t *testing.T) {
	k := bytes.Repeat([]byte{1}, 32)
	a, b := seal(k, []byte("x")), seal(k, []byte("x"))
	if bytes.Equal(a, b) {
		t.Fatal("nonce reused")
	}
	if p, err := open(k, a); err != nil || string(p) != "x" {
		t.Fatalf("roundtrip: %q %v", p, err)
	}
	a[len(a)-1] ^= 1
	if _, err := open(k, a); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}

func TestMasterKeyOrder(t *testing.T) {
	stubKeyring(t)
	dir := t.TempDir()
	t.Setenv("GLD_AUTH_KEY", strings.Repeat("ab", 32))
	k, err := loadMasterKey(dir)
	if err != nil || k[0] != 0xab {
		t.Fatalf("env: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.key")); err == nil {
		t.Fatal("file created despite env key")
	}
	t.Setenv("GLD_AUTH_KEY", "")
	k1, err := loadMasterKey(dir)
	if err != nil || len(k1) != 32 {
		t.Fatalf("file: %v", err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "auth.key"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if k2, _ := loadMasterKey(dir); !bytes.Equal(k1, k2) {
		t.Fatal("key not stable")
	}

	db, err := openAuthDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := checkMasterKey(db, k1); err != nil {
		t.Fatal(err)
	}
	if err := checkMasterKey(db, k1); err != nil {
		t.Fatal(err)
	}
	if err := checkMasterKey(db, bytes.Repeat([]byte{9}, 32)); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func TestRowSig(t *testing.T) {
	k := bytes.Repeat([]byte{2}, 32)
	s := rowSig(k, "users", "1", "a", "b")
	if !rowSigOK(k, "users", "1", s, "a", "b") {
		t.Fatal("valid sig rejected")
	}
	for name, ok := range map[string]bool{
		"table": rowSigOK(k, "sessions", "1", s, "a", "b"),
		"id":    rowSigOK(k, "users", "2", s, "a", "b"),
		"field": rowSigOK(k, "users", "1", s, "a", "c"),
		"split": rowSigOK(k, "users", "1", s, "ab", ""),
	} {
		if ok {
			t.Fatalf("%s change accepted", name)
		}
	}
}

func TestMasterKeyKeyringPaths(t *testing.T) {
	g, s := keyringGet, keyringSet
	t.Cleanup(func() { keyringGet, keyringSet = g, s })
	t.Setenv("GLD_AUTH_KEY", "")
	good := strings.Repeat("cd", 32)

	keyringGet = func() (string, error) { return " " + good + "\n", nil }
	keyringSet = func(string) error { t.Fatal("Set called"); return nil }
	if k, err := loadMasterKey(t.TempDir()); err != nil || k[0] != 0xcd {
		t.Fatalf("keyring first: %v", err)
	}

	keyringGet = func() (string, error) { return "", errors.New("locked") }
	dir := t.TempDir()
	if _, err := loadMasterKey(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.key")); err != nil {
		t.Fatal("transient error must fall back to file")
	}

	var stored string
	keyringGet = func() (string, error) { return "", keyring.ErrNotFound }
	keyringSet = func(v string) error { stored = v; return nil }
	dir = t.TempDir()
	if _, err := loadMasterKey(dir); err != nil || len(stored) != 64 {
		t.Fatalf("not found should store in keyring: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.key")); err == nil {
		t.Fatal("file written despite keyring")
	}
}

func TestMasterKeyInvalidEnv(t *testing.T) {
	stubKeyring(t)
	for _, v := range []string{"abcd", strings.Repeat("zz", 32)} {
		t.Setenv("GLD_AUTH_KEY", v)
		if _, err := loadMasterKey(t.TempDir()); err == nil {
			t.Fatalf("%q accepted", v)
		}
	}
	t.Setenv("GLD_AUTH_KEY", " "+strings.Repeat("ab", 32)+"\n")
	if _, err := loadMasterKey(t.TempDir()); err != nil {
		t.Fatalf("trim: %v", err)
	}
}

func TestOpenShortInput(t *testing.T) {
	k := bytes.Repeat([]byte{1}, 32)
	for _, in := range [][]byte{nil, {1, 2}} {
		if _, err := open(k, in); err == nil {
			t.Fatal("short input accepted")
		}
	}
}

func TestCheckMasterKeyWitnessDeleted(t *testing.T) {
	k := bytes.Repeat([]byte{1}, 32)
	db, err := openAuthDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := checkMasterKey(db, k); err != nil {
		t.Fatal(err)
	}
	db.Exec(`DELETE FROM meta WHERE key='key_witness'`)
	if err := checkMasterKey(db, k); err != nil {
		t.Fatalf("empty db should recreate witness: %v", err)
	}
	db.Exec(`DELETE FROM meta WHERE key='key_witness'`)
	if _, err := db.Exec(`INSERT INTO users(name,pw_hash,created_at,pw_changed_at) VALUES('n','h',0,0)`); err != nil {
		t.Fatal(err)
	}
	if err := checkMasterKey(db, bytes.Repeat([]byte{9}, 32)); err == nil {
		t.Fatal("wrong key accepted with witness deleted")
	}
}
