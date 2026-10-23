package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
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

func hashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemKiB, argonThreads, argonKeyLen)
	b := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		argonMemKiB, argonTime, argonThreads, b.EncodeToString(salt), b.EncodeToString(key)), nil
}

func checkPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var ver int
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &ver); err != nil || ver != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || t == 0 || p == 0 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyCheck dépense le même travail qu'une vérification, pour qu'un nom inconnu ne se distingue pas par le temps.
func dummyCheck() {
	argon2.IDKey([]byte("dummy"), make([]byte, argonSaltLen), argonTime, argonMemKiB, argonThreads, argonKeyLen)
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
