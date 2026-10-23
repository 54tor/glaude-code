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
)

// Les valeurs sont aussi les noms de colonnes de home_acl ; l'ordre fixe celui de la signature.
var allRights = []Right{RightMemRead, RightMemWrite, RightConfigWrite, RightMemSync, RightUsageRead, RightSessionsRead, RightExec}

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
	s := &authStore{db: db, key: key}
	if err == nil {
		err = s.upgrade()
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

const (
	accountsKey = "accounts"
	upgradeKey  = "upgrade"
	upgradeVer  = "7"
)

var errAccountsGone = errors.New("auth.db : tous les comptes ont disparu alors que des comptes avaient été créés ; supprimer auth.db et auth.key pour repartir de zéro")

// Le marqueur est lié au témoin de clé de cette base : copié d'une autre base, il ne vaut rien.
func (s *authStore) accountsMAC(q querier) ([]byte, error) {
	var w []byte
	if err := q.QueryRow(`SELECT value FROM meta WHERE key='key_witness'`).Scan(&w); err != nil {
		return nil, err
	}
	return rowSig(s.key, "meta", accountsKey, string(w)), nil
}

func (s *authStore) markAccounts(tx *sql.Tx) error {
	m, err := s.accountsMAC(tx)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT OR REPLACE INTO meta(key,value) VALUES(?,?)`, accountsKey, m)
	return err
}

// AccountsOn : le mode multi-utilisateur est actif. Sans compte, seul l'absence du marqueur rend le mode ouvert :
// des comptes supprimés de la base ferment le service au lieu de l'ouvrir.
func (s *authStore) AccountsOn() (bool, error) {
	on, err := s.HasUsers()
	if err != nil {
		return false, err
	}
	var v []byte
	err = s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, accountsKey).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return on, nil
	}
	if err != nil {
		return false, err
	}
	want, err := s.accountsMAC(s.db)
	if err != nil {
		return false, err
	}
	if !hmac.Equal(v, want) {
		return false, errTamper
	}
	if !on {
		return false, errAccountsGone
	}
	return true, nil
}

// upgrade re-signe une fois, avec la clé, les lignes users d'une base antérieure à mfa_enrolled : seule une ligne dont
// l'ancienne signature est valide est re-signée (une ligne altérée le reste), et le bit est déduit des facteurs vérifiés.
// Il pose aussi le marqueur des comptes sur une base qui en a déjà.
func (s *authStore) upgrade() error {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, upgradeKey).Scan(&v)
	if err == nil && v == upgradeVer {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return s.inTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT ` + userCols + ` FROM users`)
		if err != nil {
			return err
		}
		var old []*User
		for rows.Next() {
			u, err := s.scanUser(rows)
			if err != nil && !errors.Is(err, errTamper) {
				rows.Close()
				return err
			}
			if err != nil && !u.MFAEnrolled {
				old = append(old, u)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, u := range old {
			var sig []byte
			if err := tx.QueryRow(`SELECT sig FROM users WHERE id=?`, u.ID).Scan(&sig); err != nil {
				return err
			}
			if !rowSigOK(s.key, "users", strconv.FormatInt(u.ID, 10), sig, u.Name, u.PwHash, b01(u.Admin), b01(u.Disabled)) {
				continue
			}
			t, err := s.loadTOTP(tx, u.ID)
			if err != nil && !errors.Is(err, errNoTOTP) && !errors.Is(err, errTamper) {
				return err
			}
			u.MFAEnrolled = err == nil && t.confirmed.Valid
			creds, err := s.loadCreds(tx, u.ID)
			if err != nil {
				return err
			}
			for _, c := range creds {
				u.MFAEnrolled = u.MFAEnrolled || !c.bad
			}
			if _, err := tx.Exec(`UPDATE users SET mfa_enrolled=?, sig=? WHERE id=? AND sig=?`, u.MFAEnrolled, s.userSig(u), u.ID, sig); err != nil {
				return err
			}
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			if err := s.markAccounts(tx); err != nil {
				return err
			}
		}
		_, err = tx.Exec(`INSERT OR REPLACE INTO meta(key,value) VALUES(?,?)`, upgradeKey, upgradeVer)
		return err
	})
}

// markEnrolled pose le bit dans la transaction qui écrit le premier facteur ; u vient d'être relu et vérifié dans tx.
func (s *authStore) markEnrolled(tx *sql.Tx, u *User) error {
	if u.MFAEnrolled {
		return nil
	}
	u.MFAEnrolled = true
	_, err := tx.Exec(`UPDATE users SET mfa_enrolled=1, sig=? WHERE id=?`, s.userSig(u), u.ID)
	return err
}

func b01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (s *authStore) userSig(u *User) []byte {
	return rowSig(s.key, "users", strconv.FormatInt(u.ID, 10), u.Name, u.PwHash, b01(u.Admin), b01(u.Disabled), b01(u.MFAEnrolled))
}

func (s *authStore) aclSig(userID int64, homeID string, r map[Right]bool) []byte {
	return rowSig(s.key, "home_acl", strconv.FormatInt(userID, 10), append([]string{homeID}, rightStrings(r)...)...)
}

func (s *authStore) event(userID int64, kind, outcome string) {
	s.db.Exec(`INSERT INTO auth_events(at,user_id,kind,outcome) VALUES(?,?,?,?)`, authNow().Unix(), userID, kind, outcome)
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

func (e *tamperErr) Error() string { return errTamper.Error() }
func (e *tamperErr) Unwrap() error { return errTamper }

func (s *authStore) guard(err error) error {
	var t *tamperErr
	if errors.As(err, &t) {
		s.event(t.uid, "tamper", t.table)
		return errTamper
	}
	return err
}

func (s *authStore) inTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return s.guard(err)
	}
	return tx.Commit()
}

// bit : une valeur stockée hors de {0, 1} (ou d'un autre type) vaut altération.
func bit(v any) (on, ok bool) {
	n, isInt := v.(int64)
	return n == 1, isInt && (n == 0 || n == 1)
}

// Le nom est stocké normalisé : l'unicité de la colonne vaut alors celle de normName.
func cleanName(name string) (string, error) {
	n := normName(name)
	if n == "" || utf8.RuneCountInString(n) > 64 || strings.IndexFunc(n, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return "", errBadName
	}
	return n, nil
}

func (s *authStore) AddUser(name, pw string, admin bool) (int64, error) {
	n, err := cleanName(name)
	if err != nil {
		return 0, err
	}
	if err := validPassword(pw); err != nil {
		return 0, err
	}
	h, err := hashPassword(pw)
	if err != nil {
		return 0, err
	}
	u := &User{Name: n, PwHash: h, Admin: admin}
	err = s.inTx(func(tx *sql.Tx) error {
		now := authNow().Unix()
		res, err := tx.Exec(`INSERT INTO users(name,pw_hash,is_admin,disabled,created_at,pw_changed_at) VALUES(?,?,?,0,?,?)`, n, h, admin, now, now)
		var se interface{ Code() int }
		if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return errNameTaken
		}
		if err != nil {
			return err
		}
		if u.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE users SET sig=? WHERE id=?`, s.userSig(u), u.ID); err != nil {
			return err
		}
		return s.markAccounts(tx)
	})
	if err != nil {
		return 0, err
	}
	return u.ID, nil
}

const userCols = `id,name,pw_hash,is_admin,disabled,created_at,pw_changed_at,mfa_enrolled,sig`

// scanUser ne journalise pas : l'appelant passe l'erreur par guard (ou inTx).
func (s *authStore) scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	var adm, dis, enr any
	var sig []byte
	if err := row.Scan(&u.ID, &u.Name, &u.PwHash, &adm, &dis, &u.CreatedAt, &u.PwChangedAt, &enr, &sig); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNoUser
		}
		return nil, err
	}
	var ok1, ok2, ok3 bool
	u.Admin, ok1 = bit(adm)
	u.Disabled, ok2 = bit(dis)
	u.MFAEnrolled, ok3 = bit(enr)
	if !ok1 || !ok2 || !ok3 || !hmac.Equal(sig, s.userSig(u)) {
		return u, &tamperErr{u.ID, "users"}
	}
	return u, nil
}

func (s *authStore) userByID(q querier, id int64) (*User, error) {
	return s.scanUser(q.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func (s *authStore) UserByName(name string) (*User, error) {
	u, err := s.scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE name=?`, normName(name)))
	if err != nil {
		return nil, s.guard(err)
	}
	return u, nil
}

// Users renvoie aussi les lignes altérées, marquées par bad[id], une entrée de journal par ligne.
func (s *authStore) Users() (users []*User, bad map[int64]bool, err error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	bad = map[int64]bool{}
	for rows.Next() {
		u, err := s.scanUser(rows)
		if err != nil && !errors.Is(err, errTamper) {
			return nil, nil, err
		}
		if err != nil {
			bad[u.ID] = true
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows.Close()
	for id := range bad {
		s.event(id, "tamper", "users")
	}
	return users, bad, nil
}

// SetDisabled et SetPassword relisent et re-signent dans la même transaction, et révoquent les sessions du compte.
func (s *authStore) SetDisabled(id int64, off bool) error {
	return s.inTx(func(tx *sql.Tx) error {
		u, err := s.userByID(tx, id)
		if err != nil {
			return err
		}
		u.Disabled = off
		if _, err := tx.Exec(`UPDATE users SET disabled=?, sig=? WHERE id=?`, off, s.userSig(u), id); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
		return err
	})
}

func (s *authStore) SetPassword(id int64, pw string) error {
	if err := validPassword(pw); err != nil {
		return err
	}
	h, err := hashPassword(pw)
	if err != nil {
		return err
	}
	return s.inTx(func(tx *sql.Tx) error {
		u, err := s.userByID(tx, id)
		if err != nil {
			return err
		}
		u.PwHash = h
		if _, err := tx.Exec(`UPDATE users SET pw_hash=?, pw_changed_at=?, sig=? WHERE id=?`, h, authNow().Unix(), s.userSig(u), id); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
		return err
	})
}

func (s *authStore) ResetMFA(userID int64) error {
	err := s.inTx(func(tx *sql.Tx) error {
		u, err := s.userByID(tx, userID)
		if err != nil {
			return err
		}
		for _, t := range []string{"totp", "webauthn_creds", "recovery_codes", "sessions"} {
			if _, err := tx.Exec(`DELETE FROM `+t+` WHERE user_id=?`, userID); err != nil {
				return err
			}
		}
		u.MFAEnrolled = false
		_, err = tx.Exec(`UPDATE users SET mfa_enrolled=0, sig=? WHERE id=?`, s.userSig(u), userID)
		return err
	})
	if err == nil {
		s.event(userID, "mfa_reset", "ok")
	}
	return err
}

// HasUsers renvoie l'erreur de lecture : l'appelant ne doit jamais la confondre avec « aucun compte » (mode ouvert).
func (s *authStore) HasUsers() (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users)`).Scan(&n)
	return n == 1, err
}

func rightCols() string {
	c := make([]string, len(allRights))
	for i, r := range allRights {
		c[i] = string(r)
	}
	return strings.Join(c, ",")
}

// Rights relit et vérifie la ligne à chaque appel : aucune mise en cache.
func (s *authStore) Rights(userID int64, homeID string) (map[Right]bool, error) {
	m, err := s.rights(s.db, userID, homeID)
	return m, s.guard(err)
}

func (s *authStore) rights(q querier, userID int64, homeID string) (map[Right]bool, error) {
	vals := make([]any, len(allRights))
	dst := make([]any, 0, len(allRights)+1)
	for i := range vals {
		dst = append(dst, &vals[i])
	}
	var sig []byte
	dst = append(dst, &sig)
	m := map[Right]bool{}
	err := q.QueryRow(`SELECT `+rightCols()+`,sig FROM home_acl WHERE user_id=? AND home_id=?`, userID, homeID).Scan(dst...)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	bad := false
	for i, r := range allRights {
		on, ok := bit(vals[i])
		m[r], bad = on, bad || !ok
	}
	if bad || !rowSigOK(s.key, "home_acl", strconv.FormatInt(userID, 10), sig, append([]string{homeID}, rightStrings(m)...)...) {
		return nil, &tamperErr{userID, "home_acl"}
	}
	return m, nil
}

func rightStrings(m map[Right]bool) []string {
	f := make([]string, len(allRights))
	for i, r := range allRights {
		f[i] = b01(m[r])
	}
	return f
}

func (s *authStore) Grant(userID int64, homeID string, rights ...Right) error {
	return s.SetRights(userID, homeID, rights, nil)
}

func (s *authStore) Revoke(userID int64, homeID string, rights ...Right) error {
	return s.SetRights(userID, homeID, nil, rights)
}

// SetRights accorde puis retire dans une seule transaction : une seule ligne re-signée, rien d'appliqué en cas d'échec.
func (s *authStore) SetRights(userID int64, homeID string, grant, revoke []Right) error {
	if homeID == "" {
		return terr("srv.auth.homeVide")
	}
	names := func(rs []Right) string {
		n := make([]string, len(rs))
		for i, r := range rs {
			n[i] = string(r)
		}
		return strings.Join(n, ",")
	}
	for _, r := range append(slices.Clone(grant), revoke...) {
		if !validRight(r) {
			return terr("srv.auth.droitInconnu", "A1", string(r))
		}
	}
	err := s.inTx(func(tx *sql.Tx) error {
		if _, err := s.userByID(tx, userID); err != nil {
			return err
		}
		m, err := s.rights(tx, userID, homeID)
		if err != nil {
			return err
		}
		for _, r := range grant {
			m[r] = true
		}
		for _, r := range revoke {
			m[r] = false
		}
		args := []any{userID, homeID}
		for _, r := range allRights {
			args = append(args, m[r])
		}
		args = append(args, s.aclSig(userID, homeID, m))
		_, err = tx.Exec(`INSERT OR REPLACE INTO home_acl(user_id,home_id,`+rightCols()+`,sig) VALUES(?,?`+strings.Repeat(",?", len(allRights)+1)+`)`, args...)
		return err
	})
	if err != nil {
		return err
	}
	var ops []string
	if len(grant) > 0 {
		ops = append(ops, "grant "+homeID+" "+names(grant))
	}
	if len(revoke) > 0 {
		ops = append(ops, "revoke "+homeID+" "+names(revoke))
	}
	s.event(userID, "acl", strings.Join(ops, "; "))
	return nil
}

func validRight(r Right) bool {
	for _, k := range allRights {
		if k == r {
			return true
		}
	}
	return false
}
