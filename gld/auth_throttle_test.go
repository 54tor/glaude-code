package main

import (
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func testThrottle(t *testing.T) (*throttle, *authStore, *time.Time) {
	t.Helper()
	s := testStore(t)
	clock := time.Unix(1_800_000_000, 0)
	th := newThrottle(s.db)
	th.now = func() time.Time { return clock }
	return th, s, &clock
}

func TestBackoff(t *testing.T) {
	th, _, _ := testThrottle(t)
	want := map[int]time.Duration{2: 0, 3: time.Second, 4: 2 * time.Second, 8: 32 * time.Second}
	for n := 1; n <= 8; n++ {
		th.Fail("Alice", "10.0.0.1", "password")
		if w, ok := want[n]; ok {
			got, blocked := th.Check("alice", "10.0.0.1")
			if got != w || blocked {
				t.Fatalf("%d échecs : attente %v bloqué %v, voulu %v", n, got, blocked, w)
			}
		}
	}
	// Plafond, observé sur la clé ip: pour ne pas déclencher le verrou user:.
	for n := 0; n < 20; n++ {
		th.Fail(fmt.Sprintf("u%d", n), "10.0.0.9", "password")
	}
	if got, blocked := th.Check("autre", "10.0.0.9"); got != thMaxDelay || blocked {
		t.Fatalf("plafond : %v %v", got, blocked)
	}
}

func TestBackoffWindowResets(t *testing.T) {
	th, _, clock := testThrottle(t)
	for n := 0; n < 5; n++ {
		th.Fail("bob", "10.0.0.1", "password")
	}
	*clock = clock.Add(thWindow + time.Second)
	if w, _ := th.Check("bob", "10.0.0.1"); w != 0 {
		t.Fatalf("hors fenêtre : %v", w)
	}
	th.Fail("bob", "10.0.0.1", "password")
	var fails int64
	th.db.QueryRow(`SELECT fails FROM throttle WHERE key='user:bob'`).Scan(&fails)
	if fails != 1 {
		t.Fatalf("compteur après fenêtre : %d", fails)
	}
}

func TestUserLockUnknownIP(t *testing.T) {
	th, s, clock := testThrottle(t)
	id, err := s.AddUser("carol", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Exec(`INSERT INTO auth_events(at,user_id,ip,kind,outcome) VALUES(?,?,?,?,?)`,
		clock.Add(-24*time.Hour).Unix(), id, "192.0.2.7", evLogin, evSuccess)
	if !th.KnownIP(id, "192.0.2.7") || th.KnownIP(id, "198.51.100.1") {
		t.Fatal("KnownIP")
	}
	for n := 0; n < thLockAfter; n++ {
		th.Fail("Carol", fmt.Sprintf("203.0.113.%d", n), "password")
	}
	w, blocked := th.Check("carol", "198.51.100.1")
	if !blocked || w != thMaxDelay {
		t.Fatalf("IP inconnue : %v %v", w, blocked)
	}
	if w, blocked := th.Check("carol", "192.0.2.7"); blocked || w != 0 {
		t.Fatalf("IP connue : %v %v", w, blocked)
	}
	// Un nom inexistant se verrouille de la même façon.
	for n := 0; n < thLockAfter; n++ {
		th.Fail("ghost", fmt.Sprintf("203.0.113.%d", n), "password")
	}
	if _, blocked := th.Check("ghost", "198.51.100.1"); !blocked {
		t.Fatal("nom inconnu non verrouillé")
	}
	var locks int
	s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='lock'`).Scan(&locks)
	if locks != 2 {
		t.Fatalf("verrous journalisés : %d", locks)
	}
	*clock = clock.Add(thLockFor + time.Second)
	if _, blocked := th.Check("carol", "198.51.100.1"); blocked {
		t.Fatal("verrou non levé après 30 min")
	}
}

func TestMFAThreshold(t *testing.T) {
	th, _, _ := testThrottle(t)
	for n := 0; n < thMFALimit-1; n++ {
		th.Fail("dave", "10.0.0.1", "mfa")
	}
	if _, blocked := th.Check("dave", "10.0.0.1"); blocked {
		t.Fatal("bloqué trop tôt")
	}
	th.Fail("dave", "10.0.0.1", "mfa")
	th.Success("dave", "10.0.0.1")
	if _, blocked := th.Check("dave", "10.0.0.1"); !blocked {
		t.Fatal("5 échecs mfa non bloqués")
	}
}

func TestSuccessClears(t *testing.T) {
	th, _, _ := testThrottle(t)
	for n := 0; n < 4; n++ {
		th.Fail("erin", "10.0.0.1", "password")
	}
	th.Success("erin", "10.0.0.1")
	var keys []string
	rows, _ := th.db.Query(`SELECT key FROM throttle ORDER BY key`)
	for rows.Next() {
		var k string
		rows.Scan(&k)
		keys = append(keys, k)
	}
	rows.Close()
	if len(keys) != 1 || keys[0] != "ip:10.0.0.1" {
		t.Fatalf("clés restantes : %v", keys)
	}
}

func TestGlobalCap(t *testing.T) {
	th, _, clock := testThrottle(t)
	for n := 0; n < thGlobalMax; n++ {
		if w, _ := th.Check(fmt.Sprintf("u%d", n), "10.0.0.1"); w != 0 {
			t.Fatalf("tentative %d attend %v", n+1, w)
		}
		*clock = clock.Add(500 * time.Millisecond)
	}
	w, blocked := th.Check("x", "10.0.0.2")
	if w != 30*time.Second || blocked {
		t.Fatalf("61e tentative : %v %v", w, blocked)
	}
	*clock = clock.Add(w)
	if w, _ := th.Check("x", "10.0.0.2"); w != 0 {
		t.Fatalf("après glissement : %v", w)
	}
}

func TestThrottleConcurrent(t *testing.T) {
	th, _, _ := testThrottle(t)
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); th.Fail("frank", "10.0.0.1", "password") }()
	}
	wg.Wait()
	var fails int64
	th.db.QueryRow(`SELECT fails FROM throttle WHERE key='pair:frank|10.0.0.1'`).Scan(&fails)
	if fails != 20 {
		t.Fatalf("compteur : %d", fails)
	}
}

func TestClientIPXFF(t *testing.T) {
	r := httptest.NewRequest("POST", "/auth/login", nil)
	r.RemoteAddr = "10.1.2.3:4567"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.5")
	t.Setenv("GLD_TRUSTED_PROXIES", "")
	if got := clientIP(r); got != "10.1.2.3" {
		t.Fatalf("sans proxy de confiance : %s", got)
	}
	t.Setenv("GLD_TRUSTED_PROXIES", "192.0.2.1, 10.0.0.0/8")
	if got := clientIP(r); got != "203.0.113.5" {
		t.Fatalf("avec proxy de confiance : %s", got)
	}
	r.RemoteAddr = "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:443"
	if got := clientIP(r); got != "2001:db8:1:2::/64" {
		t.Fatalf("IPv6 : %s", got)
	}
}
