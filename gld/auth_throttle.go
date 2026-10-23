package main

import (
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	thWindow     = 15 * time.Minute
	thMaxDelay   = 900 * time.Second
	thLockAfter  = 10
	thLockFor    = 30 * time.Minute
	thMFALimit   = 5
	thKnownFor   = 30 * 24 * time.Hour
	thGlobalMax  = 60
	thGlobalSpan = time.Minute

	evLogin   = "login"
	evSuccess = "success"
)

// Le plafond global vit en mémoire : il repart de zéro au redémarrage, ce qui suffit
// pour borner un essai distribué en cours.
type throttle struct {
	db  *authDB
	now func() time.Time

	mu     sync.Mutex
	recent []time.Time
}

func newThrottle(db *authDB) *throttle { return &throttle{db: db, now: time.Now} }

// Les clés dérivent du nom tapé, jamais d'une recherche du compte : un nom inconnu
// se comporte exactement comme un nom existant.
func thKeys(user, ip string) (u, i, p, m string) {
	n := normName(user)
	return "user:" + n, "ip:" + ip, "pair:" + n + "|" + ip, "mfa:" + n
}

func backoff(fails int64) time.Duration {
	if fails < 3 {
		return 0
	}
	e := fails - 3
	if e > 20 {
		return thMaxDelay
	}
	return min(time.Duration(math.Pow(2, float64(e)))*time.Second, thMaxDelay)
}

func (t *throttle) Check(user, ip string) (wait time.Duration, blocked bool) {
	now := t.now()
	cut := now.Add(-thWindow).Unix()
	uk, ik, pk, mk := thKeys(user, ip)
	rows, err := t.db.Query(`SELECT key, fails, first_at, locked_until FROM throttle WHERE key IN (?,?,?,?)`, uk, ik, pk, mk)
	if err != nil {
		return thMaxDelay, true
	}
	type row struct{ fails, first, locked int64 }
	got := map[string]row{}
	for rows.Next() {
		var k string
		var r row
		if rows.Scan(&k, &r.fails, &r.first, &r.locked) == nil {
			got[k] = r
		}
	}
	rows.Close()
	inWin := func(k string) int64 {
		if r, ok := got[k]; ok && r.first > cut {
			return r.fails
		}
		return 0
	}
	if inWin(mk) >= thMFALimit {
		return min(time.Unix(got[mk].first, 0).Add(thWindow).Sub(now), thMaxDelay), true
	}
	u := got[uk]
	uLocked := u.locked > now.Unix()
	uFails := inWin(uk)
	if (uLocked || uFails >= 3) && t.knownIPByName(normName(user), ip, now) {
		uLocked, uFails = false, 0
	}
	if uLocked {
		return min(time.Unix(u.locked, 0).Sub(now), thMaxDelay), true
	}
	wait = max(backoff(uFails), backoff(inWin(ik)), backoff(inWin(pk)))
	if wait > 0 {
		return wait, false
	}
	return t.globalSlot(now), false
}

func (t *throttle) globalSlot(now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	cut := now.Add(-thGlobalSpan)
	i := 0
	for i < len(t.recent) && !t.recent[i].After(cut) {
		i++
	}
	t.recent = t.recent[i:]
	if len(t.recent) >= thGlobalMax {
		return t.recent[0].Add(thGlobalSpan).Sub(now)
	}
	t.recent = append(t.recent, now)
	return 0
}

func (t *throttle) Fail(user, ip string, stage string) {
	now := t.now().Unix()
	cut := now - int64(thWindow/time.Second)
	uk, ik, pk, mk := thKeys(user, ip)
	keys := []string{uk, ik, pk}
	if stage == "mfa" {
		keys = append(keys, mk)
	}
	tx, err := t.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	for _, k := range keys {
		var fails int64
		err := tx.QueryRow(`INSERT INTO throttle(key, fails, first_at, locked_until) VALUES (?1, 1, ?2, 0)
ON CONFLICT(key) DO UPDATE SET
  fails = CASE WHEN first_at <= ?3 THEN 1 ELSE fails + 1 END,
  first_at = CASE WHEN first_at <= ?3 THEN ?2 ELSE first_at END,
  locked_until = CASE WHEN ?1 LIKE 'user:%' AND (CASE WHEN first_at <= ?3 THEN 1 ELSE fails + 1 END) >= ?4
    THEN ?2 + ?5 ELSE locked_until END
RETURNING fails`, k, now, cut, thLockAfter, int64(thLockFor/time.Second)).Scan(&fails)
		if err != nil {
			return
		}
		if k == uk && fails == thLockAfter {
			tx.Exec(`INSERT INTO auth_events(at, name_tried, ip, kind, outcome) VALUES (?,?,?,?,?)`, now, normName(user), ip, "lock", "locked")
		}
	}
	tx.Commit()
}

// Les compteurs mfa: ne sont pas remis à zéro : sinon un mot de passe connu suffirait
// à rouvrir cinq essais de second facteur à volonté.
func (t *throttle) Success(user, ip string) {
	uk, _, pk, _ := thKeys(user, ip)
	t.db.Exec(`DELETE FROM throttle WHERE key IN (?,?)`, uk, pk)
}

func (t *throttle) KnownIP(userID int64, ip string) bool {
	var one int
	return t.db.QueryRow(`SELECT 1 FROM auth_events WHERE user_id=? AND kind=? AND outcome=? AND ip=? AND at>=? LIMIT 1`,
		userID, evLogin, evSuccess, ip, t.now().Add(-thKnownFor).Unix()).Scan(&one) == nil
}

func (t *throttle) knownIPByName(name, ip string, now time.Time) bool {
	var one int
	return t.db.QueryRow(`SELECT 1 FROM auth_events e JOIN users u ON u.id = e.user_id
WHERE u.name=? AND e.kind=? AND e.outcome=? AND e.ip=? AND e.at>=? LIMIT 1`,
		name, evLogin, evSuccess, ip, now.Add(-thKnownFor).Unix()).Scan(&one) == nil
}

func trustedProxy(a netip.Addr) bool {
	for _, s := range strings.Split(os.Getenv("GLD_TRUSTED_PROXIES"), ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil && p.Contains(a) {
			return true
		}
		if b, err := netip.ParseAddr(s); err == nil && b.Unmap() == a {
			return true
		}
	}
	return false
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	a = a.Unmap().WithZone("")
	if trustedProxy(a) {
		// On remonte la chaîne depuis la droite : seules les entrées ajoutées par nos
		// proxys sont fiables, la plus à gauche est fournie par le client.
		parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
		for i := len(parts) - 1; i >= 0; i-- {
			b, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if err != nil {
				break
			}
			a = b.Unmap().WithZone("")
			if !trustedProxy(a) {
				break
			}
		}
	}
	if a.Is6() {
		p, _ := a.Prefix(64)
		return p.String()
	}
	return a.String()
}
