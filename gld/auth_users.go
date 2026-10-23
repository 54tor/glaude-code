package main

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
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
	key, err := loadMasterKey(dir)
	if err == nil {
		err = checkMasterKey(db, key)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &authStore{db: db, key: key}, nil
}

func b01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (s *authStore) userSig(u *User) []byte {
	return rowSig(s.key, "users", strconv.FormatInt(u.ID, 10), u.Name, u.PwHash, b01(u.Admin), b01(u.Disabled))
}

func (s *authStore) aclSig(userID int64, homeID string, r map[Right]bool) []byte {
	return rowSig(s.key, "home_acl", strconv.FormatInt(userID, 10), append([]string{homeID}, rightStrings(r)...)...)
}

func (s *authStore) event(userID int64, kind, outcome string) {
	s.db.Exec(`INSERT INTO auth_events(at,user_id,kind,outcome) VALUES(?,?,?,?)`, time.Now().Unix(), userID, kind, outcome)
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
		now := time.Now().Unix()
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
		_, err = tx.Exec(`UPDATE users SET sig=? WHERE id=?`, s.userSig(u), u.ID)
		return err
	})
	if err != nil {
		return 0, err
	}
	return u.ID, nil
}

const userCols = `id,name,pw_hash,is_admin,disabled,created_at,pw_changed_at,sig`

// scanUser ne journalise pas : l'appelant passe l'erreur par guard (ou inTx).
func (s *authStore) scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	var adm, dis any
	var sig []byte
	if err := row.Scan(&u.ID, &u.Name, &u.PwHash, &adm, &dis, &u.CreatedAt, &u.PwChangedAt, &sig); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNoUser
		}
		return nil, err
	}
	var ok1, ok2 bool
	u.Admin, ok1 = bit(adm)
	u.Disabled, ok2 = bit(dis)
	if !ok1 || !ok2 || !rowSigOK(s.key, "users", strconv.FormatInt(u.ID, 10), sig, u.Name, u.PwHash, b01(u.Admin), b01(u.Disabled)) {
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
		if _, err := tx.Exec(`UPDATE users SET pw_hash=?, pw_changed_at=?, sig=? WHERE id=?`, h, time.Now().Unix(), s.userSig(u), id); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
		return err
	})
}

func (s *authStore) ResetMFA(userID int64) error {
	err := s.inTx(func(tx *sql.Tx) error {
		if _, err := s.userByID(tx, userID); err != nil {
			return err
		}
		for _, t := range []string{"totp", "webauthn_creds", "recovery_codes", "sessions"} {
			if _, err := tx.Exec(`DELETE FROM `+t+` WHERE user_id=?`, userID); err != nil {
				return err
			}
		}
		return nil
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
	return s.setRights(userID, homeID, true, rights)
}

func (s *authStore) Revoke(userID int64, homeID string, rights ...Right) error {
	return s.setRights(userID, homeID, false, rights)
}

func (s *authStore) setRights(userID int64, homeID string, on bool, rights []Right) error {
	if homeID == "" {
		return terr("srv.auth.homeVide")
	}
	names := make([]string, len(rights))
	for i, r := range rights {
		if !validRight(r) {
			return terr("srv.auth.droitInconnu", "A1", string(r))
		}
		names[i] = string(r)
	}
	err := s.inTx(func(tx *sql.Tx) error {
		if _, err := s.userByID(tx, userID); err != nil {
			return err
		}
		m, err := s.rights(tx, userID, homeID)
		if err != nil {
			return err
		}
		for _, r := range rights {
			m[r] = on
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
	op := "revoke"
	if on {
		op = "grant"
	}
	s.event(userID, "acl", op+" "+homeID+" "+strings.Join(names, ","))
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
