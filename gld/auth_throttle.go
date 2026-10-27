package main

import (
	"database/sql"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	thWindow    = 15 * time.Minute
	thMaxDelay  = 900 * time.Second
	thLockAfter = 10
	thLockFor   = 30 * time.Minute
	thMFALimit  = 5
	thKnownFor  = 30 * 24 * time.Hour
	// Bannissement (à la fail2ban) d'une IP extérieure : trop d'échecs, tous noms confondus, dans la fenêtre.
	// ponytail: durée fixe, sans escalade pour les récidivistes ; la ajouter si des IP reviennent après chaque ban.
	thBanAfter   = 8
	thBanFor     = time.Hour
	thGlobalMax  = 60
	thGlobalSpan = time.Minute
	// Suite d'échecs consécutifs par compte, hors fenêtre : un essai lent (quelques échecs par quart d'heure) finit verrouillé.
	thStreakLock    = 10
	thStreakLong    = 20
	thStreakLongFor = 24 * time.Hour
	thStreakKeep    = 7 * 24 * time.Hour

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
	n := thName(user)
	return "user:" + n, "ip:" + ip, "pair:" + n + "|" + ip, "mfa:" + n
}

func streakKey(user string) string { return "streak:" + thName(user) }

// thName : la clé commune des noms trop longs n'est pas normalisée, pour qu'aucun nom tapé ne la rejoigne.
func thName(user string) string {
	if user == oversizeName {
		return user
	}
	return normName(user)
}

// Marqueur posé par Success : first_at sert de date de dernière connexion réussie.
func knownKey(user, ip string) string { return "known:" + thName(user) + "|" + ip }

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
	if wait, blocked = t.Pending(user, ip); blocked || wait > 0 {
		return wait, blocked
	}
	return t.globalSlot(t.now()), false
}

// Pending est Check sans le plafond global : il sert à annoncer le Retry-After d'un échec sans consommer de place.
func (t *throttle) Pending(user, ip string) (wait time.Duration, blocked bool) {
	now := t.now()
	cut := now.Add(-thWindow).Unix()
	uk, ik, pk, mk := thKeys(user, ip)
	kk, sk := knownKey(user, ip), streakKey(user)
	rows, err := t.db.Query(`SELECT key, fails, first_at, locked_until, last_at FROM throttle WHERE key IN (?,?,?,?,?,?)`, uk, ik, pk, mk, kk, sk)
	if err != nil {
		return thMaxDelay, true
	}
	type row struct{ fails, first, locked, last int64 }
	got := map[string]row{}
	for rows.Next() {
		var k string
		var r row
		if err := rows.Scan(&k, &r.fails, &r.first, &r.locked, &r.last); err != nil {
			rows.Close()
			return thMaxDelay, true
		}
		got[k] = r
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return thMaxDelay, true
	}
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
	lockEnd := max(u.locked, got[sk].locked)
	uFails := inWin(uk)
	if k, ok := got[kk]; ok && k.first >= now.Add(-thKnownFor).Unix() {
		lockEnd, uFails = 0, 0
	}
	lockEnd = max(lockEnd, got[ik].locked) // bannissement de l'IP : même une IP où ce compte s'est déjà connecté n'y échappe pas
	if lockEnd > now.Unix() {
		return min(time.Unix(lockEnd, 0).Sub(now), thMaxDelay), true
	}
	// Le délai court depuis le dernier échec : un nouvel essai après Retry-After est réellement vérifié.
	left := func(k string, fails int64) time.Duration {
		return max(0, backoff(fails)-now.Sub(time.Unix(got[k].last, 0)))
	}
	return max(left(uk, uFails), left(ik, inWin(ik)), left(pk, inWin(pk))), false
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

func (t *throttle) Fail(user, ip string, stage string) error {
	now := t.now().Unix()
	cut := now - int64(thWindow/time.Second)
	uk, ik, pk, mk := thKeys(user, ip)
	keys := []string{uk, ik, pk}
	if stage == "mfa" {
		keys = append(keys, mk)
	}
	ext := 0
	if externalIP(ip) {
		ext = 1
	}
	tx, err := t.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, k := range keys {
		var fails int64
		err := tx.QueryRow(`INSERT INTO throttle(key, fails, first_at, locked_until, last_at) VALUES (?1, 1, ?2, 0, ?2)
ON CONFLICT(key) DO UPDATE SET
  last_at = ?2,
  fails = CASE WHEN first_at <= ?3 THEN 1 ELSE fails + 1 END,
  first_at = CASE WHEN first_at <= ?3 THEN ?2 ELSE first_at END,
  locked_until = CASE WHEN ?1 LIKE 'user:%' AND (CASE WHEN first_at <= ?3 THEN 1 ELSE fails + 1 END) >= ?4
    THEN ?2 + ?5
    WHEN ?1 LIKE 'ip:%' AND ?6 = 1 AND (CASE WHEN first_at <= ?3 THEN 1 ELSE fails + 1 END) >= ?7
    THEN ?2 + ?8 ELSE locked_until END
RETURNING fails`, k, now, cut, thLockAfter, int64(thLockFor/time.Second), ext, thBanAfter, int64(thBanFor/time.Second)).Scan(&fails)
		if err != nil {
			return err
		}
		if k == ik && ext == 1 && fails == thBanAfter {
			if err := lockEvent(tx, now, user, ip, "ban"); err != nil {
				return err
			}
		}
		if k == uk && fails == thLockAfter {
			if err := lockEvent(tx, now, user, ip, "locked"); err != nil {
				return err
			}
		}
	}
	var streak int64
	err = tx.QueryRow(`INSERT INTO throttle(key, fails, first_at, locked_until, last_at) VALUES (?1, 1, ?2, 0, ?2)
ON CONFLICT(key) DO UPDATE SET last_at = ?2, fails = fails + 1,
  locked_until = CASE WHEN fails + 1 >= ?4 THEN ?2 + ?6 WHEN fails + 1 >= ?3 THEN ?2 + ?5 ELSE locked_until END
RETURNING fails`, streakKey(user), now, thStreakLock, thStreakLong, int64(thLockFor/time.Second), int64(thStreakLongFor/time.Second)).Scan(&streak)
	if err != nil {
		return err
	}
	if streak == thStreakLock || streak == thStreakLong {
		if err := lockEvent(tx, now, user, ip, "streak_"+strconv.FormatInt(streak, 10)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func lockEvent(tx *sql.Tx, now int64, user, ip, outcome string) error {
	journal("auth: compte %q verrouillé (%s) depuis %s", normName(user), outcome, ip)
	_, err := tx.Exec(`INSERT INTO auth_events(at, name_tried, ip, kind, outcome) VALUES (?,?,?,?,?)`, now, normName(user), ip, "lock", outcome)
	return err
}

// Les compteurs mfa: ne sont pas remis à zéro : sinon un mot de passe connu suffirait
// à rouvrir cinq essais de second facteur à volonté.
func (t *throttle) Success(user, ip string) error {
	now := t.now().Unix()
	uk, _, pk, _ := thKeys(user, ip)
	_, err := t.db.Exec(`DELETE FROM throttle WHERE key IN (?,?,?) OR (key LIKE 'known:%' AND first_at < ?)`,
		uk, pk, streakKey(user), now-int64(thKnownFor/time.Second))
	if err != nil {
		return err
	}
	_, err = t.db.Exec(`INSERT INTO throttle(key, fails, first_at, locked_until) VALUES (?, 0, ?, 0)
ON CONFLICT(key) DO UPDATE SET first_at = excluded.first_at`, knownKey(user, ip), now)
	return err
}

// Purge retire les lignes qui ne pèsent plus sur aucune décision : un flot de noms inventés ne fait pas grossir la table.
// Une suite d'échecs reste sept jours après son dernier échec.
func (t *throttle) Purge() (int64, error) {
	now := t.now().Unix()
	res, err := t.db.Exec(`DELETE FROM throttle WHERE locked_until <= ?1 AND (
  (key LIKE 'known:%' AND first_at < ?2) OR
  (key LIKE 'streak:%' AND last_at < ?3) OR
  (key NOT LIKE 'known:%' AND key NOT LIKE 'streak:%' AND first_at <= ?4))`,
		now, now-int64(thKnownFor/time.Second), now-int64(thStreakKeep/time.Second), now-int64(thWindow/time.Second))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// externalIP : l'IP (ou le préfixe /64 rendu par clientIP) ne vient ni de la machine, ni d'un réseau privé, ni du lien local :
// seuls les accès extérieurs sont bannis, jamais le poste ni le réseau de l'utilisateur.
func externalIP(ip string) bool {
	h, _, _ := strings.Cut(ip, "/")
	a, err := netip.ParseAddr(h)
	if err != nil {
		return true
	}
	a = a.Unmap()
	return !(a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast())
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
