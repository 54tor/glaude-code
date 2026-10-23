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

//go:embed auth_commonpw.txt
var commonPwRaw []byte

var commonPw = sync.OnceValue(func() map[string]struct{} {
	m := map[string]struct{}{}
	for _, l := range bytes.Split(commonPwRaw, []byte("\n")) {
		if s := strings.TrimSpace(string(l)); s != "" {
			m[strings.ToLower(s)] = struct{}{}
		}
	}
	return m
})

// argonSem borne les calculs argon2 simultanés (64 Mio chacun) : une rafale de requêtes ne fait pas exploser la mémoire.
var (
	argonSem   = make(chan struct{}, 4)
	argonIDKey = argon2.IDKey
)

func argonKey(pw, salt []byte, t, m uint32, p uint8, l uint32) []byte {
	argonSem <- struct{}{}
	defer func() { <-argonSem }()
	return argonIDKey(pw, salt, t, m, p, l)
}

func hashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argonKey([]byte(pw), salt, argonTime, argonMemKiB, argonThreads, argonKeyLen)
	b := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		argonMemKiB, argonTime, argonThreads, b.EncodeToString(salt), b.EncodeToString(key)), nil
}

func checkPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	if parts[2] != "v=19" {
		return false
	}
	kv := strings.Split(parts[3], ",")
	if len(kv) != 3 {
		return false
	}
	var v [3]uint64
	for i, k := range []string{"m=", "t=", "p="} {
		if !strings.HasPrefix(kv[i], k) {
			return false
		}
		n, err := strconv.ParseUint(kv[i][len(k):], 10, 32)
		if err != nil {
			return false
		}
		v[i] = n
	}
	m, t, p := uint32(v[0]), uint32(v[1]), v[2]
	if m == 0 || m > 1<<20 || t == 0 || t > 10 || p == 0 || p > 16 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(salt) < 8 || len(salt) > 64 || len(want) < 16 || len(want) > 64 {
		return false
	}
	got := argonKey([]byte(pw), salt, t, m, uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyCheck dépense le même travail qu'une vérification, pour qu'un nom inconnu ne se distingue pas par le temps.
func dummyCheck() {
	argonKey([]byte("dummy"), make([]byte, argonSaltLen), argonTime, argonMemKiB, argonThreads, argonKeyLen)
}

func validPassword(pw string) error {
	switch {
	case len(pw) > 1024:
		return errPasswordLong
	case strings.IndexByte(pw, 0) >= 0:
		return errPasswordNUL
	case utf8.RuneCountInString(pw) < 12:
		return errPasswordShort
	}
	if _, bad := commonPw()[strings.ToLower(pw)]; bad {
		return errPasswordCommon
	}
	return nil
}

func normName(s string) string {
	return strings.ToLower(strings.TrimSpace(norm.NFKC.String(s)))
}
