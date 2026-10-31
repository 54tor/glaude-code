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
	thMFAGlobal = 15 // tous appareils confondus : jamais remis à zéro par une connexion, contre un attaquant qui change d'adresse
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
func thKeys(user, ip string) (userKey, ipKey, pairKey, mfaKey string) {
	normalizedName := thName(user)
	return "user:" + normalizedName, "ip:" + ip, "pair:" + normalizedName + "|" + ip, "mfa:" + normalizedName
}

func streakKey(user string) string { return "streak:" + thName(user) }

// mfaPairKey : les échecs de second facteur d'un compte depuis une adresse, remis à zéro par une connexion réussie depuis celle-ci.
func mfaPairKey(user, ip string) string { return "mfapair:" + thName(user) + "|" + ip }

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
	exponent := fails - 3
	if exponent > 20 {
		return thMaxDelay
	}
	return min(time.Duration(math.Pow(2, float64(exponent)))*time.Second, thMaxDelay)
}

func (throttler *throttle) Check(user, ip string) (wait time.Duration, blocked bool) {
	wait, blocked, known := throttler.pending(user, ip)
	if blocked || wait > 0 || known { // un couple déjà connu ne puise pas dans le plafond global : des noms inventés ne le bloquent plus
		return wait, blocked
	}
	return throttler.globalSlot(throttler.now()), false
}

// Pending est Check sans le plafond global : il sert à annoncer le Retry-After d'un échec sans consommer de place.
func (throttler *throttle) Pending(user, ip string) (wait time.Duration, blocked bool) {
	wait, blocked, _ = throttler.pending(user, ip)
	return wait, blocked
}

// pending : known vaut true si ce compte s'est connecté depuis cette adresse dans les thKnownFor.
func (throttler *throttle) pending(user, ip string) (wait time.Duration, blocked, known bool) {
	now := throttler.now()
	windowStart := now.Add(-thWindow).Unix()
	userKey, ipKey, pairKey, mfaKey := thKeys(user, ip)
	knownMarkerKey, streakCounterKey := knownKey(user, ip), streakKey(user)
	mfaPairCounterKey := mfaPairKey(user, ip)
	rows, err := throttler.db.Query(`SELECT key, fails, first_at, locked_until, last_at FROM throttle WHERE key IN (?,?,?,?,?,?,?)`, userKey, ipKey, pairKey, mfaKey, mfaPairCounterKey, knownMarkerKey, streakCounterKey)
	if err != nil {
		return thMaxDelay, true, false
	}
	type row struct{ fails, first, locked, last int64 }
	rowsByKey := map[string]row{}
	for rows.Next() {
		var scannedKey string
		var scannedRow row
		if scanErr := rows.Scan(&scannedKey, &scannedRow.fails, &scannedRow.first, &scannedRow.locked, &scannedRow.last); scanErr != nil {
			rows.Close()
			return thMaxDelay, true, false
		}
		rowsByKey[scannedKey] = scannedRow
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return thMaxDelay, true, false
	}
	failsInWindow := func(counterKey string) int64 {
		if counterRow, found := rowsByKey[counterKey]; found && counterRow.first > windowStart {
			return counterRow.fails
		}
		return 0
	}
	for _, mfaCounterKey := range []string{mfaPairCounterKey, mfaKey} {
		if limit := map[string]int64{mfaPairCounterKey: thMFALimit, mfaKey: thMFAGlobal}[mfaCounterKey]; failsInWindow(mfaCounterKey) >= limit {
			return min(time.Unix(rowsByKey[mfaCounterKey].first, 0).Add(thWindow).Sub(now), thMaxDelay), true, false
		}
	}
	userRow := rowsByKey[userKey]
	lockEnd := max(userRow.locked, rowsByKey[streakCounterKey].locked)
	userFails := failsInWindow(userKey)
	if knownRow, found := rowsByKey[knownMarkerKey]; found && knownRow.first >= now.Add(-thKnownFor).Unix() {
		lockEnd, userFails, known = 0, 0, true
	}
	lockEnd = max(lockEnd, rowsByKey[ipKey].locked) // bannissement de l'IP : même une IP où ce compte s'est déjà connecté n'y échappe pas
	if lockEnd > now.Unix() {
		return min(time.Unix(lockEnd, 0).Sub(now), thMaxDelay), true, known
	}
	// Le délai court depuis le dernier échec : un nouvel essai après Retry-After est réellement vérifié.
	remainingDelay := func(counterKey string, fails int64) time.Duration {
		return max(0, backoff(fails)-now.Sub(time.Unix(rowsByKey[counterKey].last, 0)))
	}
	return max(remainingDelay(userKey, userFails), remainingDelay(ipKey, failsInWindow(ipKey)), remainingDelay(pairKey, failsInWindow(pairKey))), false, known
}

func (throttler *throttle) globalSlot(now time.Time) time.Duration {
	throttler.mu.Lock()
	defer throttler.mu.Unlock()
	windowStart := now.Add(-thGlobalSpan)
	expiredCount := 0
	for expiredCount < len(throttler.recent) && !throttler.recent[expiredCount].After(windowStart) {
		expiredCount++
	}
	throttler.recent = throttler.recent[expiredCount:]
	if len(throttler.recent) >= thGlobalMax {
		return throttler.recent[0].Add(thGlobalSpan).Sub(now)
	}
	throttler.recent = append(throttler.recent, now)
	return 0
}

func (throttler *throttle) Fail(user, ip string, stage string) error {
	now := throttler.now().Unix()
	windowStart := now - int64(thWindow/time.Second)
	userKey, ipKey, pairKey, mfaKey := thKeys(user, ip)
	keys := []string{userKey, ipKey, pairKey}
	if stage == "mfa" {
		keys = append(keys, mfaKey, mfaPairKey(user, ip))
	}
	externalFlag := 0
	if externalIP(ip) {
		externalFlag = 1
	}
	transaction, err := throttler.db.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for _, counterKey := range keys {
		var fails int64
		upsertErr := transaction.QueryRow(`INSERT INTO throttle(key, fails, first_at, locked_until, last_at) VALUES (?1, 1, ?2, 0, ?2)
ON CONFLICT(key) DO UPDATE SET
  last_at = ?2,
  fails = CASE WHEN first_at <= ?3 THEN 1 ELSE fails + 1 END,
  first_at = CASE WHEN first_at <= ?3 THEN ?2 ELSE first_at END,
  locked_until = CASE WHEN ?1 LIKE 'user:%' AND (CASE WHEN first_at <= ?3 THEN 1 ELSE fails + 1 END) >= ?4
    THEN ?2 + ?5
    WHEN ?1 LIKE 'ip:%' AND ?6 = 1 AND (CASE WHEN first_at <= ?3 THEN 1 ELSE fails + 1 END) >= ?7
    THEN ?2 + ?8 ELSE locked_until END
RETURNING fails`, counterKey, now, windowStart, thLockAfter, int64(thLockFor/time.Second), externalFlag, thBanAfter, int64(thBanFor/time.Second)).Scan(&fails)
		if upsertErr != nil {
			return upsertErr
		}
		if counterKey == ipKey && externalFlag == 1 && fails == thBanAfter {
			if banEventErr := lockEvent(transaction, now, user, ip, "ban"); banEventErr != nil {
				return banEventErr
			}
		}
		if counterKey == userKey && fails == thLockAfter {
			if lockEventErr := lockEvent(transaction, now, user, ip, "locked"); lockEventErr != nil {
				return lockEventErr
			}
		}
	}
	var streak int64
	err = transaction.QueryRow(`INSERT INTO throttle(key, fails, first_at, locked_until, last_at) VALUES (?1, 1, ?2, 0, ?2)
ON CONFLICT(key) DO UPDATE SET last_at = ?2, fails = fails + 1,
  locked_until = CASE WHEN fails + 1 >= ?4 THEN ?2 + ?6 WHEN fails + 1 >= ?3 THEN ?2 + ?5 ELSE locked_until END
RETURNING fails`, streakKey(user), now, thStreakLock, thStreakLong, int64(thLockFor/time.Second), int64(thStreakLongFor/time.Second)).Scan(&streak)
	if err != nil {
		return err
	}
	if streak == thStreakLock || streak == thStreakLong {
		if streakEventErr := lockEvent(transaction, now, user, ip, "streak_"+strconv.FormatInt(streak, 10)); streakEventErr != nil {
			return streakEventErr
		}
	}
	return transaction.Commit()
}

func lockEvent(transaction *sql.Tx, now int64, user, ip, outcome string) error {
	journal("auth: compte %q verrouillé (%s) depuis %s", normName(user), outcome, ip)
	_, err := transaction.Exec(`INSERT INTO auth_events(at, name_tried, ip, kind, outcome) VALUES (?,?,?,?,?)`, now, normName(user), ip, "lock", outcome)
	return err
}

// Les compteurs mfa: ne sont pas remis à zéro : sinon un mot de passe connu suffirait
// à rouvrir cinq essais de second facteur à volonté.
func (throttler *throttle) Success(user, ip string) error {
	now := throttler.now().Unix()
	userKey, _, pairKey, _ := thKeys(user, ip)
	// Le compteur MFA de cette adresse repart de zéro, sauf s'il bloque déjà ; le compteur global du compte reste.
	_, err := throttler.db.Exec(`DELETE FROM throttle WHERE key IN (?,?,?) OR (key = ? AND fails < ?) OR (key LIKE 'known:%' AND first_at < ?)`,
		userKey, pairKey, streakKey(user), mfaPairKey(user, ip), thMFALimit, now-int64(thKnownFor/time.Second))
	if err != nil {
		return err
	}
	_, err = throttler.db.Exec(`INSERT INTO throttle(key, fails, first_at, locked_until) VALUES (?, 0, ?, 0)
ON CONFLICT(key) DO UPDATE SET first_at = excluded.first_at`, knownKey(user, ip), now)
	return err
}

// Purge retire les lignes qui ne pèsent plus sur aucune décision : un flot de noms inventés ne fait pas grossir la table.
// Une suite d'échecs reste sept jours après son dernier échec.
func (throttler *throttle) Purge() (int64, error) {
	now := throttler.now().Unix()
	result, err := throttler.db.Exec(`DELETE FROM throttle WHERE locked_until <= ?1 AND (
  (key LIKE 'known:%' AND first_at < ?2) OR
  (key LIKE 'streak:%' AND last_at < ?3) OR
  (key NOT LIKE 'known:%' AND key NOT LIKE 'streak:%' AND first_at <= ?4))`,
		now, now-int64(thKnownFor/time.Second), now-int64(thStreakKeep/time.Second), now-int64(thWindow/time.Second))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// externalIP : l'IP (ou le préfixe /64 rendu par clientIP) ne vient ni de la machine, ni d'un réseau privé, ni du lien local :
// seuls les accès extérieurs sont bannis, jamais le poste ni le réseau de l'utilisateur.
func externalIP(ip string) bool {
	host, _, _ := strings.Cut(ip, "/")
	address, err := netip.ParseAddr(host)
	if err != nil {
		return true
	}
	address = address.Unmap()
	return !(address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast())
}

func trustedProxy(address netip.Addr) bool {
	for _, entry := range strings.Split(os.Getenv("GLD_TRUSTED_PROXIES"), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil && prefix.Contains(address) {
			return true
		}
		if proxyAddress, err := netip.ParseAddr(entry); err == nil && proxyAddress.Unmap() == address {
			return true
		}
	}
	return false
}

func clientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	address = address.Unmap().WithZone("")
	if trustedProxy(address) {
		// On remonte la chaîne depuis la droite : seules les entrées ajoutées par nos
		// proxys sont fiables, la plus à gauche est fournie par le client.
		parts := strings.Split(strings.Join(request.Header.Values("X-Forwarded-For"), ","), ",")
		for partIndex := len(parts) - 1; partIndex >= 0; partIndex-- {
			forwardedAddress, parseErr := netip.ParseAddr(strings.TrimSpace(parts[partIndex]))
			if parseErr != nil {
				break
			}
			address = forwardedAddress.Unmap().WithZone("")
			if !trustedProxy(address) {
				break
			}
		}
	}
	if address.Is6() && !address.IsLoopback() { // ::1 réduit à ::/64 ne serait plus reconnu comme le poste, donc banni
		prefix, _ := address.Prefix(64)
		return prefix.String()
	}
	return address.String()
}
