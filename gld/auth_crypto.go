package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "gld"
	keyringUser    = "auth-master-key"
	witnessPlain   = "gld-auth-witness"
)

var (
	keyringGet = func() (string, error) { return keyring.Get(keyringService, keyringUser) }
	keyringSet = func(v string) error { return keyring.Set(keyringService, keyringUser, v) }
)

func decodeKey(s string) ([]byte, error) {
	k, err := hex.DecodeString(s)
	if err != nil || len(k) != 32 {
		return nil, errors.New("master key must be 64 hex characters")
	}
	return k, nil
}

// loadMasterKey: GLD_AUTH_KEY, then OS keyring, then auth.key. A new key goes to
// the keyring only when it reports the entry absent; any other keyring error
// (locked, bus failure) falls back to the file and never overwrites the entry.
func loadMasterKey(dir string) ([]byte, error) {
	if e := strings.TrimSpace(os.Getenv("GLD_AUTH_KEY")); e != "" {
		return decodeKey(e)
	}
	s, gerr := keyringGet()
	if gerr == nil {
		return decodeKey(strings.TrimSpace(s))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "auth.key")
	if _, err := os.Lstat(path); err == nil {
		if err := checkPrivate(path, false); err != nil {
			return nil, err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return decodeKey(strings.TrimSpace(string(b)))
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	enc := hex.EncodeToString(k)
	if errors.Is(gerr, keyring.ErrNotFound) && keyringSet(enc) == nil {
		return k, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	_, werr := f.WriteString(enc)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return nil, werr
	}
	return k, nil
}

func seal(key, plain []byte) []byte {
	g := newGCM(key)
	n := make([]byte, g.NonceSize())
	if _, err := rand.Read(n); err != nil {
		panic(err)
	}
	return g.Seal(n, n, plain, nil)
}

func open(key, boxed []byte) ([]byte, error) {
	g := newGCM(key)
	if len(boxed) < g.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	return g.Open(nil, boxed[:g.NonceSize()], boxed[g.NonceSize():], nil)
}

func newGCM(key []byte) cipher.AEAD {
	b, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		panic(err)
	}
	return g
}

func rowSigKey(key []byte) []byte {
	k, err := hkdf.Key(sha256.New, key, nil, "gld-auth-integrity", 32)
	if err != nil {
		panic(err)
	}
	return k
}

// Fields are length-prefixed so ("ab","c") and ("a","bc") sign differently.
func rowSig(key []byte, table, id string, fields ...string) []byte {
	m := hmac.New(sha256.New, rowSigKey(key))
	for _, s := range append([]string{table, id}, fields...) {
		binary.Write(m, binary.BigEndian, uint64(len(s)))
		m.Write([]byte(s))
	}
	return m.Sum(nil)
}

func rowSigOK(key []byte, table, id string, sig []byte, fields ...string) bool {
	return hmac.Equal(sig, rowSig(key, table, id, fields...))
}

func checkMasterKey(db *authDB, key []byte) error {
	var w []byte
	err := db.QueryRow(`SELECT value FROM meta WHERE key='key_witness'`).Scan(&w)
	if errors.Is(err, sql.ErrNoRows) {
		var n int
		if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM users)+(SELECT COUNT(*) FROM totp)+(SELECT COUNT(*) FROM webauthn_creds)+(SELECT COUNT(*) FROM recovery_codes)+(SELECT COUNT(*) FROM sessions)+(SELECT COUNT(*) FROM home_acl)`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return errors.New("key witness missing on a populated auth database; refusing to start")
		}
		_, err = db.Exec(`INSERT INTO meta(key,value) VALUES('key_witness',?)`, seal(key, []byte(witnessPlain)))
		return err
	}
	if err != nil {
		return err
	}
	if p, err := open(key, w); err != nil || string(p) != witnessPlain {
		return fmt.Errorf("master key does not match the auth database; refusing to start")
	}
	return nil
}
