package main

import (
	"fmt"
	"net/http/httptest"
	"strconv"
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
	th, _, clock := testThrottle(t)
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
	// Le délai court depuis le dernier échec.
	*clock = clock.Add(31 * time.Second)
	if got, _ := th.Check("alice", "10.0.0.1"); got != time.Second {
		t.Fatalf("31 s après le 8e échec : %v", got)
	}
	*clock = clock.Add(time.Second)
	if got, blocked := th.Check("alice", "10.0.0.1"); got != 0 || blocked {
		t.Fatalf("délai écoulé : %v %v", got, blocked)
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
	if _, err := s.AddUser("carol", testPw, false); err != nil {
		t.Fatal(err)
	}
	if err := th.Success("Carol", "192.0.2.7"); err != nil {
		t.Fatal(err)
	}
	// Check ne doit jamais lire la table users.
	if _, err := s.db.Exec(`ALTER TABLE users RENAME TO users_hidden`); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < thLockAfter; n++ {
		if err := th.Fail("Carol", fmt.Sprintf("203.0.113.%d", n), "password"); err != nil {
			t.Fatal(err)
		}
		th.Fail("ghost", fmt.Sprintf("203.0.113.%d", n), "password")
	}
	w, blocked := th.Check("carol", "198.51.100.1")
	if !blocked || w != thMaxDelay {
		t.Fatalf("IP inconnue : %v %v", w, blocked)
	}
	gw, gb := th.Check("ghost", "198.51.100.1")
	if gw != w || gb != blocked {
		t.Fatalf("nom inconnu %v %v, nom existant %v %v", gw, gb, w, blocked)
	}
	if w, blocked := th.Check("carol", "192.0.2.7"); blocked || w != 0 {
		t.Fatalf("IP connue : %v %v", w, blocked)
	}
	var locks int
	s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='lock' AND outcome='locked'`).Scan(&locks)
	if locks != 2 {
		t.Fatalf("verrous journalisés : %d", locks)
	}
	*clock = clock.Add(thLockFor + time.Second)
	if _, blocked := th.Check("carol", "198.51.100.1"); blocked {
		t.Fatal("verrou non levé après 30 min")
	}
}

func TestUserLockKnownIPExpires(t *testing.T) {
	th, _, clock := testThrottle(t)
	th.Success("carol", "192.0.2.7")
	*clock = clock.Add(thKnownFor + time.Hour)
	for n := 0; n < thLockAfter; n++ {
		th.Fail("carol", fmt.Sprintf("203.0.113.%d", n), "password")
	}
	if _, blocked := th.Check("carol", "192.0.2.7"); !blocked {
		t.Fatal("marqueur de plus de 30 jours encore pris en compte")
	}
	th.Success("dave", "10.0.0.1")
	var n int
	th.db.QueryRow(`SELECT COUNT(*) FROM throttle WHERE key='known:carol|192.0.2.7'`).Scan(&n)
	if n != 0 {
		t.Fatal("marqueur expiré non purgé")
	}
}

func TestThrottleClosedDB(t *testing.T) {
	th, s, _ := testThrottle(t)
	s.db.Close()
	if _, blocked := th.Check("x", "10.0.0.1"); !blocked {
		t.Fatal("Check ouvert sur base fermée")
	}
	if th.Fail("x", "10.0.0.1", "password") == nil {
		t.Fatal("Fail sans erreur sur base fermée")
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
	if len(keys) != 2 || keys[0] != "ip:10.0.0.1" || keys[1] != "known:erin|10.0.0.1" {
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
	r := httptest.NewRequest("POST", "/v1/auth/sessions", nil)
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

func TestClientIPXFFSpoof(t *testing.T) {
	t.Setenv("GLD_TRUSTED_PROXIES", "192.0.2.1,192.0.2.2")
	r := httptest.NewRequest("POST", "/v1/auth/sessions", nil)
	r.RemoteAddr = "198.51.100.9:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIP(r); got != "198.51.100.9" {
		t.Fatalf("pair non fiable : %s", got)
	}
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.5, 192.0.2.2")
	if got := clientIP(r); got != "203.0.113.5" {
		t.Fatalf("chaîne : %s", got)
	}
}

// Essai lent : cinq échecs par quart d'heure ne déclenchent jamais le verrou user:, la suite d'échecs si.
func TestStreakLock(t *testing.T) {
	th, s, clock := testThrottle(t)
	n := 0
	for round := 0; round < 12 && n < thStreakLock; round++ {
		for i := 0; i < 5; i++ {
			if _, blocked := th.Check("erin", "198.51.100.1"); blocked {
				t.Fatalf("bloqué après %d échecs", n)
			}
			stage := "password"
			if i%2 == 1 {
				stage = "mfa"
			}
			th.Fail("erin", fmt.Sprintf("203.0.113.%d", n), stage)
			n++
		}
		*clock = clock.Add(thWindow)
	}
	*clock = clock.Add(-thWindow)
	if _, blocked := th.Check("erin", "198.51.100.1"); !blocked {
		t.Fatalf("pas de verrou après %d échecs consécutifs", n)
	}
	var ev int
	s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='lock' AND outcome='streak_10'`).Scan(&ev)
	if ev != 1 {
		t.Fatalf("événement de verrou : %d", ev)
	}
	*clock = clock.Add(thLockFor + time.Second)
	if _, blocked := th.Check("erin", "198.51.100.1"); blocked {
		t.Fatal("verrou de 30 min non levé")
	}
	for ; n < thStreakLong; n++ {
		th.Fail("erin", "203.0.113.200", "mfa")
		*clock = clock.Add(thLockFor + time.Second)
	}
	*clock = clock.Add(-thLockFor)
	if w, blocked := th.Check("erin", "198.51.100.1"); !blocked || w != thMaxDelay {
		t.Fatalf("verrou long : %v %v", w, blocked)
	}
	*clock = clock.Add(thStreakLongFor)
	if err := th.Success("erin", "203.0.113.200"); err != nil {
		t.Fatal(err)
	}
	var left int
	s.db.QueryRow(`SELECT COUNT(*) FROM throttle WHERE key='streak:erin'`).Scan(&left)
	if left != 0 {
		t.Fatal("suite non remise à zéro par un succès")
	}
}

func TestThrottlePurge(t *testing.T) {
	th, s, clock := testThrottle(t)
	th.Fail("x", "10.0.0.1", "password")
	th.Success("y", "10.0.0.2")
	*clock = clock.Add(thWindow + time.Second)
	if n, err := th.Purge(); err != nil || n != 3 {
		t.Fatalf("purge fenêtre : %d %v", n, err)
	}
	*clock = clock.Add(thStreakKeep)
	if n, err := th.Purge(); err != nil || n != 1 {
		t.Fatalf("purge suite : %d %v", n, err)
	}
	*clock = clock.Add(thKnownFor)
	if n, err := th.Purge(); err != nil || n != 1 {
		t.Fatalf("purge marqueur connu : %d %v", n, err)
	}
	var left int
	s.db.QueryRow(`SELECT COUNT(*) FROM throttle`).Scan(&left)
	if left != 0 {
		t.Fatalf("restant : %d", left)
	}
}

func TestIPBan(t *testing.T) {
	th, s, clock := testThrottle(t)
	if _, err := s.AddUser("dave", testPw, false); err != nil {
		t.Fatal(err)
	}
	if err := th.Success("dave", "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	// Une IP extérieure qui rate des noms différents est bannie, même si elle a déjà réussi une connexion.
	for n := 0; n < thBanAfter; n++ {
		if err := th.Fail(fmt.Sprintf("inconnu%d", n), "203.0.113.9", "password"); err != nil {
			t.Fatal(err)
		}
	}
	if _, blocked := th.Pending("dave", "203.0.113.9"); !blocked {
		t.Fatal("IP extérieure non bannie")
	}
	if _, blocked := th.Pending("dave", "198.51.100.1"); blocked {
		t.Fatal("une autre IP est bannie")
	}
	var bans int
	s.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='lock' AND outcome='ban'`).Scan(&bans)
	if bans != 1 {
		t.Fatalf("bans journalisés : %d", bans)
	}
	*clock = clock.Add(thBanFor + time.Second)
	if _, blocked := th.Pending("dave", "203.0.113.9"); blocked {
		t.Fatal("ban non levé")
	}
	// Réseau local et poste : jamais bannis.
	loop6 := httptest.NewRequest("GET", "/", nil)
	loop6.RemoteAddr = "[::1]:4242" // tel que clientIP le réduit, pas l'adresse brute
	for _, ip := range []string{"127.0.0.1", "192.168.1.20", "10.0.0.5", "fd00::/64", clientIP(loop6)} {
		for n := 0; n < thBanAfter+2; n++ {
			th.Fail("x", ip, "password")
		}
		if externalIP(ip) {
			t.Fatalf("%s vue comme extérieure", ip)
		}
		var locked int64
		s.db.QueryRow(`SELECT locked_until FROM throttle WHERE key=?`, "ip:"+ip).Scan(&locked)
		if locked != 0 {
			t.Fatalf("%s bannie", ip)
		}
	}
}

func TestBanAlerts(t *testing.T) {
	g := newAuthRig(t)
	secret := g.enrolTOTP()
	c, _ := g.full(secret) // alice, compte ordinaire
	if w := g.do("GET", "/v1/auth/alerts", "", "", nil); w.Code == 200 {
		t.Fatal("alertes servies sans session")
	}
	for n := 0; n < thBanAfter; n++ {
		g.th.Fail(fmt.Sprintf("x%d", n), "203.0.113.50", "password")
	}
	b := jbody(t, g.do("GET", "/v1/auth/alerts?since=0", c, "", nil))
	bans, _ := b["bans"].([]any)
	if len(bans) != 1 || bans[0].(map[string]any)["ip"] != "" {
		t.Fatalf("un compte ordinaire voit l'adresse : %v", b)
	}
	admin, err := g.s.AddUser("root", testPw, true)
	if err != nil {
		t.Fatal(err)
	}
	ac, _ := fullSession(t, g.s, admin)
	ab := jbody(t, g.do("GET", "/v1/auth/alerts?since=0", ac, "", nil))
	if bans, _ := ab["bans"].([]any); len(bans) != 1 || bans[0].(map[string]any)["ip"] != "203.0.113.50" {
		t.Fatalf("alertes admin : %v", ab)
	}
	now := int64(b["now"].(float64))
	if b := jbody(t, g.do("GET", fmt.Sprintf("/v1/auth/alerts?since=%d", now+1), c, "", nil)); len(b["bans"].([]any)) != 0 {
		t.Fatalf("ban déjà vu : %v", b)
	}
}

func TestMFAParAdresse(t *testing.T) {
	th, _, _ := testThrottle(t)
	for i := 0; i < 3; i++ {
		th.Fail("eve", "203.0.113.4", "mfa")
	}
	th.Success("eve", "203.0.113.4")
	for i := 0; i < 2; i++ {
		th.Fail("eve", "203.0.113.4", "mfa")
	}
	if _, blocked := th.Pending("eve", "203.0.113.4"); blocked {
		t.Fatal("fautes d'avant la connexion réussie encore comptées")
	}
	// Réparti sur plusieurs adresses, le compteur global du compte finit par bloquer partout.
	for i := 0; i < thMFAGlobal; i++ {
		th.Fail("eve", "198.51.100."+strconv.Itoa(i), "mfa")
	}
	if _, blocked := th.Pending("eve", "192.0.2.1"); !blocked {
		t.Fatal("plafond global du second facteur non appliqué")
	}
}
