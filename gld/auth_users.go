package main

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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

func (s *authStore) tamper(userID int64, table string) error {
	s.event(userID, "tamper", table)
	return errTamper
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
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var dup int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE name=?`, n).Scan(&dup); err != nil {
		return 0, err
	}
	if dup > 0 {
		return 0, errNameTaken
	}
	now := time.Now().Unix()
	res, err := tx.Exec(`INSERT INTO users(name,pw_hash,is_admin,disabled,created_at,pw_changed_at) VALUES(?,?,?,0,?,?)`, n, h, admin, now, now)
	if err != nil {
		return 0, err
	}
	u := &User{Name: n, PwHash: h, Admin: admin}
	if u.ID, err = res.LastInsertId(); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE users SET sig=? WHERE id=?`, s.userSig(u), u.ID); err != nil {
		return 0, err
	}
	return u.ID, tx.Commit()
}

const userCols = `id,name,pw_hash,is_admin,disabled,created_at,pw_changed_at,sig`

func (s *authStore) scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	var sig []byte
	if err := row.Scan(&u.ID, &u.Name, &u.PwHash, &u.Admin, &u.Disabled, &u.CreatedAt, &u.PwChangedAt, &sig); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNoUser
		}
		return nil, err
	}
	if !rowSigOK(s.key, "users", strconv.FormatInt(u.ID, 10), sig, u.Name, u.PwHash, b01(u.Admin), b01(u.Disabled)) {
		return nil, s.tamper(u.ID, "users")
	}
	return u, nil
}

func (s *authStore) UserByName(name string) (*User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE name=?`, normName(name)))
}

func (s *authStore) userByID(id int64) (*User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
}

// Users renvoie aussi les lignes dont la signature est fausse, marquées par bad[id].
func (s *authStore) Users() (users []*User, bad map[int64]bool, err error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	bad = map[int64]bool{}
	var tampered []int64
	for rows.Next() {
		u := &User{}
		var sig []byte
		if err := rows.Scan(&u.ID, &u.Name, &u.PwHash, &u.Admin, &u.Disabled, &u.CreatedAt, &u.PwChangedAt, &sig); err != nil {
			return nil, nil, err
		}
		if !rowSigOK(s.key, "users", strconv.FormatInt(u.ID, 10), sig, u.Name, u.PwHash, b01(u.Admin), b01(u.Disabled)) {
			bad[u.ID] = true
			tampered = append(tampered, u.ID)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows.Close()
	for _, id := range tampered {
		s.tamper(id, "users")
	}
	return users, bad, nil
}

func (s *authStore) saveUser(u *User, sql string, args ...any) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(sql, args...); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET sig=? WHERE id=?`, s.userSig(u), u.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, u.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetDisabled et SetPassword révoquent les sessions ouvertes du compte.
func (s *authStore) SetDisabled(id int64, off bool) error {
	u, err := s.userByID(id)
	if err != nil {
		return err
	}
	u.Disabled = off
	return s.saveUser(u, `UPDATE users SET disabled=? WHERE id=?`, off, id)
}

func (s *authStore) SetPassword(id int64, pw string) error {
	if err := validPassword(pw); err != nil {
		return err
	}
	u, err := s.userByID(id)
	if err != nil {
		return err
	}
	if u.PwHash, err = hashPassword(pw); err != nil {
		return err
	}
	return s.saveUser(u, `UPDATE users SET pw_hash=?, pw_changed_at=? WHERE id=?`, u.PwHash, time.Now().Unix(), id)
}

func (s *authStore) ResetMFA(userID int64) error {
	if _, err := s.userByID(userID); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"totp", "webauthn_creds", "recovery_codes", "sessions"} {
		if _, err := tx.Exec(`DELETE FROM `+t+` WHERE user_id=?`, userID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.event(userID, "mfa_reset", "ok")
	return nil
}

func (s *authStore) HasUsers() bool {
	var n int
	return s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users)`).Scan(&n) == nil && n == 1
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
	vals := make([]bool, len(allRights))
	dst := make([]any, 0, len(allRights)+1)
	for i := range vals {
		dst = append(dst, &vals[i])
	}
	var sig []byte
	dst = append(dst, &sig)
	m := map[Right]bool{}
	err := s.db.QueryRow(`SELECT `+rightCols()+`,sig FROM home_acl WHERE user_id=? AND home_id=?`, userID, homeID).Scan(dst...)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	for i, r := range allRights {
		m[r] = vals[i]
	}
	if !rowSigOK(s.key, "home_acl", strconv.FormatInt(userID, 10), sig, append([]string{homeID}, rightStrings(m)...)...) {
		return nil, s.tamper(userID, "home_acl")
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
	if _, err := s.userByID(userID); err != nil {
		return err
	}
	m, err := s.Rights(userID, homeID)
	if err != nil {
		return err
	}
	names := make([]string, len(rights))
	for i, r := range rights {
		if !validRight(r) {
			return terr("srv.auth.droitInconnu", "A1", string(r))
		}
		m[r] = on
		names[i] = string(r)
	}
	args := []any{userID, homeID}
	for _, r := range allRights {
		args = append(args, m[r])
	}
	args = append(args, s.aclSig(userID, homeID, m))
	q := `INSERT OR REPLACE INTO home_acl(user_id,home_id,` + rightCols() + `,sig) VALUES(?,?` + strings.Repeat(",?", len(allRights)+1) + `)`
	if _, err := s.db.Exec(q, args...); err != nil {
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
