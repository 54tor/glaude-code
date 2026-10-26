package main

// Jumelage d'un appareil. Un navigateur déjà connecté demande un code à usage unique (huit chiffres, deux minutes) et
// l'affiche en clair et en QR. Le téléphone le saisit sur « Connecter cet appareil » (/pair, liée depuis la page du jeton
// et l'écran de connexion) : la session naît là où il le tape, donc dans la PWA. Le QR ouvre /pair?c=…, une page de
// confirmation sans effet (un aperçu de lien ne consomme rien) dont le bouton envoie le même POST de même origine.
// Les codes faux comptent dans les essais du jeton (tokenTry) : 10^8 codes, deux minutes, 10 essais par minute.
// Mode jeton : session d'appareil (devices.json, 90 jours sans visite), liée au jeton : en changer coupe tous les appareils.
// Mode comptes : session du compte dans auth.db, second facteur acquis, tant que la session qui a émis le code vit.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	mbig "math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	pairTTL   = 2 * time.Minute
	devIdle   = 90 * 24 * time.Hour
	devTouch  = time.Minute
	devCookie = "gld_dev"
)

type pairCode struct {
	exp time.Time
	uid int64  // 0 : mode jeton
	sid string // session émettrice (mode comptes)
}

var (
	pairMu    sync.Mutex
	pairCodes = map[string]pairCode{}
)

func newPairCode(uid int64, sid string) string {
	now := authNow()
	pairMu.Lock()
	defer pairMu.Unlock()
	for k, v := range pairCodes {
		if now.After(v.exp) {
			delete(pairCodes, k)
		}
	}
	for {
		n, _ := rand.Int(rand.Reader, mbig.NewInt(1e8))
		c := fmt.Sprintf("%08d", n)
		if _, used := pairCodes[c]; !used {
			pairCodes[c] = pairCode{now.Add(pairTTL), uid, sid}
			return c
		}
	}
}

// pairDigits : le code tel que tapé (« 1234 5678 », « 1234-5678 »), réduit à ses chiffres.
func pairDigits(c string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, c)
}

// pairLookup lit le code ; take le consomme, valide ou non : un code essayé ne sert plus.
func pairLookup(c string, take bool) (pairCode, bool) {
	pairMu.Lock()
	defer pairMu.Unlock()
	p, ok := pairCodes[c]
	if take {
		delete(pairCodes, c)
	}
	return p, ok && authNow().Before(p.exp)
}

// device : un appareil jumelé en mode jeton. Le cookie n'est jamais stocké, seulement son HMAC sous la clé du jeton.
type device struct {
	ID      string `json:"id"`
	Bind    string `json:"bind"` // empreinte de la clé du jeton : un autre jeton ne reconnaît plus l'appareil
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Created int64  `json:"created"`
	Seen    int64  `json:"seen"`
}

var (
	devMu  sync.Mutex
	devKey []byte // dérivée du jeton par withToken ; nil sans jeton : rien à jumeler
)

func setDevKey(token string) {
	m := hmac.New(sha256.New, []byte(token))
	m.Write([]byte("gld-device"))
	devMu.Lock()
	devKey = m.Sum(nil)
	devMu.Unlock()
}

// devMac et devBind supposent devMu tenu.
func devMac(cookie string) string {
	m := hmac.New(sha256.New, devKey)
	m.Write([]byte(cookie))
	return hex.EncodeToString(m.Sum(nil))
}

func devBind() string {
	h := sha256.Sum256(devKey)
	return hex.EncodeToString(h[:8])
}

func devicesFile() (string, error) {
	d, err := authDir()
	return filepath.Join(d, "devices.json"), err
}

func loadDevices() ([]device, error) {
	p, err := devicesFile()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ds []device
	return ds, json.Unmarshal(b, &ds)
}

func saveDevices(ds []device) error {
	p, err := devicesFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(ds, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// liveDevices : les appareils du jeton courant vus depuis moins de devIdle (devMu tenu).
func liveDevices(ds []device) []device {
	limit, bind := authNow().Add(-devIdle).Unix(), devBind()
	return slices.DeleteFunc(ds, func(d device) bool { return d.Seen < limit || d.Bind != bind })
}

func addDevice(ua, ip string) (string, error) {
	b := make([]byte, 32)
	rand.Read(b)
	c := base64.RawURLEncoding.EncodeToString(b)
	if len(ua) > 256 {
		ua = ua[:256]
	}
	now := authNow().Unix()
	devMu.Lock()
	defer devMu.Unlock()
	if devKey == nil {
		return "", errors.New("pas de jeton")
	}
	ds, err := loadDevices()
	if err != nil {
		return "", err
	}
	return c, saveDevices(append(liveDevices(ds), device{devMac(c), devBind(), ua, ip, now, now}))
}

// deviceOK admet un cookie d'appareil encore vivant et repousse son échéance (au plus une écriture par minute).
func deviceOK(cookie string) bool {
	now := authNow().Unix()
	devMu.Lock()
	defer devMu.Unlock()
	if devKey == nil {
		return false
	}
	ds, err := loadDevices()
	if err != nil {
		journal("pair: devices.json illisible : %v", err)
		return false
	}
	n, id := len(ds), devMac(cookie)
	ds = liveDevices(ds)
	i := slices.IndexFunc(ds, func(d device) bool { return hmac.Equal([]byte(d.ID), []byte(id)) })
	dirty := len(ds) != n
	if i >= 0 && now-ds[i].Seen >= int64(devTouch/time.Second) {
		ds[i].Seen, dirty = now, true
	}
	if dirty {
		if err := saveDevices(ds); err != nil {
			journal("pair: devices.json : %v", err)
		}
	}
	return i >= 0
}

var pairCOP = http.NewCrossOriginProtection()

// pairEntry : GET /pair (confirmation, sans effet) et POST /pair (création de la session). st nil : mode jeton.
func pairEntry(w http.ResponseWriter, r *http.Request, st *authStore) {
	devMu.Lock()
	keyless := devKey == nil
	devMu.Unlock()
	if st == nil && keyless {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	form := func(code int, errKey string) {
		d := gateData{Title: L("srv.pair.title"), Text: L("srv.pair.text"), Action: "/pair", Code: true, Button: L("srv.pair.button"), Link: "/", LinkText: L("srv.pair.back")}
		if errKey != "" {
			d.Err = L(errKey)
		}
		gatePage(w, code, d)
	}
	expired := func() { form(http.StatusGone, "srv.pair.expired") }
	ip := clientIP(r)
	if r.Method == "GET" && r.URL.Query().Get("c") == "" {
		form(200, "")
		return
	}
	if r.Method == "POST" && pairCOP.Check(r) != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !tokenTry(ip) {
		w.Header().Set("Retry-After", "60")
		form(http.StatusTooManyRequests, "srv.token.tooMany")
		return
	}
	if r.Method == "GET" {
		c := pairDigits(r.URL.Query().Get("c"))
		p, ok := pairLookup(c, false)
		if !ok || (p.uid != 0) != (st != nil) {
			expired()
			return
		}
		tokenRefund(ip)
		text := L("srv.pair.textToken")
		if st != nil {
			u, err := st.userByID(st.db, p.uid)
			if err != nil {
				expired()
				return
			}
			text = L("srv.pair.textAccount", "A1", u.Name)
		}
		gatePage(w, 200, gateData{Title: L("srv.pair.title"), Text: text, Action: "/pair", Hidden: c, Button: L("srv.pair.button")})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<12)
	p, ok := pairLookup(pairDigits(r.PostFormValue("c")), true)
	if !ok || (p.uid != 0) != (st != nil) {
		expired()
		return
	}
	tokenRefund(ip)
	if st == nil {
		c, err := addDevice(r.UserAgent(), ip)
		if err != nil {
			journal("pair: %v", err)
			expired()
			return
		}
		// MaxAge large : l'échéance réelle (90 jours sans visite) est tenue côté serveur.
		http.SetCookie(w, &http.Cookie{Name: devCookie, Value: c, Path: "/", MaxAge: 400 * 24 * 3600, HttpOnly: true, Secure: requestHTTPS(r), SameSite: http.SameSiteStrictMode})
		journal("pair: appareil jumelé depuis %s", ip)
	} else if !redeemSession(w, r, st, p, ip) {
		expired()
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// redeemSession : la session naît complète (second facteur acquis), à condition que la session émettrice soit encore
// ouverte et complète : la révoquer annule aussi ses codes en attente.
func redeemSession(w http.ResponseWriter, r *http.Request, st *authStore, p pairCode, ip string) bool {
	ss, err := st.userSessions(p.uid)
	if err != nil || !slices.ContainsFunc(ss, func(x sessRow) bool { return x.mfa && hex.EncodeToString(x.hash) == p.sid }) {
		return false
	}
	c, err := st.NewSession(p.uid, ip, r.UserAgent())
	var pr *Principal
	if err == nil {
		pr, err = st.Session(c) // refuse un compte désactivé depuis
	}
	var exp time.Time
	if err == nil {
		c, exp, err = st.PromoteMFA(pr.SessionID)
	}
	if err != nil {
		if !errors.Is(err, errNoSession) {
			journal("pair: session : %v", err)
		}
		return false
	}
	setSessionCookie(w, r, c, exp)
	clearHomeCookie(w)
	st.eventIP(p.uid, evLogin, "pair", ip)
	return true
}

// publicBase : l'adresse que le téléphone ouvrira. GLD_PUBLIC_URL (--public-url) d'abord : l'ordinateur qui affiche le QR
// est souvent sur 127.0.0.1 ou l'IP locale ; sinon, l'adresse par laquelle la requête est arrivée.
func publicBase(r *http.Request) string {
	if u, err := url.Parse(strings.TrimSpace(os.Getenv("GLD_PUBLIC_URL"))); err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") {
		return u.Scheme + "://" + u.Host
	}
	if requestHTTPS(r) {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}

func routesPair(mux *http.ServeMux) {
	mux.HandleFunc("POST /pair-code", func(w http.ResponseWriter, r *http.Request) {
		r, d := reqMode(r)
		var uid int64
		var sid string
		if d.on {
			g, _ := r.Context().Value(gateKey{}).(*gateInfo)
			if g == nil || g.p == nil || g.p.User == nil || !g.p.MFA {
				writeJSON(w, 403, map[string]any{"ok": false, "out": L("srv.auth.droitRefuse")})
				return
			}
			uid, sid = g.p.User.ID, g.p.SessionID
		}
		c := newPairCode(uid, sid)
		writeJSON(w, 200, map[string]any{"ok": true, "code": c, "url": publicBase(r) + "/pair?c=" + c, "ttl": int(pairTTL / time.Second)})
	})
	// Mode comptes, ou sans jeton ; en mode jeton, withToken répond avant.
	entry := func(w http.ResponseWriter, r *http.Request) {
		r, d := reqMode(r)
		var st *authStore
		if d.on {
			st = d.st
		}
		pairEntry(w, r, st)
	}
	mux.HandleFunc("GET /pair", entry)
	mux.HandleFunc("POST /pair", entry)
	// Mode jeton seulement : en mode comptes, les sessions du compte (/auth/sessions) en tiennent lieu.
	mux.HandleFunc("GET /devices", func(w http.ResponseWriter, r *http.Request) {
		if _, d := reqMode(r); d.on {
			http.NotFound(w, r)
			return
		}
		devMu.Lock()
		ds, err := loadDevices()
		if err == nil {
			ds = liveDevices(ds)
		}
		devMu.Unlock()
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "devices": ds})
	})
	mux.HandleFunc("POST /devices-revoke", func(w http.ResponseWriter, r *http.Request) {
		if _, d := reqMode(r); d.on {
			http.NotFound(w, r)
			return
		}
		var req struct{ ID string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req) != nil || req.ID == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		devMu.Lock()
		defer devMu.Unlock()
		ds, err := loadDevices()
		if err == nil {
			err = saveDevices(slices.DeleteFunc(ds, func(d device) bool { return d.ID == req.ID }))
		}
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		journal("pair: appareil %s révoqué", cut(req.ID, 8))
		writeJSON(w, 200, map[string]any{"ok": true})
	})
}
