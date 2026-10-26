package main

// Pages servies hors connexion (mode jeton) : saisie du jeton et confirmation d'un jumelage. Elles portent le manifeste,
// les icônes et les métas d'iOS : « Sur l'écran d'accueil » depuis l'une d'elles installe une PWA complète, et une PWA
// qui a perdu son cookie peut se reconnecter sans repasser par Safari.

import (
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

var gateTmpl = template.Must(template.New("gate").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>{{.Title}}</title>
<link rel="manifest" href="/manifest.webmanifest"><link rel="apple-touch-icon" href="/icons/apple-touch-icon.png">
<link rel="icon" href="/favicon.ico" sizes="any"><meta name="theme-color" content="#5a9a3e">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:17px -apple-system,system-ui,sans-serif;background:#111418;color:#e8eaed;padding:env(safe-area-inset-top) 1rem env(safe-area-inset-bottom)}
main{width:100%;max-width:22rem;text-align:center}p{color:#a0a4ab;line-height:1.5}.err{color:#f87171}
form{display:grid;gap:.75rem;margin-top:1.25rem}input{font:inherit;font-size:16px;min-height:44px;padding:0 .75rem;border:1px solid #3a3f47;border-radius:.6rem;background:#1b1f25;color:inherit}
.code{font:600 1.6rem ui-monospace,monospace;letter-spacing:.15em;text-align:center}a{color:#7cc15a}
button{font:inherit;font-weight:600;min-height:44px;padding:.75rem 1.75rem;border:0;border-radius:.6rem;background:#5a9a3e;color:#fff}
@media (prefers-color-scheme:light){body{background:#fff;color:#111418}p{color:#555}input{background:#fff;border-color:#ccc}}</style></head>
<body><main><h1>{{.Title}}</h1><p>{{.Text}}</p>{{if .Err}}<p class="err" role="alert">{{.Err}}</p>{{end}}
{{if .Action}}<form method="post" action="{{.Action}}">{{if .Hidden}}<input type="hidden" name="c" value="{{.Hidden}}">{{end}}
{{if .Field}}<input type="password" name="token" autocomplete="current-password" aria-label="{{.Field}}" placeholder="{{.Field}}" required autofocus>{{end}}
{{if .Code}}<input class="code" name="c" inputmode="numeric" autocomplete="one-time-code" maxlength="12" aria-label="{{.Title}}" placeholder="0000 0000" required autofocus>{{end}}
<button>{{.Button}}</button></form>{{end}}{{if .Link}}<p><a href="{{.Link}}">{{.LinkText}}</a></p>{{end}}</main></body></html>
`))

type gateData struct {
	Lang, Title, Text, Err, Action, Hidden, Field, Button, Link, LinkText string
	Code                                                                  bool // champ du code de jumelage
}

func gatePage(w http.ResponseWriter, code int, d gateData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; manifest-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(code)
	d.Lang = uiLang()
	gateTmpl.Execute(w, d)
}

// wantsPage : une navigation du navigateur (barre d'adresse, PWA), pas un appel de l'application.
func wantsPage(r *http.Request) bool {
	return r.Method == "GET" && (r.Header.Get("Sec-Fetch-Mode") == "navigate" || strings.Contains(r.Header.Get("Accept"), "text/html"))
}

func tokenPage(w http.ResponseWriter, code int, errKey string) {
	d := gateData{Title: "Glaude Code", Text: L("srv.token.text"), Action: "/token-login", Field: L("srv.token.field"), Button: L("srv.token.button"), Link: "/pair", LinkText: L("srv.pair.title")}
	if errKey != "" {
		d.Err = L(errKey)
	}
	gatePage(w, code, d)
}

// Essais du jeton par le formulaire, par minute : tokenMaxTries échecs par adresse (clientIP, derrière un proxy déclaré
// dans GLD_TRUSTED_PROXIES), et tokenMaxGlobal pour tout le serveur contre un essai réparti sur beaucoup d'adresses.
// Les appareils déjà connectés (cookie) ne passent pas par là.
const (
	tokenMaxTries  = 10
	tokenMaxGlobal = 100
)

type tryWin struct {
	n     int
	since time.Time
}

func (t *tryWin) at(now time.Time) *tryWin {
	if now.Sub(t.since) > time.Minute {
		t.n, t.since = 0, now
	}
	return t
}

var tokenTries = struct {
	sync.Mutex
	all tryWin
	by  map[string]*tryWin
}{by: map[string]*tryWin{}}

func resetTokenTries() {
	tokenTries.Lock()
	tokenTries.all, tokenTries.by = tryWin{}, map[string]*tryWin{}
	tokenTries.Unlock()
}

// tokenTry réserve un essai, sous le même verrou que le contrôle : des requêtes simultanées ne passent pas toutes avant
// le premier échec. false : l'adresse ou le serveur a épuisé ses essais. Un essai réussi se rend par tokenRefund.
func tokenTry(ip string) bool {
	now := authNow()
	tokenTries.Lock()
	defer tokenTries.Unlock()
	w := tokenTries.by[ip]
	if w == nil {
		if len(tokenTries.by) >= 4096 { // ponytail: purge complète au plafond, les fenêtres ne durent qu'une minute
			tokenTries.by = map[string]*tryWin{}
		}
		w = &tryWin{}
		tokenTries.by[ip] = w
	}
	w.at(now)
	tokenTries.all.at(now)
	if w.n >= tokenMaxTries || tokenTries.all.n >= tokenMaxGlobal {
		return false
	}
	w.n++
	tokenTries.all.n++
	return true
}

func tokenRefund(ip string) {
	tokenTries.Lock()
	defer tokenTries.Unlock()
	if w := tokenTries.by[ip]; w != nil && w.n > 0 {
		w.n--
	}
	if tokenTries.all.n > 0 {
		tokenTries.all.n--
	}
}

// tokenLogin : POST /token-login, formulaire de la page du jeton.
func tokenLogin(w http.ResponseWriter, r *http.Request, token string) {
	if pairCOP.Check(r) != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ip := clientIP(r)
	if !tokenTry(ip) {
		w.Header().Set("Retry-After", "60")
		tokenPage(w, http.StatusTooManyRequests, "srv.token.tooMany")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<12)
	if !sameToken(r.PostFormValue("token"), token) {
		journal("token: jeton refusé depuis %s", ip)
		tokenPage(w, http.StatusUnauthorized, "srv.token.bad")
		return
	}
	tokenRefund(ip)
	http.SetCookie(w, &http.Cookie{Name: tokenCookie, Value: token, Path: "/", MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: requestHTTPS(r), SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
