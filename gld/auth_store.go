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
	if envDir := os.Getenv("GLD_AUTH_DIR"); envDir != "" {
		return envDir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("auth dir: %w", err)
	}
	return filepath.Join(base, "gld"), nil
}

func checkPrivate(path string, wantDir bool) error {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: symlink refused", path)
	}
	if fileInfo.IsDir() != wantDir {
		return fmt.Errorf("%s: unexpected file type", path)
	}
	if !ownedByMe(fileInfo) {
		return fmt.Errorf("%s: not owned by current user", path)
	}
	if runtime.GOOS != "windows" && fileInfo.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s: mode %v too permissive", path, fileInfo.Mode().Perm())
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
	if fileInfo, err := os.Lstat(path); err == nil && fileInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s: symlink refused", path)
	}
	dbFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	dbFile.Close()
	if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
		return nil, chmodErr
	}
	if permErr := checkPrivate(path, false); permErr != nil {
		return nil, permErr
	}
	dsn := url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	authDatabase := &authDB{db}
	if migrateErr := authDatabase.migrate(nil); migrateErr != nil {
		db.Close()
		return nil, migrateErr
	}
	if walErr := walPerms(dir); walErr != nil {
		db.Close()
		return nil, walErr
	}
	return authDatabase, nil
}

func walPerms(dir string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Chmod(filepath.Join(dir, "auth.db")+suffix, 0o600); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// keyedFrom : première migration qui exige la clé maîtresse (re-signature des lignes existantes). Elle et les suivantes ne
// s'appliquent que par openAuthStore, avec keyed exécuté dans la transaction même de la migration : la re-signature a lieu
// une seule fois, liée au numéro de version, jamais à un drapeau qu'on pourrait effacer.
const keyedFrom = 7

func (authDatabase *authDB) migrate(keyed func(*sql.Tx, int) error) error {
	if _, err := authDatabase.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var currentVersion int
	err := authDatabase.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_version`).Scan(&currentVersion)
	if err != nil {
		return err
	}
	files, err := authMigrations.ReadDir("auth_migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for _, entry := range files {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		version, stepErr := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if stepErr != nil {
			return fmt.Errorf("migration %s: %w", name, stepErr)
		}
		if version <= currentVersion {
			continue
		}
		if version >= keyedFrom && keyed == nil {
			break
		}
		body, stepErr := authMigrations.ReadFile("auth_migrations/" + name)
		if stepErr != nil {
			return stepErr
		}
		transaction, stepErr := authDatabase.Begin()
		if stepErr != nil {
			return stepErr
		}
		if _, execErr := transaction.Exec(string(body)); execErr != nil {
			transaction.Rollback()
			return fmt.Errorf("migration %s: %w", name, execErr)
		}
		if keyed != nil {
			if keyedErr := keyed(transaction, version); keyedErr != nil {
				transaction.Rollback()
				return fmt.Errorf("migration %s: %w", name, keyedErr)
			}
		}
		if _, recordErr := transaction.Exec(`INSERT INTO schema_version(version) VALUES (?)`, version); recordErr != nil {
			transaction.Rollback()
			return recordErr
		}
		if commitErr := transaction.Commit(); commitErr != nil {
			return commitErr
		}
	}
	return nil
}
