package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func testStore(t *testing.T) *authStore {
	t.Helper()
	stubKeyring(t)
	t.Setenv("GLD_AUTH_DIR", t.TempDir())
	t.Setenv("GLD_AUTH_KEY", strings.Repeat("cd", 32))
	total, tt := argonMemKiB, argonTime
	argonMemKiB, argonTime = 64, 1
	t.Cleanup(func() { argonMemKiB, argonTime = total, tt })
	localAuthStore, err := openAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { localAuthStore.db.Close() })
	return localAuthStore
}

const testPw = "correct horse battery"

func tamperEvents(t *testing.T, localAuthStore *authStore) int {
	var count int
	if err := localAuthStore.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='tamper'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestAddUserNameUnique(t *testing.T) {
	authStore := testStore(t)
	if on, err := authStore.HasUsers(); on || err != nil {
		t.Fatal("empty store has users", err)
	}
	if _, err := authStore.AddUser("Alice", testPw, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", " ALICE ", "Ａｌｉｃｅ"} {
		if _, err := authStore.AddUser(name, testPw, false); !errors.Is(err, errNameTaken) {
			t.Fatalf("%q: %v", name, err)
		}
	}
	user, err := authStore.UserByName("ALICE")
	if err != nil || user.Name != "alice" || !checkPassword(user.PwHash, testPw) {
		t.Fatalf("lookup: %+v %v", user, err)
	}
	if on, err := authStore.HasUsers(); !on || err != nil {
		t.Fatal("HasUsers", on, err)
	}
	if _, err := authStore.UserByName("bob"); !errors.Is(err, errNoUser) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := authStore.AddUser("a b", testPw, false); !errors.Is(err, errBadName) {
		t.Fatalf("space: %v", err)
	}
}

func TestUserTamper(t *testing.T) {
	authStore := testStore(t)
	id, _ := authStore.AddUser("alice", testPw, false)
	if err := authStore.SetDisabled(id, true); err != nil {
		t.Fatal(err)
	}
	if user, err := authStore.UserByName("alice"); err != nil || !user.Disabled {
		t.Fatalf("disabled: %+v %v", user, err)
	}
	authStore.db.Exec(`UPDATE users SET is_admin=1 WHERE id=?`, id)
	if _, err := authStore.UserByName("alice"); !errors.Is(err, errTamper) || tamperEvents(t, authStore) != 1 {
		t.Fatalf("tamper: %v", err)
	}
	if err := authStore.SetPassword(id, testPw+"x"); !errors.Is(err, errTamper) {
		t.Fatalf("re-sign of tampered row: %v", err)
	}
}

func TestGrantDefaultNone(t *testing.T) {
	authStore := testStore(t)
	id, _ := authStore.AddUser("alice", testPw, true)
	lookup, err := authStore.Rights(id, "main")
	if err != nil || len(lookup) != 0 {
		t.Fatalf("default: %v %v", lookup, err)
	}
	if err := authStore.Grant(id, "main", RightMemRead, RightExec); err != nil {
		t.Fatal(err)
	}
	if lookup, _ = authStore.Rights(id, "main"); !lookup[RightMemRead] || !lookup[RightExec] || lookup[RightMemWrite] {
		t.Fatalf("granted: %v", lookup)
	}
	if lookup, _ = authStore.Rights(id, "other"); lookup[RightMemRead] {
		t.Fatal("leaked to other home")
	}
	authStore.Revoke(id, "main", RightExec)
	if lookup, _ = authStore.Rights(id, "main"); lookup[RightExec] || !lookup[RightMemRead] {
		t.Fatalf("revoked: %v", lookup)
	}
	if err := authStore.Grant(id, "main", "all"); err == nil {
		t.Fatal("'all' accepted")
	}
}

func TestRightsTamper(t *testing.T) {
	authStore := testStore(t)
	id, _ := authStore.AddUser("alice", testPw, false)
	authStore.Grant(id, "main", RightMemRead)
	authStore.db.Exec(`UPDATE home_acl SET exec=1 WHERE user_id=?`, id)
	if _, err := authStore.Rights(id, "main"); !errors.Is(err, errTamper) || tamperEvents(t, authStore) != 1 {
		t.Fatalf("modified row: %v", err)
	}
	authStore.db.Exec(`UPDATE home_acl SET exec=0, sig=NULL WHERE user_id=?`, id)
	if _, err := authStore.Rights(id, "main"); !errors.Is(err, errTamper) {
		t.Fatalf("unsigned row: %v", err)
	}
}

func TestRightsRowCopy(t *testing.T) {
	authStore := testStore(t)
	number, _ := authStore.AddUser("alice", testPw, false)
	limit, _ := authStore.AddUser("bob", testPw, false)
	authStore.Grant(number, "main", RightExec)
	if _, err := authStore.db.Exec(`INSERT INTO home_acl(user_id,home_id,mem_read,mem_write,config_write,mem_sync,usage_read,sessions_read,exec,fs_read,fs_write,sig) SELECT ?,home_id,mem_read,mem_write,config_write,mem_sync,usage_read,sessions_read,exec,fs_read,fs_write,sig FROM home_acl WHERE user_id=?`, limit, number); err != nil {
		t.Fatalf("copie de ligne impossible : %v", err)
	}
	if _, err := authStore.Rights(limit, "main"); !errors.Is(err, errTamper) {
		t.Fatalf("copied to other user: %v", err)
	}
	if _, err := authStore.db.Exec(`INSERT INTO home_acl(user_id,home_id,mem_read,mem_write,config_write,mem_sync,usage_read,sessions_read,exec,fs_read,fs_write,sig) SELECT user_id,'perso',mem_read,mem_write,config_write,mem_sync,usage_read,sessions_read,exec,fs_read,fs_write,sig FROM home_acl WHERE user_id=? AND home_id='main'`, number); err != nil {
		t.Fatalf("copie de ligne impossible : %v", err)
	}
	if _, err := authStore.Rights(number, "perso"); !errors.Is(err, errTamper) {
		t.Fatalf("copied to other home: %v", err)
	}
}

func TestResetMFA(t *testing.T) {
	authStore := testStore(t)
	id, _ := authStore.AddUser("alice", testPw, false)
	authStore.db.Exec(`INSERT INTO totp(user_id,secret_enc,nonce) VALUES(?,x'00',x'00')`, id)
	authStore.db.Exec(`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at) VALUES(x'01',?,0,0,0)`, id)
	if err := authStore.ResetMFA(id); err != nil {
		t.Fatal(err)
	}
	var count int
	authStore.db.QueryRow(`SELECT (SELECT COUNT(*) FROM totp)+(SELECT COUNT(*) FROM sessions)`).Scan(&count)
	if count != 0 {
		t.Fatalf("%d rows left", count)
	}
}

func withStdin(t *testing.T, input string) {
	t.Helper()
	file, localFile, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	localFile.WriteString(input)
	localFile.Close()
	old := os.Stdin
	os.Stdin = file
	t.Cleanup(func() { os.Stdin = old; file.Close() })
}

func TestUserCLIGrantNeedsConfirmation(t *testing.T) {
	authStore := testStore(t)
	authStore.db.Close()
	t.Setenv("GLD_USER_PASSWORD_STDIN", "1")
	withStdin(t, testPw+"\n")
	if _, err := run(t, cliUser, "add", "--admin", "Alice"); err != nil {
		t.Fatal(err)
	}
	withStdin(t, "")
	if out, err := run(t, cliUser, "grant", "alice", "main", "exec"); err == nil || !isMsg(err, "srv.user.confirmationRequise") || !strings.Contains(out, "exec") {
		t.Fatalf("no confirmation: %v\n%s", err, out)
	}
	if _, err := run(t, cliUser, "grant", "alice", "main", "all"); err == nil {
		t.Fatal("'all' accepted")
	}
	if _, err := run(t, cliUser, "grant", "--yes", "alice", "main", "exec", "mem_read"); err != nil {
		t.Fatal(err)
	}
	s2, err := openAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.db.Close()
	user, _ := s2.UserByName("alice")
	if lookup, err := s2.Rights(user.ID, "main"); err != nil || !lookup[RightExec] || !lookup[RightMemRead] || !user.Admin {
		t.Fatalf("rights: %v %v", lookup, err)
	}
	if out, err := run(t, cliUser, "list"); err != nil || !strings.Contains(out, "alice") || strings.Contains(out, testPw) {
		t.Fatalf("list: %v\n%s", err, out)
	}
}

func TestUserConcurrentUpdates(t *testing.T) {
	authStore := testStore(t)
	id, _ := authStore.AddUser("alice", testPw, false)
	var wg sync.WaitGroup
	for number := 0; number < 2; number++ {
		wg.Add(1)
		go func(number int) {
			defer wg.Done()
			for index := 0; index < 20; index++ {
				var err error
				if number == 0 {
					err = authStore.SetDisabled(id, index%2 == 0)
				} else {
					err = authStore.SetPassword(id, testPw+strconv.Itoa(index))
				}
				if err != nil {
					t.Error(err)
				}
			}
		}(number)
	}
	wg.Wait()
	if _, err := authStore.UserByName("alice"); err != nil || tamperEvents(t, authStore) != 0 {
		t.Fatalf("after concurrent updates: %v, %d tamper events", err, tamperEvents(t, authStore))
	}
}

func TestGrantConcurrent(t *testing.T) {
	authStore := testStore(t)
	id, _ := authStore.AddUser("alice", testPw, false)
	var wg sync.WaitGroup
	for _, right := range allRights {
		wg.Add(1)
		go func(right Right) {
			defer wg.Done()
			if err := authStore.Grant(id, "main", right, RightMemRead); err != nil {
				t.Error(err)
			}
		}(right)
	}
	wg.Wait()
	lookup, err := authStore.Rights(id, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, right := range allRights {
		if !lookup[right] {
			t.Errorf("lost grant %s", right)
		}
	}
}

func TestAddUserConcurrent(t *testing.T) {
	authStore := testStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := authStore.AddUser("alice", testPw, false)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, errNameTaken):
			t.Errorf("unexpected: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d successes", ok)
	}
}

func TestUserOutOfRangeValues(t *testing.T) {
	authStore := testStore(t)
	id, _ := authStore.AddUser("alice", testPw, true)
	authStore.Grant(id, "main", RightExec)
	authStore.db.Exec(`UPDATE users SET is_admin=2 WHERE id=?`, id)
	if _, err := authStore.UserByName("alice"); !errors.Is(err, errTamper) || tamperEvents(t, authStore) != 1 {
		t.Fatalf("is_admin=2: %v", err)
	}
	authStore.db.Exec(`UPDATE home_acl SET exec=2 WHERE user_id=?`, id)
	if _, err := authStore.Rights(id, "main"); !errors.Is(err, errTamper) || tamperEvents(t, authStore) != 2 {
		t.Fatalf("exec=2: %v", err)
	}
	authStore.db.Exec(`UPDATE home_acl SET exec='x' WHERE user_id=?`, id)
	if _, err := authStore.Rights(id, "main"); !errors.Is(err, errTamper) {
		t.Fatalf("exec='x': %v", err)
	}
}

func TestUsersListTamperOnce(t *testing.T) {
	authStore := testStore(t)
	id, _ := authStore.AddUser("alice", testPw, false)
	authStore.AddUser("bob", testPw, false)
	authStore.db.Exec(`UPDATE users SET disabled=1 WHERE id=?`, id)
	users, bad, err := authStore.Users()
	if err != nil || len(users) != 2 || len(bad) != 1 || !bad[id] || tamperEvents(t, authStore) != 1 {
		t.Fatalf("list: %d users, bad=%v, err=%v, events=%d", len(users), bad, err, tamperEvents(t, authStore))
	}
}

func TestFsRightsGrantRevoke(t *testing.T) {
	authStore := testStore(t)
	number, err := authStore.AddUser("Alice", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := authStore.Grant(number, "main", RightFsRead, RightFsWrite); err != nil {
		t.Fatal(err)
	}
	lookup, err := authStore.Rights(number, "main")
	if err != nil || !lookup[RightFsRead] || !lookup[RightFsWrite] {
		t.Fatalf("après grant : %v %v", lookup, err)
	}
	if err := authStore.Revoke(number, "main", RightFsWrite); err != nil {
		t.Fatal(err)
	}
	if lookup, err = authStore.Rights(number, "main"); err != nil || !lookup[RightFsRead] || lookup[RightFsWrite] {
		t.Fatalf("après revoke : %v %v", lookup, err)
	}
}

func TestMigration8ResignsACL(t *testing.T) {
	authStore := testStore(t)
	number, err := authStore.AddUser("Alice", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := authStore.Grant(number, "main", RightMemRead); err != nil {
		t.Fatal(err)
	}
	if err := authStore.Grant(number, "other", RightExec); err != nil {
		t.Fatal(err)
	}
	old := func(home string, on ...string) []byte {
		return rowSig(authStore.key, "home_acl", strconv.FormatInt(number, 10), append([]string{home}, on...)...)
	}
	for _, query := range []string{`ALTER TABLE home_acl DROP COLUMN fs_read`, `ALTER TABLE home_acl DROP COLUMN fs_write`,
		`DROP TRIGGER sessions_cascade`, `DROP INDEX sessions_parent_idx`, `ALTER TABLE sessions DROP COLUMN parent_hash`, `DELETE FROM schema_version WHERE version>=8`} {
		if _, err := authStore.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := authStore.db.Exec(`UPDATE home_acl SET sig=? WHERE home_id='main'`, old("main", "1", "0", "0", "0", "0", "0", "0")); err != nil {
		t.Fatal(err)
	}
	// La ligne « other » garde une signature invalide : elle doit rester altérée.
	if _, err := authStore.db.Exec(`UPDATE home_acl SET sig=? WHERE home_id='other'`, []byte("bad")); err != nil {
		t.Fatal(err)
	}
	authStore.db.Close()
	s2, err := openAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.db.Close()
	lookup, err := s2.Rights(number, "main")
	if err != nil || !lookup[RightMemRead] || lookup[RightFsRead] || lookup[RightFsWrite] {
		t.Fatalf("droits après migration : %v %v", lookup, err)
	}
	if _, err := s2.Rights(number, "other"); err == nil {
		t.Fatal("ligne altérée re-signée")
	}
}

func TestMigration9KeepsSessions(t *testing.T) {
	authStore := testStore(t)
	number, err := authStore.AddUser("Alice", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := authStore.NewSession(number, "203.0.113.5", "ua")
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`DROP TRIGGER sessions_cascade`, `DROP INDEX sessions_parent_idx`, `ALTER TABLE sessions DROP COLUMN parent_hash`, `DELETE FROM schema_version WHERE version>=9`} {
		if _, err := authStore.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	authStore.db.Close()
	s2, err := openAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.db.Close()
	if principal, err := s2.Session(cookie); err != nil || principal == nil {
		t.Fatalf("session perdue par la migration 9 : %v %v", principal, err)
	}
}
