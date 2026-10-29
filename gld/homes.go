package main

// Plusieurs dossiers de configuration de Claude Code (CLAUDE_CONFIG_DIR : un compte pro, un compte perso…).
// gld lit un seul dossier par processus : le processus principal est la passerelle. Chaque autre dossier est servi par un gld enfant
// (même binaire, CLAUDE_CONFIG_DIR et port propres, écoute locale) et le cookie gld_home désigne celui qui répond. Tout le
// reste (sessions, mémoire, réglages, terminal) fonctionne donc tel quel pour chaque dossier, sans état partagé.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	homeCookie    = "gld_home"
	childHeader   = "X-Gld-Child"
	userHeader    = "X-Gld-User"
	childTokenEnv = "GLD_WEB_CHILD_TOKEN"
	// ideMainCfgEnv : la config du processus principal, qu'un enfant doit aussi refuser comme racine de l'espace.
	ideMainCfgEnv = "GLD_WEB_MAIN_CONFIG"
)

// childAuthorized : un gld enfant (lancé avec un jeton) n'accepte que les requêtes de sa passerelle ; le processus principal accepte tout.
func childAuthorized(r *http.Request) bool {
	return childToken == "" || sameToken(r.Header.Get(childHeader), childToken)
}

// childToken : lu au démarrage, avant que main ne retire la variable de l'environnement.
var childToken = os.Getenv(childTokenEnv)

type Home struct {
	Name string `json:"name"`
	Dir  string `json:"dir"` // dossier de configuration (celui qui contient projects/)
	// ID est la clé des droits (home_acl) : stable au renommage, jamais réattribué à un nouveau home du même nom.
	ID string `json:"id,omitempty"`
}

const mainHomeID = "main"

// onHomesRemoved reçoit les identifiants des homes retirés par /homes-save ; branché par le mode multi-utilisateur.
var onHomesRemoved func(r *http.Request, ids []string)

type homeProc struct {
	dir   string
	cmd   *exec.Cmd
	port  string
	proxy *httputil.ReverseProxy
	done  chan struct{} // fermé quand le processus a fini
}

var (
	homesMu      sync.Mutex
	homeProcs    = map[string]*homeProc{}
	homeStartups = map[string]*homeStarting{}
	homeCtx      = context.Background() // remplacé par le contexte d'arrêt du serveur
	homeNameRe   = regexp.MustCompile(`^[^\x00-\x1f/\\]+$`)
	homeStart    = startHomeProc // remplaçable dans les tests
)

const homesFileName = "gld-web-homes.json"

func homesFile() string { return filepath.Join(cfgDir(), homesFileName) }

// homesFileMu sérialise les écritures du fichier : deux attributions d'identifiant concurrentes en donneraient deux différents.
var (
	homesFileMu  sync.Mutex
	mainNameOnce sync.Once
)

func readHomes() []Home { return readHomesFile(homesFile()) }

func readHomesFile(f string) []Home {
	b, err := readTrusted(f)
	if err != nil {
		return []Home{}
	}
	var st struct{ Homes []Home }
	if json.Unmarshal(b, &st) != nil || st.Homes == nil {
		return []Home{}
	}
	return st.Homes
}

// loadHomes attribue et enregistre un identifiant aux homes d'un fichier antérieur qui n'en ont pas.
func loadHomes() []Home {
	hs := readHomes()
	if slices.ContainsFunc(hs, func(h Home) bool { return strings.EqualFold(h.Name, mainHomeID) }) {
		mainNameOnce.Do(func() { journal("homes : un dossier nommé « main » (nom réservé) : le renommer") })
	}
	if !slices.ContainsFunc(hs, func(h Home) bool { return h.ID == "" }) {
		return hs
	}
	homesFileMu.Lock()
	defer homesFileMu.Unlock()
	hs = readHomes()
	changed := false
	for i := range hs {
		if hs[i].ID == "" {
			hs[i].ID, changed = newHomeID(), true
		}
	}
	if changed {
		if err := writeHomes(hs); err != nil {
			journal("homes : identifiants non enregistrés : %v", err)
		}
	}
	return hs
}

func newHomeID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeHomes(hs []Home) error {
	b, _ := json.MarshalIndent(map[string]any{"homes": hs}, "", "  ")
	return rWriteAtomic(cfgDir(), homesFile(), b, 0o600)
}

// homeIDFor : "" désigne le home principal ; un home nommé « main » garde son propre identifiant.
func homeIDFor(name string) (string, bool) {
	if name == "" {
		return mainHomeID, true
	}
	h, ok := homeByName(name)
	return h.ID, ok && h.ID != ""
}

// saveHomes valide la liste puis remplace le fichier (écriture atomique).
func saveHomes(hs []Home) error {
	seen := map[string]bool{}
	for i := range hs {
		h := &hs[i]
		h.Name = strings.TrimSpace(spaceRe.ReplaceAllString(h.Name, " "))
		if h.Name == "" || utf8.RuneCountInString(h.Name) > 40 || !homeNameRe.MatchString(h.Name) || seen[h.Name] {
			return terr("srv.homes.nomDeDossierDe")
		}
		if strings.EqualFold(h.Name, mainHomeID) {
			return terr("srv.homes.nomReserve")
		}
		seen[h.Name] = true
		h.Dir = filepath.Clean(h.Dir)
		if !filepath.IsAbs(h.Dir) || h.Dir == cfgDir() {
			return terr("srv.homes.unCheminAbsoluDifferent", "A1", h.Name)
		}
		if fi, err := os.Stat(h.Dir); err != nil || !fi.IsDir() {
			return terr("srv.homes.dossierIntrouvable", "A1", h.Name)
		}
	}
	if len(hs) > 20 {
		return terr("srv.homes.tropDeDossiersDe")
	}
	homesFileMu.Lock()
	defer homesFileMu.Unlock()
	// Un identifiant fourni n'est gardé que s'il existe déjà : un client ne peut pas reprendre celui d'un home supprimé.
	byID, byName := map[string]bool{}, map[string]string{}
	for _, h := range readHomes() {
		if h.ID != "" {
			byID[h.ID], byName[h.Name] = true, h.ID
		}
	}
	used := map[string]bool{}
	for i := range hs {
		h := &hs[i]
		if !byID[h.ID] || used[h.ID] {
			h.ID = byName[h.Name]
		}
		if h.ID == "" || used[h.ID] {
			h.ID = newHomeID()
		}
		used[h.ID] = true
	}
	return writeHomes(hs)
}

// mergeHomes : en mode multi-utilisateur, l'interface n'envoie que les homes visibles de l'admin. Un home du fichier absent
// de la liste (ni par identifiant ni par nom) est donc conservé ; seuls les identifiants de removed sont retirés.
func mergeHomes(before, sent []Home, removed []string) []Home {
	out := []Home{}
	for _, h := range sent {
		if h.ID == "" || !slices.Contains(removed, h.ID) {
			out = append(out, h)
		}
	}
	for _, h := range before {
		if slices.Contains(removed, h.ID) {
			continue
		}
		if !slices.ContainsFunc(sent, func(s Home) bool { return (h.ID != "" && s.ID == h.ID) || s.Name == h.Name }) {
			out = append(out, h)
		}
	}
	return out
}

func homeByName(name string) (Home, bool) {
	for _, h := range loadHomes() {
		if h.Name == name {
			return h, true
		}
	}
	return Home{}, false
}

func freePort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	_, p, err := net.SplitHostPort(l.Addr().String())
	return p, err
}

func childProxy(target *url.URL, token string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target) // le Host devient celui de l'enfant, que sa garde anti DNS-rebinding accepte
			// La passerelle a déjà jugé l'origine de la requête (anti CSRF) et ses droits : l'enfant ne reçoit ni Origin ni
			// Sec-Fetch-*, qu'il ne pourrait comparer qu'à sa propre adresse, ni cookie (session, jeton, choix de dossier) ni
			// Authorization. Il exige le jeton de la passerelle.
			for k := range r.Out.Header {
				if strings.HasPrefix(strings.ToLower(k), "sec-fetch-") {
					r.Out.Header.Del(k)
				}
			}
			r.Out.Header.Del("Origin")
			r.Out.Header.Del("Cookie")
			r.Out.Header.Del("Authorization")
			r.Out.Header.Del(ideAuditHeader)
			// Auteur des messages d'équipe : toujours effacé côté client, puis posé d'après le principal authentifié.
			r.Out.Header.Del(userHeader)
			if g := gateFrom(r.In); g != nil && g.p != nil {
				r.Out.Header.Set(userHeader, principalName(g))
			}
			r.Out.Header.Del(guestHeader)
			r.Out.Header.Del(authorHeader)
			r.Out.Header.Del(inviteHeader)
			r.Out.Header.Del(guestHTTPSHeader)
			r.Out.Header.Del(guestQuietHeader)
			if sec, _ := r.In.Context().Value(guestRelayKey{}).(string); sec != "" {
				r.Out.Header.Set(guestHeader, sec)
				if q, _ := r.In.Context().Value(guestQuietKey{}).(bool); q {
					r.Out.Header.Set(guestQuietHeader, "1")
				}
			}
			if requestHTTPS(r.In) { // l'enfant pose le cookie invité : il doit savoir s'il sera Secure
				r.Out.Header.Set(guestHTTPSHeader, "1")
			}
			r.Out.Header.Set(childHeader, token)
			r.Out = r.Out.WithContext(context.WithValue(r.Out.Context(), auditIPKey{}, clientIP(r.In)))
		},
		ModifyResponse: ideAuditRelay,
	}
}

// startHomeProc lance un gld enfant pour ce dossier et attend qu'il réponde.
func startHomeProc(h Home) (*homeProc, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	token := newToken()
	cmd := exec.CommandContext(homeCtx, self, "web")
	cmd.Env = append(childEnv(), "CLAUDE_CONFIG_DIR="+h.Dir, "GLD_WEB_PORT="+port, "GLD_WEB_LISTEN=127.0.0.1", childTokenEnv+"="+token)
	if runAsOn() {
		cmd.Env = append(cmd.Env, runAsChildEnv+"=1")
		applyRunAs(cmd, childTokenEnv, "GLD_WEB_PORT", "GLD_WEB_LISTEN", ideMainCfgEnv)
	}
	cmd.Stderr, cmd.Stdout = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:" + port}
	p := &homeProc{dir: h.Dir, cmd: cmd, port: port, done: make(chan struct{})}
	go func() { cmd.Wait(); close(p.done) }()
	p.proxy = childProxy(target, token)
	for i := 0; i < 100; i++ {
		select {
		case <-p.done:
			return nil, terr("srv.homes.leProcessusEnfantS")
		default:
		}
		if c, err := net.DialTimeout("tcp", target.Host, 200*time.Millisecond); err == nil {
			c.Close()
			return p, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	cmd.Process.Kill()
	return nil, terr("srv.homes.leProcessusEnfantNe")
}

// environNoAuth : l'environnement du processus sans GLD_AUTH_KEY (clé maître de la base d'authentification) :
// ni claude, ni le terminal web, ni un home enfant n'en héritent (un `env` exécuté par Claude finirait dans un transcript).
// GLD_AUTH_DIR reste : ce n'est pas un secret, et le garde Bash s'en sert pour protéger le dossier.
func environNoAuth() []string {
	var out []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GLD_AUTH_KEY=") {
			out = append(out, e)
		}
	}
	return out
}

// childEnv : l'environnement du processus principal, sans le jeton ni les réglages d'écoute du serveur.
func childEnv() []string {
	var out []string
	for _, e := range environNoAuth() {
		k, _, _ := strings.Cut(e, "=")
		if k == "GLD_WEB_TOKEN" || k == "GLD_WEB_PORT" || k == "GLD_WEB_LISTEN" || k == "CLAUDE_CONFIG_DIR" || k == "CC_WEB_PORT" || k == childTokenEnv || k == ideMainCfgEnv {
			continue
		}
		out = append(out, e)
	}
	main := cfgDir()
	if m := os.Getenv(ideMainCfgEnv); m != "" {
		main = m
	}
	return append(out, ideMainCfgEnv+"="+main)
}

// homeFrom : le dossier choisi par ce navigateur ("" : celui du processus principal). Le nom est encodé dans le cookie.
func homeFrom(r *http.Request) string {
	c, err := r.Cookie(homeCookie)
	if err != nil {
		return ""
	}
	v, err := url.QueryUnescape(c.Value)
	if err != nil {
		return ""
	}
	return v
}

// homeStarting : un démarrage en cours pour un nom ; les appels concurrents attendent ce résultat au lieu de lancer un doublon.
type homeStarting struct {
	done chan struct{}
	p    *homeProc
	err  error
}

// homeProxy : le proxy de l'enfant de ce dossier, lancé au besoin. Le démarrage (jusqu'à dix secondes) se fait hors de
// homesMu, que /homes et l'enregistrement des dossiers prennent aussi.
func homeProxy(name string) (http.Handler, error) {
	h, ok := homeByName(name)
	if !ok {
		return nil, terr("srv.homes.dossierDeConfigurationInconnu")
	}
	homesMu.Lock()
	if p := homeProcs[name]; p != nil {
		select {
		case <-p.done:
			delete(homeProcs, name)
		default:
			homesMu.Unlock()
			return p.proxy, nil
		}
	}
	if s := homeStartups[name]; s != nil {
		homesMu.Unlock()
		<-s.done
		if s.err != nil {
			return nil, s.err
		}
		return s.p.proxy, nil
	}
	s := &homeStarting{done: make(chan struct{})}
	homeStartups[name] = s
	homesMu.Unlock()

	p, err := homeStart(h)
	// Le dossier a pu être retiré ou déplacé pendant le démarrage : le processus ne servirait plus le bon dossier.
	cur, still := homeByName(name)
	homesMu.Lock()
	delete(homeStartups, name)
	if err == nil && (!still || cur.Dir != h.Dir) {
		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Kill()
		}
		p, err = nil, terr("srv.homes.dossierDeConfigurationInconnu")
	}
	if err == nil {
		homeProcs[name] = p
	}
	s.p, s.err = p, err
	homesMu.Unlock()
	close(s.done)
	if err != nil {
		return nil, err
	}
	return p.proxy, nil
}

func stopHomes() {
	homesMu.Lock()
	defer homesMu.Unlock()
	for n, p := range homeProcs {
		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Kill()
		}
		delete(homeProcs, n)
	}
}

// withHomes : les requêtes d'un navigateur qui a choisi un autre dossier vont à l'enfant correspondant ; /v1/homes* reste local.
func withHomes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g := gateFrom(r)
		if g != nil && g.local { // routes de la passerelle (connexion, admin, statiques) : jamais relayées
			next.ServeHTTP(w, r)
			return
		}
		if name := homeFrom(r); name != "" && !strings.HasPrefix(r.URL.Path, "/v1/homes") {
			p, err := homeProxy(name)
			if err != nil {
				journal("homes %s : %v", name, err)
				// Garde active : le droit a été vérifié pour ce home, servir un autre home le contournerait.
				if g != nil || gateActive(r) {
					writeErr(w, 503, "unavailable", L("srv.homes.indisponible"))
					return
				}
				// dossier retiré ou enfant en panne : retour au dossier courant
				http.SetCookie(w, &http.Cookie{Name: homeCookie, Path: "/", MaxAge: -1})
				next.ServeHTTP(w, r)
				return
			}
			if strings.Contains(strings.ToLower(r.Header.Get("Upgrade")), "websocket") && !originIsHost(r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			p.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originIsHost : l'Origin de la requête désigne ce serveur. CrossOriginProtection laisse passer les GET, dont l'ouverture d'un
// WebSocket : la passerelle en juge ici, avant de retirer l'Origin, et l'enfant se fie ensuite à son jeton (termweb.go).
func originIsHost(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host)
}

func routesHomes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/homes", func(w http.ResponseWriter, r *http.Request) {
		cur := homeFrom(r)
		homesMu.Lock()
		running := map[string]bool{}
		for n, p := range homeProcs {
			select {
			case <-p.done:
			default:
				running[n] = true
			}
		}
		homesMu.Unlock()
		type row struct {
			Home
			Running bool `json:"running"`
		}
		g := gateFrom(r)
		rows := []row{}
		for _, h := range loadHomes() {
			if g.homeAllowed(h.ID) {
				rows = append(rows, row{h, running[h.Name]})
			}
		}
		main := cfgDir()
		if !g.homeAllowed(mainHomeID) {
			main = ""
		}
		writeJSON(w, 200, map[string]any{"current": cur, "main": main, "mainId": mainHomeID, "homes": rows})
	})
	mux.HandleFunc("PUT /v1/homes", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Homes   []Home
			Removed []string
		}
		if !decodeBody(w, r, &req, 1<<16) {
			return
		}
		if req.Homes == nil {
			writeErr(w, 400, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		before := readHomes()
		list := req.Homes
		if gateFrom(r) != nil {
			// Le nom d'un home existant, peut-être invisible de l'admin, ne se reprend pas sans son identifiant : sinon le
			// home serait repointé vers un autre dossier pour ceux qui y ont des droits.
			for _, s := range req.Homes {
				name := strings.TrimSpace(spaceRe.ReplaceAllString(s.Name, " "))
				if slices.ContainsFunc(before, func(b Home) bool { return b.Name == name && (s.ID == "" || s.ID != b.ID) }) {
					writeErr(w, 409, "conflict", L("srv.homes.nomDejaPris", "A1", name))
					return
				}
			}
			list = mergeHomes(before, req.Homes, req.Removed)
		}
		err := saveHomes(list)
		journal("homes-save %d dossier(s) : %v", len(list), errOr(err))
		if err != nil {
			writeErr(w, 400, "bad_request", err.Error())
			return
		}
		if onHomesRemoved != nil {
			after := readHomes()
			var gone []string
			for _, h := range before {
				if h.ID != "" && !slices.ContainsFunc(after, func(a Home) bool { return a.ID == h.ID }) {
					gone = append(gone, h.ID)
				}
			}
			if len(gone) > 0 {
				onHomesRemoved(r, gone)
			}
		}
		keep := map[string]string{}
		for _, h := range list {
			keep[h.Name] = h.Dir
		}
		homesMu.Lock() // un dossier retiré (ou dont le chemin a changé) n'a plus de processus
		for n, p := range homeProcs {
			if dir, ok := keep[n]; !ok || dir != p.dir {
				if p.cmd != nil && p.cmd.Process != nil {
					p.cmd.Process.Kill()
				}
				delete(homeProcs, n)
			}
		}
		homesMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/homes/sync-plans", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ From, To string }
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		items, err := syncPlan(req.From, req.To)
		if err != nil {
			writeErr(w, 400, "bad_request", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})
	mux.HandleFunc("POST /v1/homes/syncs", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			From, To string
			Picks    []SyncPick
		}
		if !decodeBody(w, r, &req, 1<<16) {
			return
		}
		if len(req.Picks) > 2000 {
			writeErr(w, 400, "too_many", L("srv.http_util.requeteInvalide"))
			return
		}
		out, err := syncApply(req.From, req.To, req.Picks)
		journal("homes-sync %q → %q : %d élément(s) : %v", req.From, req.To, len(req.Picks), errOr(err))
		if err != nil {
			writeErr(w, 400, "bad_request", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"message": out})
	})
	mux.HandleFunc("PUT /v1/homes/current", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		if !decodeBody(w, r, &req, 1<<10) {
			return
		}
		if req.Name != "" {
			if _, err := homeProxy(req.Name); err != nil {
				writeErr(w, 404, "not_found", fmt.Sprint(err))
				return
			}
		}
		c := &http.Cookie{Name: homeCookie, Value: url.QueryEscape(req.Name), Path: "/", SameSite: http.SameSiteStrictMode, HttpOnly: true, MaxAge: 365 * 24 * 3600}
		if req.Name == "" {
			c.MaxAge = -1
		}
		http.SetCookie(w, c)
		w.WriteHeader(http.StatusNoContent)
	})
}
