package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

var (
	errPasswordShort  = errors.New("password too short")
	errPasswordLong   = errors.New("password too long")
	errPasswordNUL    = errors.New("password contains NUL")
	errPasswordCommon = errors.New("password too common")
)

// Valeurs de départ ; les hachages existants portent leurs propres paramètres.
var (
	argonMemKiB  uint32 = 64 * 1024
	argonTime    uint32 = 3
	argonThreads uint8  = 2
)

const (
	argonKeyLen  = 32
	argonSaltLen = 16
)

// Bornes acceptées pour un hachage stocké : au-delà, la ligne est rejetée avant tout calcul argon2.
const (
	argonMaxMemKiB  = 1 << 20
	argonMaxTime    = 10
	argonMaxThreads = 16
	argonMinSaltLen = 8
	argonMaxSaltLen = 64
	argonMinKeyLen  = 16
	argonMaxKeyLen  = 64
)

const (
	passwordMaxBytes = 1024
	passwordMinRunes = 12
)

//go:embed auth_commonpw.txt
var commonPwRaw []byte

var commonPw = sync.OnceValue(func() map[string]struct{} {
	commonPasswords := map[string]struct{}{}
	for _, rawLine := range bytes.Split(commonPwRaw, []byte("\n")) {
		if candidate := strings.TrimSpace(string(rawLine)); candidate != "" {
			commonPasswords[strings.ToLower(candidate)] = struct{}{}
		}
	}
	return commonPasswords
})

// argonSem borne les calculs argon2 simultanés (64 Mio chacun) : une rafale de requêtes ne fait pas exploser la mémoire.
var (
	argonSem   = make(chan struct{}, 4)
	argonIDKey = argon2.IDKey
)

func argonKey(password, salt []byte, iterations, memoryKiB uint32, threads uint8, keyLength uint32) []byte {
	argonSem <- struct{}{}
	defer func() { <-argonSem }()
	return argonIDKey(password, salt, iterations, memoryKiB, threads, keyLength)
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argonKey([]byte(password), salt, argonTime, argonMemKiB, argonThreads, argonKeyLen)
	encoding := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		argonMemKiB, argonTime, argonThreads, encoding.EncodeToString(salt), encoding.EncodeToString(key)), nil
}

func checkPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	if parts[2] != "v=19" {
		return false
	}
	parameters := strings.Split(parts[3], ",")
	if len(parameters) != 3 {
		return false
	}
	var parsedParams [3]uint64
	for paramIndex, prefix := range []string{"m=", "t=", "p="} {
		if !strings.HasPrefix(parameters[paramIndex], prefix) {
			return false
		}
		parsedValue, err := strconv.ParseUint(parameters[paramIndex][len(prefix):], 10, 32)
		if err != nil {
			return false
		}
		parsedParams[paramIndex] = parsedValue
	}
	memoryKiB, iterations, threads := uint32(parsedParams[0]), uint32(parsedParams[1]), parsedParams[2] // #nosec G115 -- valeur bornée en amont
	if memoryKiB == 0 || memoryKiB > argonMaxMemKiB || iterations == 0 || iterations > argonMaxTime || threads == 0 || threads > argonMaxThreads {
		return false
	}
	salt, saltErr := base64.RawStdEncoding.DecodeString(parts[4])
	expectedKey, keyErr := base64.RawStdEncoding.DecodeString(parts[5])
	if saltErr != nil || keyErr != nil || len(salt) < argonMinSaltLen || len(salt) > argonMaxSaltLen || len(expectedKey) < argonMinKeyLen || len(expectedKey) > argonMaxKeyLen {
		return false
	}
	computedKey := argonKey([]byte(password), salt, iterations, memoryKiB, uint8(threads), uint32(len(expectedKey))) // #nosec G115 -- valeur bornée en amont
	return subtle.ConstantTimeCompare(computedKey, expectedKey) == 1
}

// dummyCheck dépense le même travail qu'une vérification, pour qu'un nom inconnu ne se distingue pas par le temps.
func dummyCheck() {
	argonKey([]byte("dummy"), make([]byte, argonSaltLen), argonTime, argonMemKiB, argonThreads, argonKeyLen)
}

func validPassword(password string) error {
	switch {
	case len(password) > passwordMaxBytes:
		return errPasswordLong
	case strings.IndexByte(password, 0) >= 0:
		return errPasswordNUL
	case utf8.RuneCountInString(password) < passwordMinRunes:
		return errPasswordShort
	}
	if _, isCommon := commonPw()[strings.ToLower(password)]; isCommon {
		return errPasswordCommon
	}
	return nil
}

func normName(name string) string {
	return strings.ToLower(strings.TrimSpace(norm.NFKC.String(name)))
}
