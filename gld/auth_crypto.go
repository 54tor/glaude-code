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
	"sync/atomic"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "gld"
	keyringUser    = "auth-master-key"
	witnessPlain   = "gld-auth-witness"
	masterKeyLen   = 32
)

var (
	keyringGet = func() (string, error) { return keyring.Get(keyringService, keyringUser) }
	keyringSet = func(encodedKey string) error { return keyring.Set(keyringService, keyringUser, encodedKey) }
)

func decodeKey(hexKey string) ([]byte, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != masterKeyLen {
		return nil, errors.New("master key must be 64 hex characters")
	}
	return key, nil
}

// keyFromFile : la clé vient de auth.key (protection plus faible qu'un coffre-fort) ; lu par main pour avertir.
var keyFromFile atomic.Bool

// loadMasterKey: GLD_AUTH_KEY, then OS keyring, then auth.key. A new key goes to
// the keyring only when it reports the entry absent; any other keyring error
// (locked, bus failure) falls back to the file and never overwrites the entry.
// A base that already carries a key witness never gets a new key: regenerating
// would silently orphan every signed row.
func loadMasterKey(dir string, db *authDB) ([]byte, error) {
	if envKey := strings.TrimSpace(os.Getenv("GLD_AUTH_KEY")); envKey != "" {
		// La clé reste dans /proc/<gld>/environ, lisible par les sessions du même compte tant que gld est « dumpable ».
		if err := hideEnviron(); err != nil {
			journal("hideEnviron : %v", err)
		}
		return decodeKey(envKey)
	}
	keyringValue, keyringErr := keyringGet()
	if keyringErr == nil {
		return decodeKey(strings.TrimSpace(keyringValue))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "auth.key")
	if _, statErr := os.Lstat(path); statErr == nil {
		if permErr := checkPrivate(path, false); permErr != nil {
			return nil, permErr
		}
		keyContent, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		keyFromFile.Store(true)
		return decodeKey(strings.TrimSpace(string(keyContent)))
	}
	var witnessMarker int
	switch err := db.QueryRow(`SELECT 1 FROM meta WHERE key='key_witness'`).Scan(&witnessMarker); {
	case err == nil:
		return nil, fmt.Errorf("master key unavailable (keyring: %v) and no auth.key, but the auth database already has a key; refusing to create a new one", keyringErr)
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	newKey := make([]byte, masterKeyLen)
	if _, err := rand.Read(newKey); err != nil {
		return nil, err
	}
	encodedKey := hex.EncodeToString(newKey)
	if errors.Is(keyringErr, keyring.ErrNotFound) && keyringSet(encodedKey) == nil {
		return newKey, nil
	}
	keyFile, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	_, writeErr := keyFile.WriteString(encodedKey)
	if closeErr := keyFile.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		os.Remove(path)
		return nil, writeErr
	}
	keyFromFile.Store(true)
	return newKey, nil
}

func seal(key, plain []byte) []byte {
	aead := newGCM(key)
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return aead.Seal(nonce, nonce, plain, nil)
}

func open(key, boxed []byte) ([]byte, error) {
	aead := newGCM(key)
	if len(boxed) < aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	return aead.Open(nil, boxed[:aead.NonceSize()], boxed[aead.NonceSize():], nil)
}

func newGCM(key []byte) cipher.AEAD {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return aead
}

func rowSigKey(key []byte) []byte {
	derivedKey, err := hkdf.Key(sha256.New, key, nil, "gld-auth-integrity", 32)
	if err != nil {
		panic(err)
	}
	return derivedKey
}

// Fields are length-prefixed so ("ab","c") and ("a","bc") sign differently.
func rowSig(key []byte, table, id string, fields ...string) []byte {
	mac := hmac.New(sha256.New, rowSigKey(key))
	for _, field := range append([]string{table, id}, fields...) {
		binary.Write(mac, binary.BigEndian, uint64(len(field)))
		mac.Write([]byte(field))
	}
	return mac.Sum(nil)
}

func rowSigOK(key []byte, table, id string, sig []byte, fields ...string) bool {
	return hmac.Equal(sig, rowSig(key, table, id, fields...))
}

func checkMasterKey(db *authDB, key []byte) error {
	var witness []byte
	err := db.QueryRow(`SELECT value FROM meta WHERE key='key_witness'`).Scan(&witness)
	if errors.Is(err, sql.ErrNoRows) {
		var rowCount int
		if countErr := db.QueryRow(`SELECT (SELECT COUNT(*) FROM users)+(SELECT COUNT(*) FROM totp)+(SELECT COUNT(*) FROM webauthn_creds)+(SELECT COUNT(*) FROM recovery_codes)+(SELECT COUNT(*) FROM sessions)+(SELECT COUNT(*) FROM home_acl)+(SELECT COUNT(*) FROM meta WHERE key='accounts')`).Scan(&rowCount); countErr != nil {
			return countErr
		}
		if rowCount > 0 {
			return errors.New("key witness missing on a populated auth database; refusing to start")
		}
		// L'état des comptes naît avec le témoin : par la suite, son absence ne peut être qu'une suppression.
		witness = seal(key, []byte(witnessPlain))
		transaction, beginErr := db.Begin()
		if beginErr != nil {
			return beginErr
		}
		defer transaction.Rollback()
		if _, insertWitnessErr := transaction.Exec(`INSERT INTO meta(key,value) VALUES('key_witness',?)`, witness); insertWitnessErr != nil {
			return insertWitnessErr
		}
		if _, insertAccountsErr := transaction.Exec(`INSERT INTO meta(key,value) VALUES(?,?)`, accountsKey, accountsValue(key, witness, "off")); insertAccountsErr != nil {
			return insertAccountsErr
		}
		return transaction.Commit()
	}
	if err != nil {
		return err
	}
	if plain, openErr := open(key, witness); openErr != nil || string(plain) != witnessPlain {
		return fmt.Errorf("master key does not match the auth database; refusing to start")
	}
	return nil
}
