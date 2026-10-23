package main

import (
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed auth_migrations/*.sql
var authMigrations embed.FS

type authDB struct{ *sql.DB }

func authDir() (string, error) {
	if d := os.Getenv("GLD_AUTH_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("auth dir: %w", err)
	}
	return filepath.Join(base, "gld"), nil
}

func checkPrivate(path string, wantDir bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: symlink refused", path)
	}
	if fi.IsDir() != wantDir {
		return fmt.Errorf("%s: unexpected file type", path)
	}
	if !ownedByMe(fi) {
		return fmt.Errorf("%s: not owned by current user", path)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s: mode %v too permissive", path, fi.Mode().Perm())
	}
	return nil
}

func openAuthDB(dir string) (*authDB, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	if err := checkPrivate(dir, true); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "auth.db")
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s: symlink refused", path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	if err := checkPrivate(path, false); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	a := &authDB{db}
	if err := a.migrate(nil); err != nil {
		db.Close()
		return nil, err
	}
	if err := walPerms(dir); err != nil {
		db.Close()
		return nil, err
	}
	return a, nil
}

func walPerms(dir string) error {
	for _, sfx := range []string{"-wal", "-shm"} {
		if err := os.Chmod(filepath.Join(dir, "auth.db")+sfx, 0o600); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// keyedFrom : première migration qui exige la clé maîtresse (re-signature des lignes existantes). Elle et les suivantes ne
// s'appliquent que par openAuthStore, avec keyed exécuté dans la transaction même de la migration : la re-signature a lieu
// une seule fois, liée au numéro de version, jamais à un drapeau qu'on pourrait effacer.
const keyedFrom = 7

func (a *authDB) migrate(keyed func(*sql.Tx, int) error) error {
	if _, err := a.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var cur int
	err := a.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_version`).Scan(&cur)
	if err != nil {
		return err
	}
	files, err := authMigrations.ReadDir("auth_migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		v, err := strconv.Atoi(strings.SplitN(n, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: %w", n, err)
		}
		if v <= cur {
			continue
		}
		if v >= keyedFrom && keyed == nil {
			break
		}
		body, err := authMigrations.ReadFile("auth_migrations/" + n)
		if err != nil {
			return err
		}
		tx, err := a.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", n, err)
		}
		if keyed != nil {
			if err := keyed(tx, v); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s: %w", n, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, v); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
