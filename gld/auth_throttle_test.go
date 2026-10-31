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
	localAuthStore := testStore(t)
	clock := time.Unix(1_800_000_000, 0)
	th := newThrottle(localAuthStore.db)
	th.now = func() time.Time { return clock }
	return th, localAuthStore, &clock
}

func TestBackoff(t *testing.T) {
	th, _, clock := testThrottle(t)
	want := map[int]time.Duration{2: 0, 3: time.Second, 4: 2 * time.Second, 8: 32 * time.Second}
	for count := 1; count <= 8; count++ {
		th.Fail("Alice", "10.0.0.1", "password")
		if duration, ok := want[count]; ok {
			got, blocked := th.Check("alice", "10.0.0.1")
			if got != duration || blocked {
				t.Fatalf("%d échecs : attente %v bloqué %v, voulu %v", count, got, blocked, duration)
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
	for count := 0; count < 20; count++ {
		th.Fail(fmt.Sprintf("u%d", count), "10.0.0.9", "password")
	}
	if got, blocked := th.Check("autre", "10.0.0.9"); got != thMaxDelay || blocked {
		t.Fatalf("plafond : %v %v", got, blocked)
	}
}

func TestBackoffWindowResets(t *testing.T) {
	th, _, clock := testThrottle(t)
	for count := 0; count < 5; count++ {
		th.Fail("bob", "10.0.0.1", "password")
	}
	*clock = clock.Add(thWindow + time.Second)
	if duration, _ := th.Check("bob", "10.0.0.1"); duration != 0 {
		t.Fatalf("hors fenêtre : %v", duration)
	}
	th.Fail("bob", "10.0.0.1", "password")
	var fails int64
	th.db.QueryRow(`SELECT fails FROM throttle WHERE key='user:bob'`).Scan(&fails)
	if fails != 1 {
		t.Fatalf("compteur après fenêtre : %d", fails)
	}
}

func TestUserLockUnknownIP(t *testing.T) {
	th, authStore, clock := testThrottle(t)
	if _, err := authStore.AddUser("carol", testPw, false); err != nil {
		t.Fatal(err)
	}
	if err := th.Success("Carol", "192.0.2.7"); err != nil {
		t.Fatal(err)
	}
	// Check ne doit jamais lire la table users.
	if _, err := authStore.db.Exec(`ALTER TABLE users RENAME TO users_hidden`); err != nil {
		t.Fatal(err)
	}
	for count := 0; count < thLockAfter; count++ {
		if err := th.Fail("Carol", fmt.Sprintf("203.0.113.%d", count), "password"); err != nil {
			t.Fatal(err)
		}
		th.Fail("ghost", fmt.Sprintf("203.0.113.%d", count), "password")
	}
	duration, blocked := th.Check("carol", "198.51.100.1")
	if !blocked || duration != thMaxDelay {
		t.Fatalf("IP inconnue : %v %v", duration, blocked)
	}
	gw, gb := th.Check("ghost", "198.51.100.1")
	if gw != duration || gb != blocked {
		t.Fatalf("nom inconnu %v %v, nom existant %v %v", gw, gb, duration, blocked)
	}
	if duration, blocked := th.Check("carol", "192.0.2.7"); blocked || duration != 0 {
		t.Fatalf("IP connue : %v %v", duration, blocked)
	}
	var locks int
	authStore.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='lock' AND outcome='locked'`).Scan(&locks)
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
	for count := 0; count < thLockAfter; count++ {
		th.Fail("carol", fmt.Sprintf("203.0.113.%d", count), "password")
	}
	if _, blocked := th.Check("carol", "192.0.2.7"); !blocked {
		t.Fatal("marqueur de plus de 30 jours encore pris en compte")
	}
	th.Success("dave", "10.0.0.1")
	var localCount int
	th.db.QueryRow(`SELECT COUNT(*) FROM throttle WHERE key='known:carol|192.0.2.7'`).Scan(&localCount)
	if localCount != 0 {
		t.Fatal("marqueur expiré non purgé")
	}
}

func TestThrottleClosedDB(t *testing.T) {
	th, authStore, _ := testThrottle(t)
	authStore.db.Close()
	if _, blocked := th.Check("x", "10.0.0.1"); !blocked {
		t.Fatal("Check ouvert sur base fermée")
	}
	if th.Fail("x", "10.0.0.1", "password") == nil {
		t.Fatal("Fail sans erreur sur base fermée")
	}
}

func TestMFAThreshold(t *testing.T) {
	th, _, _ := testThrottle(t)
	for count := 0; count < thMFALimit-1; count++ {
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
	for count := 0; count < 4; count++ {
		th.Fail("erin", "10.0.0.1", "password")
	}
	th.Success("erin", "10.0.0.1")
	var keys []string
	rows, _ := th.db.Query(`SELECT key FROM throttle ORDER BY key`)
	for rows.Next() {
		var key string
		rows.Scan(&key)
		keys = append(keys, key)
	}
	rows.Close()
	if len(keys) != 2 || keys[0] != "ip:10.0.0.1" || keys[1] != "known:erin|10.0.0.1" {
		t.Fatalf("clés restantes : %v", keys)
	}
}

func TestGlobalCap(t *testing.T) {
	th, _, clock := testThrottle(t)
	for count := 0; count < thGlobalMax; count++ {
		if duration, _ := th.Check(fmt.Sprintf("u%d", count), "10.0.0.1"); duration != 0 {
			t.Fatalf("tentative %d attend %v", count+1, duration)
		}
		*clock = clock.Add(500 * time.Millisecond)
	}
	localDuration, blocked := th.Check("x", "10.0.0.2")
	if localDuration != 30*time.Second || blocked {
		t.Fatalf("61e tentative : %v %v", localDuration, blocked)
	}
	*clock = clock.Add(localDuration)
	if duration, _ := th.Check("x", "10.0.0.2"); duration != 0 {
		t.Fatalf("après glissement : %v", duration)
	}
}

// Des noms inventés venus de nombreuses adresses saturent le plafond global, sans bloquer un compte déjà connecté depuis son adresse.
func TestGlobalCapSparesKnownPair(t *testing.T) {
	th, _, _ := testThrottle(t)
	if err := th.Success("grace", "10.0.0.9"); err != nil {
		t.Fatal(err)
	}
	for count := 0; count < thGlobalMax; count++ {
		th.Check(fmt.Sprintf("inconnu%d", count), fmt.Sprintf("192.0.2.%d", count))
	}
	if duration, _ := th.Check("x", "10.0.0.2"); duration == 0 {
		t.Fatal("plafond global non atteint")
	}
	if duration, blocked := th.Check("grace", "10.0.0.9"); duration != 0 || blocked {
		t.Fatalf("couple connu bloqué par le plafond global : %v %v", duration, blocked)
	}
	if duration, _ := th.Check("grace", "10.0.0.10"); duration == 0 {
		t.Error("même compte, autre adresse : le plafond global doit s'appliquer")
	}
}

func TestThrottleConcurrent(t *testing.T) {
	th, _, _ := testThrottle(t)
	var wg sync.WaitGroup
	for count := 0; count < 20; count++ {
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
	request := httptest.NewRequest("POST", "/v1/auth/sessions", nil)
	request.RemoteAddr = "10.1.2.3:4567"
	request.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.5")
	t.Setenv("GLD_TRUSTED_PROXIES", "")
	if got := clientIP(request); got != "10.1.2.3" {
		t.Fatalf("sans proxy de confiance : %s", got)
	}
	t.Setenv("GLD_TRUSTED_PROXIES", "192.0.2.1, 10.0.0.0/8")
	if got := clientIP(request); got != "203.0.113.5" {
		t.Fatalf("avec proxy de confiance : %s", got)
	}
	request.RemoteAddr = "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:443"
	if got := clientIP(request); got != "2001:db8:1:2::/64" {
		t.Fatalf("IPv6 : %s", got)
	}
}

func TestClientIPXFFSpoof(t *testing.T) {
	t.Setenv("GLD_TRUSTED_PROXIES", "192.0.2.1,192.0.2.2")
	request := httptest.NewRequest("POST", "/v1/auth/sessions", nil)
	request.RemoteAddr = "198.51.100.9:1234"
	request.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIP(request); got != "198.51.100.9" {
		t.Fatalf("pair non fiable : %s", got)
	}
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.5, 192.0.2.2")
	if got := clientIP(request); got != "203.0.113.5" {
		t.Fatalf("chaîne : %s", got)
	}
}

// Essai lent : cinq échecs par quart d'heure ne déclenchent jamais le verrou user:, la suite d'échecs si.
func TestStreakLock(t *testing.T) {
	th, authStore, clock := testThrottle(t)
	count := 0
	for round := 0; round < 12 && count < thStreakLock; round++ {
		for index := 0; index < 5; index++ {
			if _, blocked := th.Check("erin", "198.51.100.1"); blocked {
				t.Fatalf("bloqué après %d échecs", count)
			}
			stage := "password"
			if index%2 == 1 {
				stage = "mfa"
			}
			th.Fail("erin", fmt.Sprintf("203.0.113.%d", count), stage)
			count++
		}
		*clock = clock.Add(thWindow)
	}
	*clock = clock.Add(-thWindow)
	if _, blocked := th.Check("erin", "198.51.100.1"); !blocked {
		t.Fatalf("pas de verrou après %d échecs consécutifs", count)
	}
	var ev int
	authStore.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='lock' AND outcome='streak_10'`).Scan(&ev)
	if ev != 1 {
		t.Fatalf("événement de verrou : %d", ev)
	}
	*clock = clock.Add(thLockFor + time.Second)
	if _, blocked := th.Check("erin", "198.51.100.1"); blocked {
		t.Fatal("verrou de 30 min non levé")
	}
	for ; count < thStreakLong; count++ {
		th.Fail("erin", "203.0.113.200", "mfa")
		*clock = clock.Add(thLockFor + time.Second)
	}
	*clock = clock.Add(-thLockFor)
	if duration, blocked := th.Check("erin", "198.51.100.1"); !blocked || duration != thMaxDelay {
		t.Fatalf("verrou long : %v %v", duration, blocked)
	}
	*clock = clock.Add(thStreakLongFor)
	if err := th.Success("erin", "203.0.113.200"); err != nil {
		t.Fatal(err)
	}
	var left int
	authStore.db.QueryRow(`SELECT COUNT(*) FROM throttle WHERE key='streak:erin'`).Scan(&left)
	if left != 0 {
		t.Fatal("suite non remise à zéro par un succès")
	}
}

func TestThrottlePurge(t *testing.T) {
	th, authStore, clock := testThrottle(t)
	th.Fail("x", "10.0.0.1", "password")
	th.Success("y", "10.0.0.2")
	*clock = clock.Add(thWindow + time.Second)
	if count, err := th.Purge(); err != nil || count != 3 {
		t.Fatalf("purge fenêtre : %d %v", count, err)
	}
	*clock = clock.Add(thStreakKeep)
	if count, err := th.Purge(); err != nil || count != 1 {
		t.Fatalf("purge suite : %d %v", count, err)
	}
	*clock = clock.Add(thKnownFor)
	if count, err := th.Purge(); err != nil || count != 1 {
		t.Fatalf("purge marqueur connu : %d %v", count, err)
	}
	var left int
	authStore.db.QueryRow(`SELECT COUNT(*) FROM throttle`).Scan(&left)
	if left != 0 {
		t.Fatalf("restant : %d", left)
	}
}

func TestIPBan(t *testing.T) {
	th, authStore, clock := testThrottle(t)
	if _, err := authStore.AddUser("dave", testPw, false); err != nil {
		t.Fatal(err)
	}
	if err := th.Success("dave", "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	// Une IP extérieure qui rate des noms différents est bannie, même si elle a déjà réussi une connexion.
	for count := 0; count < thBanAfter; count++ {
		if err := th.Fail(fmt.Sprintf("inconnu%d", count), "203.0.113.9", "password"); err != nil {
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
	authStore.db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE kind='lock' AND outcome='ban'`).Scan(&bans)
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
		for count := 0; count < thBanAfter+2; count++ {
			th.Fail("x", ip, "password")
		}
		if externalIP(ip) {
			t.Fatalf("%s vue comme extérieure", ip)
		}
		var locked int64
		authStore.db.QueryRow(`SELECT locked_until FROM throttle WHERE key=?`, "ip:"+ip).Scan(&locked)
		if locked != 0 {
			t.Fatalf("%s bannie", ip)
		}
	}
}

func TestBanAlerts(t *testing.T) {
	authRig := newAuthRig(t)
	secret := authRig.enrolTOTP()
	content, _ := authRig.full(secret) // alice, compte ordinaire
	if recorder := authRig.do("GET", "/v1/auth/alerts", "", "", nil); recorder.Code == 200 {
		t.Fatal("alertes servies sans session")
	}
	for count := 0; count < thBanAfter; count++ {
		authRig.th.Fail(fmt.Sprintf("x%d", count), "203.0.113.50", "password")
	}
	lookup := jbody(t, authRig.do("GET", "/v1/auth/alerts?since=0", content, "", nil))
	bans, _ := lookup["bans"].([]any)
	if len(bans) != 1 || bans[0].(map[string]any)["ip"] != "" {
		t.Fatalf("un compte ordinaire voit l'adresse : %v", lookup)
	}
	admin, err := authRig.s.AddUser("root", testPw, true)
	if err != nil {
		t.Fatal(err)
	}
	ac, _ := fullSession(t, authRig.s, admin)
	ab := jbody(t, authRig.do("GET", "/v1/auth/alerts?since=0", ac, "", nil))
	if bans, _ := ab["bans"].([]any); len(bans) != 1 || bans[0].(map[string]any)["ip"] != "203.0.113.50" {
		t.Fatalf("alertes admin : %v", ab)
	}
	now := int64(lookup["now"].(float64))
	if lookup := jbody(t, authRig.do("GET", fmt.Sprintf("/v1/auth/alerts?since=%d", now+1), content, "", nil)); len(lookup["bans"].([]any)) != 0 {
		t.Fatalf("ban déjà vu : %v", lookup)
	}
}

func TestMFAParAdresse(t *testing.T) {
	th, _, _ := testThrottle(t)
	for index := 0; index < 3; index++ {
		th.Fail("eve", "203.0.113.4", "mfa")
	}
	th.Success("eve", "203.0.113.4")
	for index := 0; index < 2; index++ {
		th.Fail("eve", "203.0.113.4", "mfa")
	}
	if _, blocked := th.Pending("eve", "203.0.113.4"); blocked {
		t.Fatal("fautes d'avant la connexion réussie encore comptées")
	}
	// Réparti sur plusieurs adresses, le compteur global du compte finit par bloquer partout.
	for index := 0; index < thMFAGlobal; index++ {
		th.Fail("eve", "198.51.100."+strconv.Itoa(index), "mfa")
	}
	if _, blocked := th.Pending("eve", "192.0.2.1"); !blocked {
		t.Fatal("plafond global du second facteur non appliqué")
	}
}
