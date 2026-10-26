package main

// Jumelage d'un appareil par QR code. Un navigateur déjà connecté demande un code à usage unique (deux minutes) et
// l'affiche en QR. L'adresse ouvre une page de confirmation sans effet (un aperçu de lien ne consomme rien) ; son bouton
// envoie un POST de même origine qui crée la session de l'appareil, sans qu'il voie le jeton ni le mot de passe.
// Mode jeton : session d'appareil (devices.json, 90 jours sans visite), liée au jeton : en changer coupe tous les appareils.
// Mode comptes : session du compte dans auth.db, second facteur acquis, tant que la session qui a émis le code vit.
// Sur iPhone, l'appareil photo ouvre l'adresse dans Safari : la PWA ajoutée ensuite à l'écran d'accueil en copie le cookie.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
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
	uid int64  // 0 : mode jeton
	sid string // session émettrice (mode comptes)
}

var (
	pairMu    sync.Mutex
	pairCodes = map[string]pairCode{}
)

func newPairCode(uid int64, sid string) string {
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
	pairCodes[c] = pairCode{now.Add(pairTTL), uid, sid}
	return c
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

var pairTmpl = template.Must(template.New("pair").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:17px -apple-system,system-ui,sans-serif;background:#111418;color:#e8eaed}
main{max-width:22rem;padding:1.5rem;text-align:center}p{color:#a0a4ab;line-height:1.5}
button{font:inherit;font-weight:600;min-height:44px;padding:.75rem 1.75rem;border:0;border-radius:.6rem;background:#5a9a3e;color:#fff}
@media (prefers-color-scheme:light){body{background:#fff;color:#111418}p{color:#555}}</style></head>
<body><main><h1>{{.Title}}</h1><p>{{.Text}}</p>{{if .Code}}<form method="post" action="/pair"><input type="hidden" name="c" value="{{.Code}}"><button>{{.Button}}</button></form>{{end}}</main></body></html>
`))

func pairPage(w http.ResponseWriter, code int, title, text, c string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer") // le code est dans l'adresse
	w.WriteHeader(code)
	pairTmpl.Execute(w, map[string]string{"Lang": uiLang(), "Title": title, "Text": text, "Code": c, "Button": L("srv.pair.button")})
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
	expired := func() { pairPage(w, http.StatusGone, L("srv.pair.title"), L("srv.pair.expired"), "") }
	if r.Method == "GET" {
		c := r.URL.Query().Get("c")
		p, ok := pairLookup(c, false)
		if !ok || (p.uid != 0) != (st != nil) {
			expired()
			return
		}
		text := L("srv.pair.textToken")
		if st != nil {
			u, err := st.userByID(st.db, p.uid)
			if err != nil {
				expired()
				return
			}
			text = L("srv.pair.textAccount", "A1", u.Name)
		}
		pairPage(w, 200, L("srv.pair.title"), text, c)
		return
	}
	if pairCOP.Check(r) != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<12)
	p, ok := pairLookup(r.PostFormValue("c"), true)
	if !ok || (p.uid != 0) != (st != nil) {
		expired()
		return
	}
	ip := clientIP(r)
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
		scheme := "http"
		if requestHTTPS(r) {
			scheme = "https"
		}
		writeJSON(w, 200, map[string]any{"ok": true, "url": scheme + "://" + r.Host + "/pair?c=" + newPairCode(uid, sid), "ttl": int(pairTTL / time.Second)})
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
