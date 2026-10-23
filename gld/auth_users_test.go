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
	m, tt := argonMemKiB, argonTime
	argonMemKiB, argonTime = 64, 1
	t.Cleanup(func() { argonMemKiB, argonTime = m, tt })
	s, err := openAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	return s
}

const testPw = "correct horse battery"

func tamperEvents(t *testing.T, s *authStore) int {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='tamper'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAddUserNameUnique(t *testing.T) {
	s := testStore(t)
	if on, err := s.HasUsers(); on || err != nil {
		t.Fatal("empty store has users", err)
	}
	if _, err := s.AddUser("Alice", testPw, false); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"alice", " ALICE ", "Ａｌｉｃｅ"} {
		if _, err := s.AddUser(n, testPw, false); !errors.Is(err, errNameTaken) {
			t.Fatalf("%q: %v", n, err)
		}
	}
	u, err := s.UserByName("ALICE")
	if err != nil || u.Name != "alice" || !checkPassword(u.PwHash, testPw) {
		t.Fatalf("lookup: %+v %v", u, err)
	}
	if on, err := s.HasUsers(); !on || err != nil {
		t.Fatal("HasUsers", on, err)
	}
	if _, err := s.UserByName("bob"); !errors.Is(err, errNoUser) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := s.AddUser("a b", testPw, false); !errors.Is(err, errBadName) {
		t.Fatalf("space: %v", err)
	}
}

func TestUserTamper(t *testing.T) {
	s := testStore(t)
	id, _ := s.AddUser("alice", testPw, false)
	if err := s.SetDisabled(id, true); err != nil {
		t.Fatal(err)
	}
	if u, err := s.UserByName("alice"); err != nil || !u.Disabled {
		t.Fatalf("disabled: %+v %v", u, err)
	}
	s.db.Exec(`UPDATE users SET is_admin=1 WHERE id=?`, id)
	if _, err := s.UserByName("alice"); !errors.Is(err, errTamper) || tamperEvents(t, s) != 1 {
		t.Fatalf("tamper: %v", err)
	}
	if err := s.SetPassword(id, testPw+"x"); !errors.Is(err, errTamper) {
		t.Fatalf("re-sign of tampered row: %v", err)
	}
}

func TestGrantDefaultNone(t *testing.T) {
	s := testStore(t)
	id, _ := s.AddUser("alice", testPw, true)
	r, err := s.Rights(id, "main")
	if err != nil || len(r) != 0 {
		t.Fatalf("default: %v %v", r, err)
	}
	if err := s.Grant(id, "main", RightMemRead, RightExec); err != nil {
		t.Fatal(err)
	}
	if r, _ = s.Rights(id, "main"); !r[RightMemRead] || !r[RightExec] || r[RightMemWrite] {
		t.Fatalf("granted: %v", r)
	}
	if r, _ = s.Rights(id, "other"); r[RightMemRead] {
		t.Fatal("leaked to other home")
	}
	s.Revoke(id, "main", RightExec)
	if r, _ = s.Rights(id, "main"); r[RightExec] || !r[RightMemRead] {
		t.Fatalf("revoked: %v", r)
	}
	if err := s.Grant(id, "main", "all"); err == nil {
		t.Fatal("'all' accepted")
	}
}

func TestRightsTamper(t *testing.T) {
	s := testStore(t)
	id, _ := s.AddUser("alice", testPw, false)
	s.Grant(id, "main", RightMemRead)
	s.db.Exec(`UPDATE home_acl SET exec=1 WHERE user_id=?`, id)
	if _, err := s.Rights(id, "main"); !errors.Is(err, errTamper) || tamperEvents(t, s) != 1 {
		t.Fatalf("modified row: %v", err)
	}
	s.db.Exec(`UPDATE home_acl SET exec=0, sig=NULL WHERE user_id=?`, id)
	if _, err := s.Rights(id, "main"); !errors.Is(err, errTamper) {
		t.Fatalf("unsigned row: %v", err)
	}
}

func TestRightsRowCopy(t *testing.T) {
	s := testStore(t)
	a, _ := s.AddUser("alice", testPw, false)
	b, _ := s.AddUser("bob", testPw, false)
	s.Grant(a, "main", RightExec)
	s.db.Exec(`INSERT INTO home_acl SELECT ?,home_id,mem_read,mem_write,config_write,mem_sync,usage_read,sessions_read,exec,sig FROM home_acl WHERE user_id=?`, b, a)
	if _, err := s.Rights(b, "main"); !errors.Is(err, errTamper) {
		t.Fatalf("copied to other user: %v", err)
	}
	s.db.Exec(`INSERT INTO home_acl SELECT user_id,'perso',mem_read,mem_write,config_write,mem_sync,usage_read,sessions_read,exec,sig FROM home_acl WHERE user_id=? AND home_id='main'`, a)
	if _, err := s.Rights(a, "perso"); !errors.Is(err, errTamper) {
		t.Fatalf("copied to other home: %v", err)
	}
}

func TestResetMFA(t *testing.T) {
	s := testStore(t)
	id, _ := s.AddUser("alice", testPw, false)
	s.db.Exec(`INSERT INTO totp(user_id,secret_enc,nonce) VALUES(?,x'00',x'00')`, id)
	s.db.Exec(`INSERT INTO sessions(id_hash,user_id,created_at,last_seen_at,expires_at) VALUES(x'01',?,0,0,0)`, id)
	if err := s.ResetMFA(id); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM totp)+(SELECT COUNT(*) FROM sessions)`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d rows left", n)
	}
}

func withStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString(input)
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })
}

func TestUserCLIGrantNeedsConfirmation(t *testing.T) {
	s := testStore(t)
	s.db.Close()
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
	u, _ := s2.UserByName("alice")
	if r, err := s2.Rights(u.ID, "main"); err != nil || !r[RightExec] || !r[RightMemRead] || !u.Admin {
		t.Fatalf("rights: %v %v", r, err)
	}
	if out, err := run(t, cliUser, "list"); err != nil || !strings.Contains(out, "alice") || strings.Contains(out, testPw) {
		t.Fatalf("list: %v\n%s", err, out)
	}
}

func TestUserConcurrentUpdates(t *testing.T) {
	s := testStore(t)
	id, _ := s.AddUser("alice", testPw, false)
	var wg sync.WaitGroup
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				var err error
				if g == 0 {
					err = s.SetDisabled(id, i%2 == 0)
				} else {
					err = s.SetPassword(id, testPw+strconv.Itoa(i))
				}
				if err != nil {
					t.Error(err)
				}
			}
		}(g)
	}
	wg.Wait()
	if _, err := s.UserByName("alice"); err != nil || tamperEvents(t, s) != 0 {
		t.Fatalf("after concurrent updates: %v, %d tamper events", err, tamperEvents(t, s))
	}
}

func TestGrantConcurrent(t *testing.T) {
	s := testStore(t)
	id, _ := s.AddUser("alice", testPw, false)
	var wg sync.WaitGroup
	for _, r := range allRights {
		wg.Add(1)
		go func(r Right) {
			defer wg.Done()
			if err := s.Grant(id, "main", r); err != nil {
				t.Error(err)
			}
		}(r)
	}
	wg.Wait()
	m, err := s.Rights(id, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range allRights {
		if !m[r] {
			t.Errorf("lost grant %s", r)
		}
	}
}

func TestAddUserConcurrent(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.AddUser("alice", testPw, false)
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
	s := testStore(t)
	id, _ := s.AddUser("alice", testPw, true)
	s.Grant(id, "main", RightExec)
	s.db.Exec(`UPDATE users SET is_admin=2 WHERE id=?`, id)
	if _, err := s.UserByName("alice"); !errors.Is(err, errTamper) || tamperEvents(t, s) != 1 {
		t.Fatalf("is_admin=2: %v", err)
	}
	s.db.Exec(`UPDATE home_acl SET exec=2 WHERE user_id=?`, id)
	if _, err := s.Rights(id, "main"); !errors.Is(err, errTamper) || tamperEvents(t, s) != 2 {
		t.Fatalf("exec=2: %v", err)
	}
	s.db.Exec(`UPDATE home_acl SET exec='x' WHERE user_id=?`, id)
	if _, err := s.Rights(id, "main"); !errors.Is(err, errTamper) {
		t.Fatalf("exec='x': %v", err)
	}
}

func TestUsersListTamperOnce(t *testing.T) {
	s := testStore(t)
	id, _ := s.AddUser("alice", testPw, false)
	s.AddUser("bob", testPw, false)
	s.db.Exec(`UPDATE users SET disabled=1 WHERE id=?`, id)
	users, bad, err := s.Users()
	if err != nil || len(users) != 2 || len(bad) != 1 || !bad[id] || tamperEvents(t, s) != 1 {
		t.Fatalf("list: %d users, bad=%v, err=%v, events=%d", len(users), bad, err, tamperEvents(t, s))
	}
}
