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
	childTokenEnv = "GLD_WEB_CHILD_TOKEN"
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

func homesFile() string { return filepath.Join(cfgDir(), "gld-web-homes.json") }

// homesFileMu sérialise les écritures du fichier : deux attributions d'identifiant concurrentes en donneraient deux différents.
var (
	homesFileMu  sync.Mutex
	mainNameOnce sync.Once
)

func readHomes() []Home {
	b, err := os.ReadFile(homesFile())
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
	tmp := homesFile() + ".tmp"
	if err := writeSynced(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, homesFile())
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
			r.Out.Header.Set(childHeader, token)
		},
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

// childEnv : l'environnement du processus principal, sans le jeton ni les réglages d'écoute du serveur.
func childEnv() []string {
	var out []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if k == "GLD_WEB_TOKEN" || k == "GLD_WEB_PORT" || k == "GLD_WEB_LISTEN" || k == "CLAUDE_CONFIG_DIR" || k == "CC_WEB_PORT" || k == childTokenEnv {
			continue
		}
		out = append(out, e)
	}
	return out
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

// withHomes : les requêtes d'un navigateur qui a choisi un autre dossier vont à l'enfant correspondant ; /homes* reste local.
func withHomes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g := gateFrom(r)
		if g != nil && g.local { // routes de la passerelle (connexion, admin, statiques) : jamais relayées
			next.ServeHTTP(w, r)
			return
		}
		if name := homeFrom(r); name != "" && !strings.HasPrefix(r.URL.Path, "/homes") {
			p, err := homeProxy(name)
			if err != nil {
				journal("homes %s : %v", name, err)
				// Garde active : le droit a été vérifié pour ce home, servir un autre home le contournerait.
				if g != nil || gateActive(r) {
					writeJSON(w, 503, map[string]any{"ok": false, "out": L("srv.homes.indisponible")})
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
	mux.HandleFunc("GET /homes", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("POST /homes-save", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Homes []Home }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil || req.Homes == nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		err := saveHomes(req.Homes)
		journal("homes-save %d dossier(s) : %v", len(req.Homes), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		keep := map[string]string{}
		for _, h := range req.Homes {
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
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.homes.dossiersDeConfigurationEnregistres")})
	})
	mux.HandleFunc("POST /homes-sync-plan", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ From, To string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		items, err := syncPlan(req.From, req.To)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": "", "items": items})
	})
	mux.HandleFunc("POST /homes-sync-apply", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			From, To string
			Picks    []SyncPick
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil || len(req.Picks) > 2000 {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		out, err := syncApply(req.From, req.To, req.Picks)
		journal("homes-sync %q → %q : %d élément(s) : %v", req.From, req.To, len(req.Picks), errOr(err))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "out": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "out": out})
	})
	mux.HandleFunc("POST /homes-select", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": L("srv.http_util.requeteInvalide")})
			return
		}
		if req.Name != "" {
			if _, err := homeProxy(req.Name); err != nil {
				writeJSON(w, 200, map[string]any{"ok": false, "out": fmt.Sprint(err)})
				return
			}
		}
		c := &http.Cookie{Name: homeCookie, Value: url.QueryEscape(req.Name), Path: "/", SameSite: http.SameSiteStrictMode, HttpOnly: true, MaxAge: 365 * 24 * 3600}
		if req.Name == "" {
			c.MaxAge = -1
		}
		http.SetCookie(w, c)
		writeJSON(w, 200, map[string]any{"ok": true, "out": L("srv.homes.dossierDeConfigurationChoisi")})
	})
}
