package main

import (
	"crypto/hmac"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	sqlite3 "modernc.org/sqlite/lib"
)

type Right string

const (
	RightMemRead      Right = "mem_read"
	RightMemWrite     Right = "mem_write"
	RightConfigWrite  Right = "config_write"
	RightMemSync      Right = "mem_sync"
	RightUsageRead    Right = "usage_read"
	RightSessionsRead Right = "sessions_read"
	RightExec         Right = "exec"
	RightFsRead       Right = "fs_read"
	RightFsWrite      Right = "fs_write"
)

// Les valeurs sont aussi les noms de colonnes de home_acl ; l'ordre fixe celui de la signature.
var allRights = []Right{RightMemRead, RightMemWrite, RightConfigWrite, RightMemSync, RightUsageRead, RightSessionsRead, RightExec, RightFsRead, RightFsWrite}

var (
	errTamper    = terr("srv.auth.tamper")
	errNoUser    = terr("srv.auth.utilisateurInconnu")
	errNameTaken = terr("srv.auth.nomPris")
	errBadName   = terr("srv.auth.nomInvalide")
)

type User struct {
	ID          int64
	Name        string
	PwHash      string
	Admin       bool
	Disabled    bool
	CreatedAt   int64
	PwChangedAt int64
	// MFAEnrolled est posé avec le premier facteur et effacé seulement par ResetMFA : sans lui, supprimer les lignes de
	// facteurs rouvrirait l'enrôlement avec le seul mot de passe.
	MFAEnrolled bool
}

type authStore struct {
	db  *authDB
	key []byte
}

func openAuthStore() (*authStore, error) {
	dir, err := authDir()
	if err != nil {
		return nil, err
	}
	db, err := openAuthDB(dir)
	if err != nil {
		return nil, err
	}
	key, err := loadMasterKey(dir, db)
	if err == nil {
		err = checkMasterKey(db, key)
	}
	store := &authStore{db: db, key: key}
	if err == nil {
		err = db.migrate(store.keyedStep)
	}
	if err == nil {
		err = walPerms(dir)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

const accountsKey = "accounts"

var errAccountsGone = errors.New("auth.db : tous les comptes ont disparu alors que des comptes avaient été créés ; supprimer auth.db et auth.key pour repartir de zéro")

// accountsValue : l'état des comptes (« off » à la création de la base, « on » au premier compte), lié par MAC au témoin de
// clé de cette base : copié d'une autre base, ou d'un état à l'autre, il ne vaut rien.
func accountsValue(key, witness []byte, state string) []byte {
	return append([]byte(state+":"), rowSig(key, "meta", accountsKey, string(witness), state)...)
}

func (store *authStore) setAccounts(transaction *sql.Tx, state string) error {
	var witness []byte
	if err := transaction.QueryRow(`SELECT value FROM meta WHERE key='key_witness'`).Scan(&witness); err != nil {
		return err
	}
	_, err := transaction.Exec(`INSERT OR REPLACE INTO meta(key,value) VALUES(?,?)`, accountsKey, accountsValue(store.key, witness, state))
	return err
}

func (store *authStore) markAccounts(transaction *sql.Tx) error {
	return store.setAccounts(transaction, "on")
}

// AccountsOn : le mode multi-utilisateur est actif. L'état est écrit avec le témoin de clé : absent, illisible ou
// contredit par la table users, c'est une altération et le service est fermé ; des comptes supprimés le ferment aussi.
func (store *authStore) AccountsOn() (bool, error) {
	hasUsers, err := store.HasUsers()
	if err != nil {
		return false, err
	}
	var accountsState, witness []byte
	err = store.db.QueryRow(`SELECT value FROM meta WHERE key=?`, accountsKey).Scan(&accountsState)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errTamper
	}
	if err == nil {
		err = store.db.QueryRow(`SELECT value FROM meta WHERE key='key_witness'`).Scan(&witness)
	}
	if err != nil {
		return false, err
	}
	switch {
	case hmac.Equal(accountsState, accountsValue(store.key, witness, "on")):
		if !hasUsers {
			return false, errAccountsGone
		}
		return true, nil
	case hmac.Equal(accountsState, accountsValue(store.key, witness, "off")) && !hasUsers:
		return false, nil
	}
	return false, errTamper
}

// keyedStep s'exécute dans la transaction de la migration v, une seule fois. Pour 7 (mfa_enrolled signé) : les lignes users
// à l'ancienne signature valide sont re-signées, le bit déduit des facteurs vérifiés à cet instant (une ligne altérée le
// reste) ; une base antérieure reçoit l'état des comptes qu'elle n'avait pas.
func (store *authStore) keyedStep(transaction *sql.Tx, version int) error {
	switch version {
	case 7:
		return store.stepMFAEnrolled(transaction)
	case 8:
		return store.stepFsRights(transaction)
	}
	return nil
}

// stepFsRights : la signature de home_acl couvre la liste ordonnée des droits, que 8 allonge de deux colonnes à 0. Une
// ligne à l'ancienne signature valide est re-signée ; une ligne altérée le reste.
func (store *authStore) stepFsRights(transaction *sql.Tx) error {
	const legacyRightCount = 7
	rows, err := transaction.Query(`SELECT user_id,home_id,` + strings.Join(strings.Split(rightCols(), ",")[:legacyRightCount], ",") + `,sig FROM home_acl`)
	if err != nil {
		return err
	}
	type legacy struct {
		uid  int64
		home string
		m    map[Right]bool
		sig  []byte
	}
	var legacyRows []legacy
	for rows.Next() {
		legacyRow := legacy{m: map[Right]bool{}}
		rawValues := make([]any, legacyRightCount)
		destinations := []any{&legacyRow.uid, &legacyRow.home}
		for valueIndex := range rawValues {
			destinations = append(destinations, &rawValues[valueIndex])
		}
		destinations = append(destinations, &legacyRow.sig)
		if scanErr := rows.Scan(destinations...); scanErr != nil {
			rows.Close()
			return scanErr
		}
		allValid := true
		signedFields := []string{legacyRow.home}
		for valueIndex, right := range allRights[:legacyRightCount] {
			granted, isValid := bit(rawValues[valueIndex])
			legacyRow.m[right], allValid = granted, allValid && isValid
			signedFields = append(signedFields, b01(granted))
		}
		if allValid && rowSigOK(store.key, "home_acl", strconv.FormatInt(legacyRow.uid, 10), legacyRow.sig, signedFields...) {
			legacyRows = append(legacyRows, legacyRow)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, legacyRow := range legacyRows {
		if _, updateErr := transaction.Exec(`UPDATE home_acl SET sig=? WHERE user_id=? AND home_id=? AND sig=?`, store.aclSig(legacyRow.uid, legacyRow.home, legacyRow.m), legacyRow.uid, legacyRow.home, legacyRow.sig); updateErr != nil {
			return updateErr
		}
	}
	return nil
}

func (store *authStore) stepMFAEnrolled(transaction *sql.Tx) error {
	rows, err := transaction.Query(`SELECT id,name,pw_hash,is_admin,disabled,sig FROM users`)
	if err != nil {
		return err
	}
	type legacy struct {
		u   User
		sig []byte
	}
	var legacyRows []legacy
	for rows.Next() {
		var legacyRow legacy
		var rawAdmin, rawDisabled any
		if scanErr := rows.Scan(&legacyRow.u.ID, &legacyRow.u.Name, &legacyRow.u.PwHash, &rawAdmin, &rawDisabled, &legacyRow.sig); scanErr != nil {
			rows.Close()
			return scanErr
		}
		var adminValid, disabledValid bool
		legacyRow.u.Admin, adminValid = bit(rawAdmin)
		legacyRow.u.Disabled, disabledValid = bit(rawDisabled)
		if adminValid && disabledValid && rowSigOK(store.key, "users", strconv.FormatInt(legacyRow.u.ID, 10), legacyRow.sig, legacyRow.u.Name, legacyRow.u.PwHash, b01(legacyRow.u.Admin), b01(legacyRow.u.Disabled)) {
			legacyRows = append(legacyRows, legacyRow)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, legacyRow := range legacyRows {
		user := legacyRow.u
		totp, factorErr := store.loadTOTP(transaction, user.ID)
		if factorErr != nil && !errors.Is(factorErr, errNoTOTP) && !errors.Is(factorErr, errTamper) {
			return factorErr
		}
		user.MFAEnrolled = factorErr == nil && totp.confirmed.Valid
		creds, factorErr := store.loadCreds(transaction, user.ID)
		if factorErr != nil {
			return factorErr
		}
		for _, cred := range creds {
			user.MFAEnrolled = user.MFAEnrolled || !cred.bad
		}
		if _, updateErr := transaction.Exec(`UPDATE users SET mfa_enrolled=?, sig=? WHERE id=? AND sig=?`, user.MFAEnrolled, store.userSig(&user), user.ID, legacyRow.sig); updateErr != nil {
			return updateErr
		}
	}
	var accountsMarkerCount, userCount int
	if countAccountsErr := transaction.QueryRow(`SELECT COUNT(*) FROM meta WHERE key=?`, accountsKey).Scan(&accountsMarkerCount); countAccountsErr != nil {
		return countAccountsErr
	}
	if countUsersErr := transaction.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); countUsersErr != nil {
		return countUsersErr
	}
	if accountsMarkerCount > 0 {
		return nil
	}
	if userCount > 0 {
		return store.setAccounts(transaction, "on")
	}
	return store.setAccounts(transaction, "off")
}

// markEnrolled pose le bit dans la transaction qui écrit le premier facteur ; u vient d'être relu et vérifié dans tx.
func (store *authStore) markEnrolled(transaction *sql.Tx, user *User) error {
	if user.MFAEnrolled {
		return nil
	}
	user.MFAEnrolled = true
	_, err := transaction.Exec(`UPDATE users SET mfa_enrolled=1, sig=? WHERE id=?`, store.userSig(user), user.ID)
	return err
}

func b01(flag bool) string {
	if flag {
		return "1"
	}
	return "0"
}

func (store *authStore) userSig(user *User) []byte {
	return rowSig(store.key, "users", strconv.FormatInt(user.ID, 10), user.Name, user.PwHash, b01(user.Admin), b01(user.Disabled), b01(user.MFAEnrolled))
}

func (store *authStore) aclSig(userID int64, homeID string, grantedRights map[Right]bool) []byte {
	return rowSig(store.key, "home_acl", strconv.FormatInt(userID, 10), append([]string{homeID}, rightStrings(grantedRights)...)...)
}

func (store *authStore) event(userID int64, kind, outcome string) {
	store.db.Exec(`INSERT INTO auth_events(at,user_id,kind,outcome) VALUES(?,?,?,?)`, authNow().Unix(), userID, kind, outcome)
}

type querier interface {
	QueryRow(string, ...any) *sql.Row
}

// tamperErr remonte la ligne fautive jusqu'à guard, qui la journalise hors transaction :
// la connexion unique est tenue par la transaction tant qu'elle n'est pas close.
type tamperErr struct {
	uid   int64
	table string
}

func (tampered *tamperErr) Error() string { return errTamper.Error() }
func (tampered *tamperErr) Unwrap() error { return errTamper }

func (store *authStore) guard(err error) error {
	var tampered *tamperErr
	if errors.As(err, &tampered) {
		store.event(tampered.uid, "tamper", tampered.table)
		return errTamper
	}
	return err
}

func (store *authStore) inTx(work func(*sql.Tx) error) error {
	transaction, err := store.db.Begin()
	if err != nil {
		return err
	}
	if workErr := work(transaction); workErr != nil {
		transaction.Rollback()
		return store.guard(workErr)
	}
	return transaction.Commit()
}

// bit : une valeur stockée hors de {0, 1} (ou d'un autre type) vaut altération.
func bit(value any) (isOne, valid bool) {
	intValue, isInt := value.(int64)
	return intValue == 1, isInt && (intValue == 0 || intValue == 1)
}

const nameMaxRunes = 64

// Le nom est stocké normalisé : l'unicité de la colonne vaut alors celle de normName.
func cleanName(name string) (string, error) {
	normalized := normName(name)
	if normalized == "" || utf8.RuneCountInString(normalized) > nameMaxRunes || strings.IndexFunc(normalized, func(character rune) bool { return unicode.IsControl(character) || unicode.IsSpace(character) }) >= 0 {
		return "", errBadName
	}
	return normalized, nil
}

func (store *authStore) AddUser(name, password string, admin bool) (int64, error) {
	normalized, err := cleanName(name)
	if err != nil {
		return 0, err
	}
	if validErr := validPassword(password); validErr != nil {
		return 0, validErr
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return 0, err
	}
	user := &User{Name: normalized, PwHash: passwordHash, Admin: admin}
	err = store.inTx(func(transaction *sql.Tx) error {
		now := authNow().Unix()
		result, txErr := transaction.Exec(`INSERT INTO users(name,pw_hash,is_admin,disabled,created_at,pw_changed_at) VALUES(?,?,?,0,?,?)`, normalized, passwordHash, admin, now, now)
		var constraintErr interface{ Code() int }
		if errors.As(txErr, &constraintErr) && constraintErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return errNameTaken
		}
		if txErr != nil {
			return txErr
		}
		if user.ID, txErr = result.LastInsertId(); txErr != nil {
			return txErr
		}
		if _, txErr = transaction.Exec(`UPDATE users SET sig=? WHERE id=?`, store.userSig(user), user.ID); txErr != nil {
			return txErr
		}
		return store.markAccounts(transaction)
	})
	if err != nil {
		return 0, err
	}
	return user.ID, nil
}

const userCols = `id,name,pw_hash,is_admin,disabled,created_at,pw_changed_at,mfa_enrolled,sig`

// scanUser ne journalise pas : l'appelant passe l'erreur par guard (ou inTx).
func (store *authStore) scanUser(row interface{ Scan(...any) error }) (*User, error) {
	user := &User{}
	var rawAdmin, rawDisabled, rawEnrolled any
	var sig []byte
	if err := row.Scan(&user.ID, &user.Name, &user.PwHash, &rawAdmin, &rawDisabled, &user.CreatedAt, &user.PwChangedAt, &rawEnrolled, &sig); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNoUser
		}
		return nil, err
	}
	var adminValid, disabledValid, enrolledValid bool
	user.Admin, adminValid = bit(rawAdmin)
	user.Disabled, disabledValid = bit(rawDisabled)
	user.MFAEnrolled, enrolledValid = bit(rawEnrolled)
	if !adminValid || !disabledValid || !enrolledValid || !hmac.Equal(sig, store.userSig(user)) {
		return user, &tamperErr{user.ID, "users"}
	}
	return user, nil
}

func (store *authStore) userByID(queryRunner querier, userID int64) (*User, error) {
	return store.scanUser(queryRunner.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, userID))
}

func (store *authStore) UserByName(name string) (*User, error) {
	user, err := store.scanUser(store.db.QueryRow(`SELECT `+userCols+` FROM users WHERE name=?`, normName(name)))
	if err != nil {
		return nil, store.guard(err)
	}
	return user, nil
}

// Users renvoie aussi les lignes altérées, marquées par bad[id], une entrée de journal par ligne.
func (store *authStore) Users() (users []*User, tamperedByID map[int64]bool, err error) {
	rows, err := store.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	tamperedByID = map[int64]bool{}
	for rows.Next() {
		user, scanErr := store.scanUser(rows)
		if scanErr != nil && !errors.Is(scanErr, errTamper) {
			return nil, nil, scanErr
		}
		if scanErr != nil {
			tamperedByID[user.ID] = true
		}
		users = append(users, user)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, nil, rowsErr
	}
	rows.Close()
	for userID := range tamperedByID {
		store.event(userID, "tamper", "users")
	}
	return users, tamperedByID, nil
}

// SetDisabled et SetPassword relisent et re-signent dans la même transaction, et révoquent les sessions du compte.
func (store *authStore) SetDisabled(userID int64, disabled bool) error {
	return store.inTx(func(transaction *sql.Tx) error {
		user, txErr := store.userByID(transaction, userID)
		if txErr != nil {
			return txErr
		}
		user.Disabled = disabled
		if _, updateErr := transaction.Exec(`UPDATE users SET disabled=?, sig=? WHERE id=?`, disabled, store.userSig(user), userID); updateErr != nil {
			return updateErr
		}
		_, txErr = transaction.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
		return txErr
	})
}

func (store *authStore) SetPassword(userID int64, password string) error {
	if err := validPassword(password); err != nil {
		return err
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return store.inTx(func(transaction *sql.Tx) error {
		user, txErr := store.userByID(transaction, userID)
		if txErr != nil {
			return txErr
		}
		user.PwHash = passwordHash
		if _, updateErr := transaction.Exec(`UPDATE users SET pw_hash=?, pw_changed_at=?, sig=? WHERE id=?`, passwordHash, authNow().Unix(), store.userSig(user), userID); updateErr != nil {
			return updateErr
		}
		_, txErr = transaction.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
		return txErr
	})
}

func (store *authStore) ResetMFA(userID int64) error {
	err := store.inTx(func(transaction *sql.Tx) error {
		user, txErr := store.userByID(transaction, userID)
		if txErr != nil {
			return txErr
		}
		for _, table := range []string{"totp", "webauthn_creds", "recovery_codes", "sessions"} {
			if _, deleteErr := transaction.Exec(`DELETE FROM `+table+` WHERE user_id=?`, userID); deleteErr != nil {
				return deleteErr
			}
		}
		user.MFAEnrolled = false
		_, txErr = transaction.Exec(`UPDATE users SET mfa_enrolled=0, sig=? WHERE id=?`, store.userSig(user), userID)
		return txErr
	})
	if err == nil {
		store.event(userID, "mfa_reset", "ok")
	}
	return err
}

// HasUsers renvoie l'erreur de lecture : l'appelant ne doit jamais la confondre avec « aucun compte » (mode ouvert).
func (store *authStore) HasUsers() (bool, error) {
	var exists int
	err := store.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists)
	return exists == 1, err
}

func rightCols() string {
	columns := make([]string, len(allRights))
	for rightIndex, right := range allRights {
		columns[rightIndex] = string(right)
	}
	return strings.Join(columns, ",")
}

// Rights relit et vérifie la ligne à chaque appel : aucune mise en cache.
func (store *authStore) Rights(userID int64, homeID string) (map[Right]bool, error) {
	grantedRights, err := store.rights(store.db, userID, homeID)
	return grantedRights, store.guard(err)
}

func (store *authStore) rights(queryRunner querier, userID int64, homeID string) (map[Right]bool, error) {
	rawValues := make([]any, len(allRights))
	destinations := make([]any, 0, len(allRights)+1)
	for valueIndex := range rawValues {
		destinations = append(destinations, &rawValues[valueIndex])
	}
	var sig []byte
	destinations = append(destinations, &sig)
	grantedRights := map[Right]bool{}
	err := queryRunner.QueryRow(`SELECT `+rightCols()+`,sig FROM home_acl WHERE user_id=? AND home_id=?`, userID, homeID).Scan(destinations...)
	if errors.Is(err, sql.ErrNoRows) {
		return grantedRights, nil
	}
	if err != nil {
		return nil, err
	}
	anyInvalid := false
	for valueIndex, right := range allRights {
		granted, valid := bit(rawValues[valueIndex])
		grantedRights[right], anyInvalid = granted, anyInvalid || !valid
	}
	if anyInvalid || !rowSigOK(store.key, "home_acl", strconv.FormatInt(userID, 10), sig, append([]string{homeID}, rightStrings(grantedRights)...)...) {
		return nil, &tamperErr{userID, "home_acl"}
	}
	return grantedRights, nil
}

func rightStrings(grantedRights map[Right]bool) []string {
	flags := make([]string, len(allRights))
	for rightIndex, right := range allRights {
		flags[rightIndex] = b01(grantedRights[right])
	}
	return flags
}

func (store *authStore) Grant(userID int64, homeID string, rights ...Right) error {
	return store.SetRights(userID, homeID, rights, nil)
}

func (store *authStore) Revoke(userID int64, homeID string, rights ...Right) error {
	return store.SetRights(userID, homeID, nil, rights)
}

// SetRights accorde puis retire dans une seule transaction : une seule ligne re-signée, rien d'appliqué en cas d'échec.
func (store *authStore) SetRights(userID int64, homeID string, grant, revoke []Right) error {
	if homeID == "" {
		return terr("srv.auth.homeVide")
	}
	joinRights := func(rightList []Right) string {
		nameList := make([]string, len(rightList))
		for rightIndex, right := range rightList {
			nameList[rightIndex] = string(right)
		}
		return strings.Join(nameList, ",")
	}
	for _, right := range append(slices.Clone(grant), revoke...) {
		if !validRight(right) {
			return terr("srv.auth.droitInconnu", "A1", string(right))
		}
	}
	err := store.inTx(func(transaction *sql.Tx) error {
		if _, lookupErr := store.userByID(transaction, userID); lookupErr != nil {
			return lookupErr
		}
		grantedRights, txErr := store.rights(transaction, userID, homeID)
		if txErr != nil {
			return txErr
		}
		for _, right := range grant {
			grantedRights[right] = true
		}
		for _, right := range revoke {
			grantedRights[right] = false
		}
		// Écrire (mémoire ou réglages) sans lire permettrait de lire par le détour des brouillons du modèle.
		if (grantedRights[RightMemWrite] || grantedRights[RightConfigWrite]) && !grantedRights[RightMemRead] {
			return terr("srv.auth.ecritureSansLecture")
		}
		args := []any{userID, homeID}
		for _, right := range allRights {
			args = append(args, grantedRights[right])
		}
		args = append(args, store.aclSig(userID, homeID, grantedRights))
		_, txErr = transaction.Exec(`INSERT OR REPLACE INTO home_acl(user_id,home_id,`+rightCols()+`,sig) VALUES(?,?`+strings.Repeat(",?", len(allRights)+1)+`)`, args...)
		return txErr
	})
	if err != nil {
		return err
	}
	var operations []string
	if len(grant) > 0 {
		operations = append(operations, "grant "+homeID+" "+joinRights(grant))
	}
	if len(revoke) > 0 {
		operations = append(operations, "revoke "+homeID+" "+joinRights(revoke))
	}
	store.event(userID, "acl", strings.Join(operations, "; "))
	return nil
}

func validRight(right Right) bool {
	for _, candidate := range allRights {
		if candidate == right {
			return true
		}
	}
	return false
}
