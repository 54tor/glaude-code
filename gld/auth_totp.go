package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	totpPeriod      = 30
	recoveryCount   = 10
	recoveryAlpha   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	recoveryCodeLen = 10
)

// SHA1 et 6 chiffres : les seuls réglages que toutes les applications d'authentification acceptent.
var totpOpts = totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

var (
	errTOTPActive = terr("srv.auth.totpDejaActif")
	errBadCode    = terr("srv.auth.codeInvalide")
	errNoTOTP     = errors.New("no totp")
)

type totpRow struct {
	uid       int64
	enc       []byte
	nonce     []byte
	confirmed sql.NullInt64
	lastStep  int64
	sig       []byte
}

// seal n'a pas de données associées : le chiffré et le nonce entrent dans la signature pour lier la ligne à son compte.
func (s *authStore) totpSig(r *totpRow) []byte {
	conf := ""
	if r.confirmed.Valid {
		conf = strconv.FormatInt(r.confirmed.Int64, 10)
	}
	return rowSig(s.key, "totp", strconv.FormatInt(r.uid, 10), string(r.nonce), string(r.enc), conf, strconv.FormatInt(r.lastStep, 10))
}

// loadTOTP ne journalise pas : l'appelant passe l'erreur par guard (ou inTx).
func (s *authStore) loadTOTP(q querier, userID int64) (*totpRow, error) {
	r := &totpRow{uid: userID}
	err := q.QueryRow(`SELECT secret_enc,nonce,confirmed_at,last_step,sig FROM totp WHERE user_id=?`, userID).
		Scan(&r.enc, &r.nonce, &r.confirmed, &r.lastStep, &r.sig)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoTOTP
	}
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(r.sig, s.totpSig(r)) {
		return nil, &tamperErr{userID, "totp"}
	}
	return r, nil
}

// matchStep essaie les pas −1, 0, +1 autour de at et ne retient qu'un pas postérieur à lastStep.
func (s *authStore) matchStep(r *totpRow, code string, at time.Time) (int64, error) {
	if len(code) != 6 {
		return 0, errBadCode
	}
	secret, err := open(s.key, append(append([]byte{}, r.nonce...), r.enc...))
	if err != nil {
		return 0, s.guard(&tamperErr{r.uid, "totp"})
	}
	cur := at.Unix() / totpPeriod
	for step := cur - 1; step <= cur+1; step++ {
		if step <= r.lastStep {
			continue
		}
		want, err := totp.GenerateCodeCustom(string(secret), time.Unix(step*totpPeriod, 0), totpOpts)
		if err != nil {
			return 0, err
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, nil
		}
	}
	return 0, errBadCode
}

// TOTPBegin remplace un secret en attente, jamais un secret confirmé : changer d'application passe par ResetMFA.
func (s *authStore) TOTPBegin(userID int64, account string) (secret, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "gld", AccountName: account, Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return "", "", err
	}
	boxed := seal(s.key, []byte(key.Secret()))
	n := newGCM(s.key).NonceSize()
	r := &totpRow{uid: userID, nonce: boxed[:n], enc: boxed[n:]}
	err = s.inTx(func(tx *sql.Tx) error {
		if _, err := s.userByID(tx, userID); err != nil {
			return err
		}
		old, err := s.loadTOTP(tx, userID)
		if errors.Is(err, errNoTOTP) {
			_, err = tx.Exec(`INSERT INTO totp(user_id,secret_enc,nonce,confirmed_at,last_step,sig) VALUES(?,?,?,NULL,0,?)`, userID, r.enc, r.nonce, s.totpSig(r))
			return err
		}
		if err != nil {
			return err
		}
		if old.confirmed.Valid {
			return errTOTPActive
		}
		res, err := tx.Exec(`UPDATE totp SET secret_enc=?, nonce=?, last_step=0, sig=? WHERE user_id=? AND confirmed_at IS NULL AND sig=?`,
			r.enc, r.nonce, s.totpSig(r), userID, old.sig)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return errors.New("totp row changed concurrently")
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

func (s *authStore) TOTPConfirm(userID int64, code string) error {
	r, err := s.loadTOTP(s.db, userID)
	err = s.guard(err)
	if errors.Is(err, errNoTOTP) {
		return errBadCode
	}
	if err != nil {
		return err
	}
	if r.confirmed.Valid {
		return errTOTPActive
	}
	now := authNow()
	step, err := s.matchStep(r, code, now)
	if err != nil {
		return err
	}
	old := r.sig
	r.confirmed, r.lastStep = sql.NullInt64{Int64: now.Unix(), Valid: true}, step
	res, err := s.db.Exec(`UPDATE totp SET confirmed_at=?, last_step=?, sig=? WHERE user_id=? AND sig=? AND confirmed_at IS NULL`,
		r.confirmed.Int64, step, s.totpSig(r), userID, old)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return errBadCode
	}
	s.event(userID, "totp", "enrolled")
	return nil
}

// TOTPVerify avance last_step par un UPDATE conditionnel : de deux vérifications concurrentes du même code, une seule gagne.
func (s *authStore) TOTPVerify(userID int64, code string, at time.Time) bool {
	r, err := s.loadTOTP(s.db, userID)
	err = s.guard(err)
	if err != nil || !r.confirmed.Valid {
		if err != nil && !errors.Is(err, errNoTOTP) && !errors.Is(err, errTamper) {
			journal("auth: totp verify: %v", err)
		}
		return false
	}
	step, err := s.matchStep(r, code, at)
	if err != nil {
		if !errors.Is(err, errBadCode) && !errors.Is(err, errTamper) {
			journal("auth: totp verify: %v", err)
		}
		return false
	}
	old := r.sig
	r.lastStep = step
	res, err := s.db.Exec(`UPDATE totp SET last_step=?, sig=? WHERE user_id=? AND last_step<? AND sig=?`, step, s.totpSig(r), userID, step, old)
	if err != nil {
		journal("auth: totp verify: %v", err)
		return false
	}
	n, err := res.RowsAffected()
	if err != nil {
		journal("auth: totp verify: %v", err)
		return false
	}
	return n == 1
}

// TOTPRemove refuse de retirer un TOTP confirmé s'il ne reste aucune clé WebAuthn utilisable ; un secret en attente part toujours.
func (s *authStore) TOTPRemove(userID int64) error {
	confirmed := false
	err := s.inTx(func(tx *sql.Tx) error {
		if _, err := s.userByID(tx, userID); err != nil {
			return err
		}
		r, err := s.loadTOTP(tx, userID)
		if err != nil {
			return err
		}
		confirmed = r.confirmed.Valid
		if confirmed {
			list, err := s.loadCreds(tx, userID)
			if err != nil {
				return err
			}
			left := 0
			for _, c := range list {
				if !c.bad {
					left++
				}
			}
			if left == 0 {
				return errLastFactor
			}
		}
		_, err = tx.Exec(`DELETE FROM totp WHERE user_id=? AND sig=?`, userID, r.sig)
		return err
	})
	if err != nil {
		return err
	}
	if confirmed {
		s.event(userID, "totp", "removed")
	}
	return nil
}

func (s *authStore) recoverySig(userID int64, hash string, used sql.NullInt64) []byte {
	u := ""
	if used.Valid {
		u = strconv.FormatInt(used.Int64, 10)
	}
	return rowSig(s.key, "recovery_codes", strconv.FormatInt(userID, 10), hash, u)
}

func normRecovery(c string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(c)))
}

// NewRecoveryCodes remplace le jeu précédent ; les codes en clair ne sont rendus qu'ici.
func (s *authStore) NewRecoveryCodes(userID int64) ([]string, error) {
	codes := make([]string, 0, recoveryCount)
	hashes := make([]string, 0, recoveryCount)
	seen := map[string]bool{}
	buf := make([]byte, recoveryCodeLen)
	for len(codes) < recoveryCount {
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		for i, b := range buf {
			// 32 symboles : b&31 reste uniforme.
			buf[i] = recoveryAlpha[b&31]
		}
		c := string(buf)
		if seen[c] {
			continue
		}
		seen[c] = true
		h, err := hashPassword(c)
		if err != nil {
			return nil, err
		}
		codes = append(codes, c[:5]+"-"+c[5:])
		hashes = append(hashes, h)
	}
	err := s.inTx(func(tx *sql.Tx) error {
		if _, err := s.userByID(tx, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id=?`, userID); err != nil {
			return err
		}
		for _, h := range hashes {
			if _, err := tx.Exec(`INSERT INTO recovery_codes(user_id,hash,used_at,sig) VALUES(?,?,NULL,?)`, userID, h, s.recoverySig(userID, h, sql.NullInt64{})); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.event(userID, "recovery", "generated")
	return codes, nil
}

// UseRecoveryCode hache hors transaction (argon2 est lent et la connexion est unique),
// puis marque le code par un UPDATE conditionnel sur used_at IS NULL.
func (s *authStore) UseRecoveryCode(userID int64, code string) bool {
	c := normRecovery(code)
	if len(c) != recoveryCodeLen {
		return false
	}
	type rc struct {
		hash string
		sig  []byte
	}
	rows, err := s.db.Query(`SELECT hash,sig FROM recovery_codes WHERE user_id=? AND used_at IS NULL`, userID)
	if err != nil {
		journal("auth: recovery: %v", err)
		return false
	}
	var list []rc
	for rows.Next() {
		var r rc
		if err := rows.Scan(&r.hash, &r.sig); err != nil {
			rows.Close()
			journal("auth: recovery: %v", err)
			return false
		}
		list = append(list, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		journal("auth: recovery: %v", err)
		return false
	}
	for _, r := range list {
		if !hmac.Equal(r.sig, s.recoverySig(userID, r.hash, sql.NullInt64{})) {
			s.guard(&tamperErr{userID, "recovery_codes"})
			return false
		}
	}
	for _, r := range list {
		if !checkPassword(r.hash, c) {
			continue
		}
		used := sql.NullInt64{Int64: authNow().Unix(), Valid: true}
		res, err := s.db.Exec(`UPDATE recovery_codes SET used_at=?, sig=? WHERE user_id=? AND hash=? AND used_at IS NULL AND sig=?`,
			used.Int64, s.recoverySig(userID, r.hash, used), userID, r.hash, r.sig)
		if err != nil {
			journal("auth: recovery: %v", err)
			return false
		}
		n, err := res.RowsAffected()
		if err != nil {
			journal("auth: recovery: %v", err)
			return false
		}
		if n == 1 {
			s.event(userID, "recovery", "used")
		}
		return n == 1
	}
	return false
}
