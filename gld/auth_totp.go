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
func (store *authStore) totpSig(totpRecord *totpRow) []byte {
	confirmedText := ""
	if totpRecord.confirmed.Valid {
		confirmedText = strconv.FormatInt(totpRecord.confirmed.Int64, 10)
	}
	return rowSig(store.key, "totp", strconv.FormatInt(totpRecord.uid, 10), string(totpRecord.nonce), string(totpRecord.enc), confirmedText, strconv.FormatInt(totpRecord.lastStep, 10))
}

// loadTOTP ne journalise pas : l'appelant passe l'erreur par guard (ou inTx).
func (store *authStore) loadTOTP(queryRunner querier, userID int64) (*totpRow, error) {
	totpRecord := &totpRow{uid: userID}
	err := queryRunner.QueryRow(`SELECT secret_enc,nonce,confirmed_at,last_step,sig FROM totp WHERE user_id=?`, userID).
		Scan(&totpRecord.enc, &totpRecord.nonce, &totpRecord.confirmed, &totpRecord.lastStep, &totpRecord.sig)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoTOTP
	}
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(totpRecord.sig, store.totpSig(totpRecord)) {
		return nil, &tamperErr{userID, "totp"}
	}
	return totpRecord, nil
}

// matchStep essaie les pas −1, 0, +1 autour de at et ne retient qu'un pas postérieur à lastStep.
func (store *authStore) matchStep(totpRecord *totpRow, code string, checkedAt time.Time) (int64, error) {
	if len(code) != 6 {
		return 0, errBadCode
	}
	secret, err := open(store.key, append(append([]byte{}, totpRecord.nonce...), totpRecord.enc...))
	if err != nil {
		return 0, store.guard(&tamperErr{totpRecord.uid, "totp"})
	}
	currentStep := checkedAt.Unix() / totpPeriod
	for step := currentStep - 1; step <= currentStep+1; step++ {
		if step <= totpRecord.lastStep {
			continue
		}
		expectedCode, generateErr := totp.GenerateCodeCustom(string(secret), time.Unix(step*totpPeriod, 0), totpOpts)
		if generateErr != nil {
			return 0, generateErr
		}
		if subtle.ConstantTimeCompare([]byte(expectedCode), []byte(code)) == 1 {
			return step, nil
		}
	}
	return 0, errBadCode
}

// TOTPBegin remplace un secret en attente, jamais un secret confirmé : changer d'application passe par ResetMFA.
func (store *authStore) TOTPBegin(userID int64, account string) (secret, uri string, err error) {
	generatedKey, err := totp.Generate(totp.GenerateOpts{Issuer: "gld", AccountName: account, Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return "", "", err
	}
	boxed := seal(store.key, []byte(generatedKey.Secret()))
	nonceSize := newGCM(store.key).NonceSize()
	totpRecord := &totpRow{uid: userID, nonce: boxed[:nonceSize], enc: boxed[nonceSize:]}
	err = store.inTx(func(transaction *sql.Tx) error {
		if _, lookupErr := store.userByID(transaction, userID); lookupErr != nil {
			return lookupErr
		}
		existingRow, txErr := store.loadTOTP(transaction, userID)
		if errors.Is(txErr, errNoTOTP) {
			_, txErr = transaction.Exec(`INSERT INTO totp(user_id,secret_enc,nonce,confirmed_at,last_step,sig) VALUES(?,?,?,NULL,0,?)`, userID, totpRecord.enc, totpRecord.nonce, store.totpSig(totpRecord))
			return txErr
		}
		if txErr != nil {
			return txErr
		}
		if existingRow.confirmed.Valid {
			return errTOTPActive
		}
		result, txErr := transaction.Exec(`UPDATE totp SET secret_enc=?, nonce=?, last_step=0, sig=? WHERE user_id=? AND confirmed_at IS NULL AND sig=?`,
			totpRecord.enc, totpRecord.nonce, store.totpSig(totpRecord), userID, existingRow.sig)
		if txErr != nil {
			return txErr
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
			return affectedErr
		} else if affected == 0 {
			return errors.New("totp row changed concurrently")
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return generatedKey.Secret(), generatedKey.URL(), nil
}

func (store *authStore) TOTPConfirm(userID int64, code string) error {
	totpRecord, err := store.loadTOTP(store.db, userID)
	err = store.guard(err)
	if errors.Is(err, errNoTOTP) {
		return errBadCode
	}
	if err != nil {
		return err
	}
	if totpRecord.confirmed.Valid {
		return errTOTPActive
	}
	now := authNow()
	step, err := store.matchStep(totpRecord, code, now)
	if err != nil {
		return err
	}
	previousSig := totpRecord.sig
	totpRecord.confirmed, totpRecord.lastStep = sql.NullInt64{Int64: now.Unix(), Valid: true}, step
	err = store.inTx(func(transaction *sql.Tx) error {
		user, txErr := store.userByID(transaction, userID)
		if txErr != nil {
			return txErr
		}
		result, txErr := transaction.Exec(`UPDATE totp SET confirmed_at=?, last_step=?, sig=? WHERE user_id=? AND sig=? AND confirmed_at IS NULL`,
			totpRecord.confirmed.Int64, step, store.totpSig(totpRecord), userID, previousSig)
		if txErr != nil {
			return txErr
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
			return affectedErr
		} else if affected == 0 {
			return errBadCode
		}
		return store.markEnrolled(transaction, user)
	})
	if err != nil {
		return err
	}
	store.event(userID, "totp", "enrolled")
	return nil
}

// TOTPVerify avance last_step par un UPDATE conditionnel : de deux vérifications concurrentes du même code, une seule gagne.
func (store *authStore) TOTPVerify(userID int64, code string, checkedAt time.Time) bool {
	totpRecord, err := store.loadTOTP(store.db, userID)
	err = store.guard(err)
	if err != nil || !totpRecord.confirmed.Valid {
		if err != nil && !errors.Is(err, errNoTOTP) && !errors.Is(err, errTamper) {
			journal("auth: totp verify: %v", err)
		}
		return false
	}
	step, err := store.matchStep(totpRecord, code, checkedAt)
	if err != nil {
		if !errors.Is(err, errBadCode) && !errors.Is(err, errTamper) {
			journal("auth: totp verify: %v", err)
		}
		return false
	}
	previousSig := totpRecord.sig
	totpRecord.lastStep = step
	result, err := store.db.Exec(`UPDATE totp SET last_step=?, sig=? WHERE user_id=? AND last_step<? AND sig=?`, step, store.totpSig(totpRecord), userID, step, previousSig)
	if err != nil {
		journal("auth: totp verify: %v", err)
		return false
	}
	affected, err := result.RowsAffected()
	if err != nil {
		journal("auth: totp verify: %v", err)
		return false
	}
	return affected == 1
}

// TOTPRemove refuse de retirer un TOTP confirmé s'il ne reste aucune clé WebAuthn utilisable ; un secret en attente part toujours.
func (store *authStore) TOTPRemove(userID int64) error {
	confirmed := false
	err := store.inTx(func(transaction *sql.Tx) error {
		if _, lookupErr := store.userByID(transaction, userID); lookupErr != nil {
			return lookupErr
		}
		totpRecord, txErr := store.loadTOTP(transaction, userID)
		if txErr != nil {
			return txErr
		}
		confirmed = totpRecord.confirmed.Valid
		if confirmed {
			creds, credsErr := store.loadCreds(transaction, userID)
			if credsErr != nil {
				return credsErr
			}
			usableCount := 0
			for _, cred := range creds {
				if !cred.bad {
					usableCount++
				}
			}
			if usableCount == 0 {
				return errLastFactor
			}
		}
		result, txErr := transaction.Exec(`DELETE FROM totp WHERE user_id=? AND sig=?`, userID, totpRecord.sig)
		if txErr != nil {
			return txErr
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
			return affectedErr
		} else if affected == 0 {
			return errNoTOTP
		}
		return nil
	})
	if err != nil {
		return err
	}
	if confirmed {
		store.event(userID, "totp", "removed")
	}
	return nil
}

func (store *authStore) recoverySig(userID int64, hash string, used sql.NullInt64) []byte {
	usedText := ""
	if used.Valid {
		usedText = strconv.FormatInt(used.Int64, 10)
	}
	return rowSig(store.key, "recovery_codes", strconv.FormatInt(userID, 10), hash, usedText)
}

func normRecovery(code string) string {
	return strings.Map(func(character rune) rune {
		if character == ' ' || character == '-' {
			return -1
		}
		return character
	}, strings.ToUpper(strings.TrimSpace(code)))
}

// NewRecoveryCodes remplace le jeu précédent ; les codes en clair ne sont rendus qu'ici.
func (store *authStore) NewRecoveryCodes(userID int64) ([]string, error) {
	codes, hashes, err := genRecoveryCodes()
	if err != nil {
		return nil, err
	}
	return codes, store.storeRecoveryCodes(userID, hashes)
}

// genRecoveryCodes fait le travail argon2 hors de toute section verrouillée ; storeRecoveryCodes écrit le jeu.
func genRecoveryCodes() (codes, hashes []string, err error) {
	codes = make([]string, 0, recoveryCount)
	hashes = make([]string, 0, recoveryCount)
	seen := map[string]bool{}
	randomBytes := make([]byte, recoveryCodeLen)
	for len(codes) < recoveryCount {
		if _, randErr := rand.Read(randomBytes); randErr != nil {
			return nil, nil, randErr
		}
		for byteIndex, randomByte := range randomBytes {
			// 32 symboles : b&31 reste uniforme.
			randomBytes[byteIndex] = recoveryAlpha[randomByte&31]
		}
		code := string(randomBytes)
		if seen[code] {
			continue
		}
		seen[code] = true
		hash, hashErr := hashPassword(code)
		if hashErr != nil {
			return nil, nil, hashErr
		}
		codes = append(codes, code[:5]+"-"+code[5:])
		hashes = append(hashes, hash)
	}
	return codes, hashes, nil
}

func (store *authStore) storeRecoveryCodes(userID int64, hashes []string) error {
	err := store.inTx(func(transaction *sql.Tx) error {
		if _, lookupErr := store.userByID(transaction, userID); lookupErr != nil {
			return lookupErr
		}
		if _, deleteErr := transaction.Exec(`DELETE FROM recovery_codes WHERE user_id=?`, userID); deleteErr != nil {
			return deleteErr
		}
		for _, hash := range hashes {
			if _, insertErr := transaction.Exec(`INSERT INTO recovery_codes(user_id,hash,used_at,sig) VALUES(?,?,NULL,?)`, userID, hash, store.recoverySig(userID, hash, sql.NullInt64{})); insertErr != nil {
				return insertErr
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	store.event(userID, "recovery", "generated")
	return nil
}

// UseRecoveryCode hache hors transaction (argon2 est lent et la connexion est unique),
// puis marque le code par un UPDATE conditionnel sur used_at IS NULL.
func (store *authStore) UseRecoveryCode(userID int64, code string) bool {
	normalizedCode := normRecovery(code)
	if len(normalizedCode) != recoveryCodeLen {
		return false
	}
	type recoveryRow struct {
		hash string
		sig  []byte
	}
	rows, err := store.db.Query(`SELECT hash,sig FROM recovery_codes WHERE user_id=? AND used_at IS NULL`, userID)
	if err != nil {
		journal("auth: recovery: %v", err)
		return false
	}
	var storedCodes []recoveryRow
	for rows.Next() {
		var stored recoveryRow
		if scanErr := rows.Scan(&stored.hash, &stored.sig); scanErr != nil {
			rows.Close()
			journal("auth: recovery: %v", scanErr)
			return false
		}
		storedCodes = append(storedCodes, stored)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		journal("auth: recovery: %v", err)
		return false
	}
	for _, stored := range storedCodes {
		if !hmac.Equal(stored.sig, store.recoverySig(userID, stored.hash, sql.NullInt64{})) {
			store.guard(&tamperErr{userID, "recovery_codes"})
			return false
		}
	}
	for _, stored := range storedCodes {
		if !checkPassword(stored.hash, normalizedCode) {
			continue
		}
		usedAt := sql.NullInt64{Int64: authNow().Unix(), Valid: true}
		result, updateErr := store.db.Exec(`UPDATE recovery_codes SET used_at=?, sig=? WHERE user_id=? AND hash=? AND used_at IS NULL AND sig=?`,
			usedAt.Int64, store.recoverySig(userID, stored.hash, usedAt), userID, stored.hash, stored.sig)
		if updateErr != nil {
			journal("auth: recovery: %v", updateErr)
			return false
		}
		affected, updateErr := result.RowsAffected()
		if updateErr != nil {
			journal("auth: recovery: %v", updateErr)
			return false
		}
		if affected == 1 {
			store.event(userID, "recovery", "used")
		}
		return affected == 1
	}
	return false
}
