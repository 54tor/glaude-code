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
// the keyring when available, else to auth.key.
func loadMasterKey(dir string) ([]byte, error) {
	if e := os.Getenv("GLD_AUTH_KEY"); e != "" {
		return decodeKey(e)
	}
	if s, err := keyringGet(); err == nil {
		return decodeKey(s)
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
		return decodeKey(string(b))
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	enc := hex.EncodeToString(k)
	if keyringSet(enc) == nil {
		return k, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.WriteString(enc); err != nil {
		return nil, err
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
