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
	callback, localCallback := keyringGet, keyringSet
	keyringGet = func() (string, error) { return "", errors.New("no keyring") }
	keyringSet = func(string) error { return errors.New("no keyring") }
	t.Cleanup(func() { keyringGet, keyringSet = callback, localCallback })
}

func TestSealOpen(t *testing.T) {
	data := bytes.Repeat([]byte{1}, 32)
	localData, anotherData := seal(data, []byte("x")), seal(data, []byte("x"))
	if bytes.Equal(localData, anotherData) {
		t.Fatal("nonce reused")
	}
	if anotherData, err := open(data, localData); err != nil || string(anotherData) != "x" {
		t.Fatalf("roundtrip: %q %v", anotherData, err)
	}
	localData[len(localData)-1] ^= 1
	if _, err := open(data, localData); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}

func TestMasterKeyOrder(t *testing.T) {
	stubKeyring(t)
	dir := t.TempDir()
	t.Setenv("GLD_AUTH_KEY", strings.Repeat("ab", 32))
	data, err := loadMasterKey(dir, emptyDB(t))
	if err != nil || data[0] != 0xab {
		t.Fatalf("env: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.key")); err == nil {
		t.Fatal("file created despite env key")
	}
	t.Setenv("GLD_AUTH_KEY", "")
	k1, err := loadMasterKey(dir, emptyDB(t))
	if err != nil || len(k1) != 32 {
		t.Fatalf("file: %v", err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "auth.key"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if k2, _ := loadMasterKey(dir, emptyDB(t)); !bytes.Equal(k1, k2) {
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
	data := bytes.Repeat([]byte{2}, 32)
	localData := rowSig(data, "users", "1", "a", "b")
	if !rowSigOK(data, "users", "1", localData, "a", "b") {
		t.Fatal("valid sig rejected")
	}
	for name, ok := range map[string]bool{
		"table": rowSigOK(data, "sessions", "1", localData, "a", "b"),
		"id":    rowSigOK(data, "users", "2", localData, "a", "b"),
		"field": rowSigOK(data, "users", "1", localData, "a", "c"),
		"split": rowSigOK(data, "users", "1", localData, "ab", ""),
	} {
		if ok {
			t.Fatalf("%s change accepted", name)
		}
	}
}

func TestMasterKeyKeyringPaths(t *testing.T) {
	callback, localCallback := keyringGet, keyringSet
	t.Cleanup(func() { keyringGet, keyringSet = callback, localCallback })
	t.Setenv("GLD_AUTH_KEY", "")
	good := strings.Repeat("cd", 32)

	keyringGet = func() (string, error) { return " " + good + "\n", nil }
	keyringSet = func(string) error { t.Fatal("Set called"); return nil }
	if data, err := loadMasterKey(t.TempDir(), emptyDB(t)); err != nil || data[0] != 0xcd {
		t.Fatalf("keyring first: %v", err)
	}

	keyringGet = func() (string, error) { return "", errors.New("locked") }
	dir := t.TempDir()
	if _, err := loadMasterKey(dir, emptyDB(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.key")); err != nil {
		t.Fatal("transient error must fall back to file")
	}

	var stored string
	keyringGet = func() (string, error) { return "", keyring.ErrNotFound }
	keyringSet = func(value string) error { stored = value; return nil }
	dir = t.TempDir()
	if _, err := loadMasterKey(dir, emptyDB(t)); err != nil || len(stored) != 64 {
		t.Fatalf("not found should store in keyring: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.key")); err == nil {
		t.Fatal("file written despite keyring")
	}
}

func TestMasterKeyInvalidEnv(t *testing.T) {
	stubKeyring(t)
	for _, value := range []string{"abcd", strings.Repeat("zz", 32)} {
		t.Setenv("GLD_AUTH_KEY", value)
		if _, err := loadMasterKey(t.TempDir(), emptyDB(t)); err == nil {
			t.Fatalf("%q accepted", value)
		}
	}
	t.Setenv("GLD_AUTH_KEY", " "+strings.Repeat("ab", 32)+"\n")
	if _, err := loadMasterKey(t.TempDir(), emptyDB(t)); err != nil {
		t.Fatalf("trim: %v", err)
	}
}

func TestOpenShortInput(t *testing.T) {
	data := bytes.Repeat([]byte{1}, 32)
	for _, in := range [][]byte{nil, {1, 2}} {
		if _, err := open(data, in); err == nil {
			t.Fatal("short input accepted")
		}
	}
}

func TestCheckMasterKeyWitnessDeleted(t *testing.T) {
	data := bytes.Repeat([]byte{1}, 32)
	db, err := openAuthDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := checkMasterKey(db, data); err != nil {
		t.Fatal(err)
	}
	db.Exec(`DELETE FROM meta WHERE key='key_witness'`)
	if err := checkMasterKey(db, data); err == nil {
		t.Fatal("witness deleted beside the accounts state: recreated")
	}
	db.Exec(`DELETE FROM meta WHERE key='accounts'`)
	if err := checkMasterKey(db, data); err != nil {
		t.Fatalf("empty db should recreate witness: %v", err)
	}
	db.Exec(`DELETE FROM meta WHERE key='key_witness'`)
	if _, err := db.Exec(`INSERT INTO users(name,pw_hash,created_at,pw_changed_at) VALUES('n','h',0,0)`); err != nil {
		t.Fatal(err)
	}
	if err := checkMasterKey(db, data); err == nil {
		t.Fatal("populated db without witness accepted")
	}
	db.Exec(`DELETE FROM users`)
	db.Exec(`PRAGMA foreign_keys=OFF`)
	if _, err := db.Exec(`INSERT INTO home_acl(user_id,home_id) VALUES(99,'h')`); err != nil {
		t.Fatal(err)
	}
	if err := checkMasterKey(db, data); err == nil {
		t.Fatal("home_acl row without witness accepted")
	}
}

func emptyDB(t *testing.T) *authDB {
	t.Helper()
	db, err := openAuthDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Coffre-fort en erreur sans auth.key sur une base qui a déjà un témoin : aucune clé n'est créée ni écrite.
func TestMasterKeyNoRegenerate(t *testing.T) {
	callback, localCallback := keyringGet, keyringSet
	t.Cleanup(func() { keyringGet, keyringSet = callback, localCallback })
	t.Setenv("GLD_AUTH_KEY", "")
	db := emptyDB(t)
	if err := checkMasterKey(db, bytes.Repeat([]byte{3}, 32)); err != nil {
		t.Fatal(err)
	}
	for _, kerr := range []error{errors.New("locked"), keyring.ErrNotFound} {
		keyringGet = func() (string, error) { return "", kerr }
		keyringSet = func(string) error { t.Fatal("Set called"); return nil }
		dir := t.TempDir()
		if _, err := loadMasterKey(dir, db); err == nil {
			t.Fatalf("%v : clé régénérée sur une base peuplée", kerr)
		}
		if _, err := os.Stat(filepath.Join(dir, "auth.key")); err == nil {
			t.Fatalf("%v : auth.key écrite", kerr)
		}
	}
	keyringGet = func() (string, error) { return "", errors.New("locked") }
	dir := t.TempDir()
	if _, err := loadMasterKey(dir, emptyDB(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.key")); err != nil {
		t.Fatal("base neuve : clé non générée")
	}
}
