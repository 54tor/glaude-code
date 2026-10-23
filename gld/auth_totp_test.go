package main

import (
	"bytes"
	"encoding/base32"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func codeAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	c, err := totp.GenerateCodeCustom(secret, at, totpOpts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// enrolled renvoie un compte au TOTP confirmé et l'instant de la confirmation.
func enrolled(t *testing.T) (*authStore, int64, string, time.Time) {
	t.Helper()
	now := testClock(t)
	s, id := sessUser(t)
	secret, uri, err := s.TOTPBegin(id, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uri, "otpauth://totp/gld:alice?") || !strings.Contains(uri, "issuer=gld") {
		t.Fatalf("uri = %q", uri)
	}
	if s.TOTPVerify(id, codeAt(t, secret, *now), *now) {
		t.Fatal("pending secret accepted before confirmation")
	}
	if err := s.TOTPConfirm(id, "000000x"); err == nil {
		t.Fatal("bad code confirmed")
	}
	if err := s.TOTPConfirm(id, codeAt(t, secret, *now)); err != nil {
		t.Fatal(err)
	}
	return s, id, secret, *now
}

func TestTOTPDrift(t *testing.T) {
	s, id, secret, t0 := enrolled(t)
	at := t0.Add(10 * time.Minute)
	for _, d := range []int{-2, 2} {
		if s.TOTPVerify(id, codeAt(t, secret, at.Add(time.Duration(d)*30*time.Second)), at) {
			t.Fatalf("step %+d accepted", d)
		}
	}
	for _, d := range []int{-1, 1} {
		if !s.TOTPVerify(id, codeAt(t, secret, at.Add(time.Duration(d)*30*time.Second)), at) {
			t.Fatalf("step %+d refused", d)
		}
	}
}

func TestTOTPReplay(t *testing.T) {
	s, id, secret, t0 := enrolled(t)
	if s.TOTPVerify(id, codeAt(t, secret, t0), t0) {
		t.Fatal("confirmation code replayed")
	}
	at := t0.Add(time.Minute)
	c := codeAt(t, secret, at)
	if !s.TOTPVerify(id, c, at) {
		t.Fatal("fresh code refused")
	}
	if s.TOTPVerify(id, c, at) {
		t.Fatal("replay accepted")
	}
	if s.TOTPVerify(id, codeAt(t, secret, at.Add(-30*time.Second)), at) {
		t.Fatal("older step accepted after a newer one")
	}
}

func TestTOTPConcurrentReplay(t *testing.T) {
	s, id, secret, t0 := enrolled(t)
	at := t0.Add(time.Minute)
	c := codeAt(t, secret, at)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.TOTPVerify(id, c, at) {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("accepted %d times", ok)
	}
}

func TestTOTPSecretEncrypted(t *testing.T) {
	s, id, secret, _ := enrolled(t)
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var enc, nonce []byte
	if err := s.db.QueryRow(`SELECT secret_enc, nonce FROM totp WHERE user_id=?`, id).Scan(&enc, &nonce); err != nil {
		t.Fatal(err)
	}
	for _, col := range [][]byte{enc, nonce} {
		if bytes.Contains(col, []byte(secret)) || bytes.Contains(col, raw) {
			t.Fatal("plaintext secret stored")
		}
	}
}

func TestTOTPTamper(t *testing.T) {
	s, id, secret, t0 := enrolled(t)
	if _, err := s.db.Exec(`UPDATE totp SET last_step=0 WHERE user_id=?`, id); err != nil {
		t.Fatal(err)
	}
	before := tamperEvents(t, s)
	if s.TOTPVerify(id, codeAt(t, secret, t0.Add(time.Minute)), t0.Add(time.Minute)) {
		t.Fatal("tampered row verified")
	}
	if tamperEvents(t, s) != before+1 {
		t.Fatal("tamper not logged")
	}
	if _, err := s.db.Exec(`UPDATE totp SET confirmed_at=NULL WHERE user_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.TOTPConfirm(id, codeAt(t, secret, t0)); !errors.Is(err, errTamper) {
		t.Fatalf("confirm on tampered row: %v", err)
	}
}

func TestTOTPBeginKeepsConfirmed(t *testing.T) {
	s, id, secret, t0 := enrolled(t)
	if _, _, err := s.TOTPBegin(id, "alice"); err == nil {
		t.Fatal("confirmed secret replaced")
	}
	at := t0.Add(time.Minute)
	if !s.TOTPVerify(id, codeAt(t, secret, at), at) {
		t.Fatal("confirmed secret lost")
	}
}

func TestTOTPBeginReplacesPending(t *testing.T) {
	now := testClock(t)
	s, id := sessUser(t)
	first, _, err := s.TOTPBegin(id, "alice")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.TOTPBegin(id, "alice")
	if err != nil || first == second {
		t.Fatalf("second begin: %v", err)
	}
	if err := s.TOTPConfirm(id, codeAt(t, first, *now)); err == nil {
		t.Fatal("replaced pending secret confirmed")
	}
	if err := s.TOTPConfirm(id, codeAt(t, second, *now)); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryOnce(t *testing.T) {
	s, id := sessUser(t)
	codes, err := s.NewRecoveryCodes(id)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^[ABCDEFGHJKLMNPQRSTUVWXYZ23456789]{5}-[ABCDEFGHJKLMNPQRSTUVWXYZ23456789]{5}$`)
	seen := map[string]bool{}
	for _, c := range codes {
		if !re.MatchString(c) || seen[c] {
			t.Fatalf("bad or duplicate code %q", c)
		}
		seen[c] = true
	}
	if len(seen) != 10 {
		t.Fatalf("%d codes", len(seen))
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM recovery_codes WHERE user_id=? AND hash LIKE '$argon2id$%'`, id).Scan(&n); err != nil || n != 10 {
		t.Fatalf("stored %d hashes, %v", n, err)
	}
	if !s.UseRecoveryCode(id, codes[3]) {
		t.Fatal("first use refused")
	}
	if s.UseRecoveryCode(id, codes[3]) {
		t.Fatal("second use accepted")
	}
	if s.UseRecoveryCode(id, "AAAAA-AAAAA") {
		t.Fatal("unknown code accepted")
	}
	again, err := s.NewRecoveryCodes(id)
	if err != nil {
		t.Fatal(err)
	}
	if s.UseRecoveryCode(id, codes[4]) || !s.UseRecoveryCode(id, again[0]) {
		t.Fatal("old set still valid or new set refused")
	}
}

func TestRecoveryNormalised(t *testing.T) {
	s, id := sessUser(t)
	codes, err := s.NewRecoveryCodes(id)
	if err != nil {
		t.Fatal(err)
	}
	c := strings.ToLower(strings.ReplaceAll(codes[0], "-", " "))
	if !s.UseRecoveryCode(id, " "+c+" ") {
		t.Fatal("normalised code refused")
	}
}

func TestRecoveryTamper(t *testing.T) {
	s, id := sessUser(t)
	codes, err := s.NewRecoveryCodes(id)
	if err != nil {
		t.Fatal(err)
	}
	if !s.UseRecoveryCode(id, codes[0]) {
		t.Fatal("first use refused")
	}
	if _, err := s.db.Exec(`UPDATE recovery_codes SET used_at=NULL WHERE user_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if s.UseRecoveryCode(id, codes[0]) || tamperEvents(t, s) == 0 {
		t.Fatal("tampered row accepted or not logged")
	}
}

func TestTOTPBeginTamperedPending(t *testing.T) {
	s, id, _, _ := enrolled(t)
	var before []byte
	if err := s.db.QueryRow(`SELECT secret_enc FROM totp WHERE user_id=?`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE totp SET confirmed_at=NULL WHERE user_id=?`, id); err != nil {
		t.Fatal(err)
	}
	n := tamperEvents(t, s)
	if _, _, err := s.TOTPBegin(id, "alice"); !errors.Is(err, errTamper) {
		t.Fatalf("begin over tampered row: %v", err)
	}
	var after []byte
	if err := s.db.QueryRow(`SELECT secret_enc FROM totp WHERE user_id=?`, id).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || tamperEvents(t, s) != n+1 {
		t.Fatal("tampered row replaced or tamper not logged")
	}
}
