package main

// Jumelage d'un appareil par QR code. Un navigateur déjà connecté demande un code à usage unique (deux minutes) et
// l'affiche en QR ; l'appareil qui ouvre l'adresse reçoit sa propre session, sans voir le jeton ni le mot de passe.
// Mode jeton : session d'appareil (devices.json, 90 jours sans visite), révocable sans changer le jeton.
// Mode comptes : session du compte dans auth.db, second facteur acquis, comme après une connexion complète.
// Sur iPhone, l'appareil photo ouvre l'adresse dans Safari : la PWA ajoutée ensuite à l'écran d'accueil en copie le cookie.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
	uid int64 // 0 : mode jeton
}

var (
	pairMu    sync.Mutex
	pairCodes = map[string]pairCode{}
)

func newPairCode(uid int64) string {
	b := make([]byte, 24)
	rand.Read(b)
	c := base64.RawURLEncoding.EncodeToString(b)
	now := authNow()
	pairMu.Lock()
	defer pairMu.Unlock()
	for k, v := range pairCodes {
		if now.After(v.exp) {
			delete(pairCodes, k)
		}
	}
	pairCodes[c] = pairCode{now.Add(pairTTL), uid}
	return c
}

// takePairCode consomme le code, valide ou non : un code essayé ne sert plus.
func takePairCode(c string) (pairCode, bool) {
	pairMu.Lock()
	defer pairMu.Unlock()
	p, ok := pairCodes[c]
	delete(pairCodes, c)
	return p, ok && authNow().Before(p.exp)
}

// device : un appareil jumelé en mode jeton. Le cookie n'est jamais stocké, seulement son empreinte.
type device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Created int64  `json:"created"`
	Seen    int64  `json:"seen"`
}

var devMu sync.Mutex

func devicesFile() (string, error) {
	d, err := authDir()
	return filepath.Join(d, "devices.json"), err
}

func devHash(cookie string) string {
	h := sha256.Sum256([]byte(cookie))
	return hex.EncodeToString(h[:])
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

// liveDevices : les appareils vus depuis moins de devIdle.
func liveDevices(ds []device) []device {
	limit := authNow().Add(-devIdle).Unix()
	return slices.DeleteFunc(ds, func(d device) bool { return d.Seen < limit })
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
	ds, err := loadDevices()
	if err != nil {
		return "", err
	}
	return c, saveDevices(append(liveDevices(ds), device{devHash(c), ua, ip, now, now}))
}

// deviceOK admet un cookie d'appareil encore vivant et repousse son échéance (au plus une écriture par minute).
func deviceOK(cookie string) bool {
	id, now := devHash(cookie), authNow().Unix()
	devMu.Lock()
	defer devMu.Unlock()
	ds, err := loadDevices()
	if err != nil {
		journal("pair: devices.json illisible : %v", err)
		return false
	}
	n := len(ds)
	ds = liveDevices(ds)
	i := slices.IndexFunc(ds, func(d device) bool { return d.ID == id })
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

// redeemDevice : GET /pair?c=… en mode jeton. Faux si le code n'est pas un code du mode jeton (l'appelant refuse alors).
func redeemDevice(w http.ResponseWriter, r *http.Request) bool {
	p, ok := takePairCode(r.URL.Query().Get("c"))
	if !ok || p.uid != 0 {
		return false
	}
	c, err := addDevice(r.UserAgent(), clientIP(r))
	if err != nil {
		journal("pair: %v", err)
		return false
	}
	// MaxAge large : l'échéance réelle (90 jours sans visite) est tenue côté serveur.
	http.SetCookie(w, &http.Cookie{Name: devCookie, Value: c, Path: "/", MaxAge: 400 * 24 * 3600, HttpOnly: true, Secure: requestHTTPS(r), SameSite: http.SameSiteLaxMode})
	journal("pair: appareil jumelé depuis %s", clientIP(r))
	http.Redirect(w, r, "/", http.StatusSeeOther)
	return true
}

func routesPair(mux *http.ServeMux) {
	mux.HandleFunc("POST /pair", func(w http.ResponseWriter, r *http.Request) {
		r, d := reqMode(r)
		var uid int64
		if d.on {
			g, _ := r.Context().Value(gateKey{}).(*gateInfo)
			if g == nil || g.p == nil || g.p.User == nil || !g.p.MFA {
				writeJSON(w, 403, map[string]any{"ok": false, "out": L("srv.auth.droitRefuse")})
				return
			}
			uid = g.p.User.ID
		}
		scheme := "http"
		if requestHTTPS(r) {
			scheme = "https"
		}
		writeJSON(w, 200, map[string]any{"ok": true, "url": scheme + "://" + r.Host + "/pair?c=" + newPairCode(uid), "ttl": int(pairTTL / time.Second)})
	})
	// Mode comptes ; en mode jeton, withToken répond avant (redeemDevice).
	mux.HandleFunc("GET /pair", func(w http.ResponseWriter, r *http.Request) {
		r, d := reqMode(r)
		if d.on && redeemSession(w, r, d.st) {
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	// Mode jeton seulement : en mode comptes, les sessions du compte (/auth/sessions) en tiennent lieu.
	mux.HandleFunc("GET /devices", func(w http.ResponseWriter, r *http.Request) {
		if _, d := reqMode(r); d.on {
			http.NotFound(w, r)
			return
		}
		devMu.Lock()
		ds, err := loadDevices()
		devMu.Unlock()
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "devices": liveDevices(ds)})
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

// redeemSession : GET /pair?c=… en mode comptes. La session naît complète (second facteur acquis) : le code a été
// demandé par une session complète du même compte.
func redeemSession(w http.ResponseWriter, r *http.Request, st *authStore) bool {
	p, ok := takePairCode(r.URL.Query().Get("c"))
	if !ok || p.uid == 0 || st == nil {
		return false
	}
	ip := clientIP(r)
	c, err := st.NewSession(p.uid, ip, r.UserAgent())
	var pr *Principal
	if err == nil {
		pr, err = st.Session(c)
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
	http.Redirect(w, r, "/", http.StatusSeeOther)
	return true
}
