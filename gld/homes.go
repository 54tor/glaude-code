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
	rightsHeader  = "X-Gld-Rights"
	childTokenEnv = "GLD_WEB_CHILD_TOKEN"
	// ideMainCfgEnv : la config du processus principal, qu'un enfant doit aussi refuser comme racine de l'espace.
	ideMainCfgEnv = "GLD_WEB_MAIN_CONFIG"
)

// childAuthorized : un gld enfant (lancé avec un jeton) n'accepte que les requêtes de sa passerelle ; le processus principal accepte tout.
func childAuthorized(request *http.Request) bool {
	return childToken == "" || sameToken(request.Header.Get(childHeader), childToken)
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

// onHomesRemoved reçoit les identifiants des homes retirés par PUT /v1/homes ; branché par le mode multi-utilisateur.
var onHomesRemoved func(request *http.Request, ids []string)

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

func readHomesFile(path string) []Home {
	content, err := readTrusted(path)
	if err != nil {
		return []Home{}
	}
	var homesDocument struct{ Homes []Home }
	if json.Unmarshal(content, &homesDocument) != nil || homesDocument.Homes == nil {
		return []Home{}
	}
	return homesDocument.Homes
}

// loadHomes attribue et enregistre un identifiant aux homes d'un fichier antérieur qui n'en ont pas.
func loadHomes() []Home {
	homes := readHomes()
	if slices.ContainsFunc(homes, func(home Home) bool { return strings.EqualFold(home.Name, mainHomeID) }) {
		mainNameOnce.Do(func() { journal("homes : un dossier nommé « main » (nom réservé) : le renommer") })
	}
	if !slices.ContainsFunc(homes, func(home Home) bool { return home.ID == "" }) {
		return homes
	}
	homesFileMu.Lock()
	defer homesFileMu.Unlock()
	homes = readHomes()
	changed := false
	for homeIndex := range homes {
		if homes[homeIndex].ID == "" {
			homes[homeIndex].ID, changed = newHomeID(), true
		}
	}
	if changed {
		if err := writeHomes(homes); err != nil {
			journal("homes : identifiants non enregistrés : %v", err)
		}
	}
	return homes
}

func newHomeID() string {
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	return hex.EncodeToString(randomBytes)
}

func writeHomes(homes []Home) error {
	encoded, _ := json.MarshalIndent(map[string]any{"homes": homes}, "", "  ")
	return rWriteAtomic(cfgDir(), homesFile(), encoded, 0o600)
}

// homeIDFor : "" désigne le home principal ; un home nommé « main » garde son propre identifiant.
func homeIDFor(name string) (string, bool) {
	if name == "" {
		return mainHomeID, true
	}
	home, found := homeByName(name)
	return home.ID, found && home.ID != ""
}

// saveHomes valide la liste puis remplace le fichier (écriture atomique).
func saveHomes(homes []Home) error {
	seen := map[string]bool{}
	for homeIndex := range homes {
		home := &homes[homeIndex]
		home.Name = strings.TrimSpace(spaceRe.ReplaceAllString(home.Name, " "))
		if home.Name == "" || utf8.RuneCountInString(home.Name) > 40 || !homeNameRe.MatchString(home.Name) || seen[home.Name] {
			return terr("srv.homes.nomDeDossierDe")
		}
		if strings.EqualFold(home.Name, mainHomeID) {
			return terr("srv.homes.nomReserve")
		}
		seen[home.Name] = true
		home.Dir = filepath.Clean(home.Dir)
		if !filepath.IsAbs(home.Dir) || home.Dir == cfgDir() {
			return terr("srv.homes.unCheminAbsoluDifferent", "A1", home.Name)
		}
		if fileInfo, err := os.Stat(home.Dir); err != nil || !fileInfo.IsDir() {
			return terr("srv.homes.dossierIntrouvable", "A1", home.Name)
		}
	}
	if len(homes) > 20 {
		return terr("srv.homes.tropDeDossiersDe")
	}
	homesFileMu.Lock()
	defer homesFileMu.Unlock()
	// Un identifiant fourni n'est gardé que s'il existe déjà : un client ne peut pas reprendre celui d'un home supprimé.
	byID, byName := map[string]bool{}, map[string]string{}
	for _, storedHome := range readHomes() {
		if storedHome.ID != "" {
			byID[storedHome.ID], byName[storedHome.Name] = true, storedHome.ID
		}
	}
	used := map[string]bool{}
	for homeIndex := range homes {
		home := &homes[homeIndex]
		if !byID[home.ID] || used[home.ID] {
			home.ID = byName[home.Name]
		}
		if home.ID == "" || used[home.ID] {
			home.ID = newHomeID()
		}
		used[home.ID] = true
	}
	return writeHomes(homes)
}

// mergeHomes : en mode multi-utilisateur, l'interface n'envoie que les homes visibles de l'admin. Un home du fichier absent
// de la liste (ni par identifiant ni par nom) est donc conservé ; seuls les identifiants de removed sont retirés.
func mergeHomes(before, sent []Home, removed []string) []Home {
	merged := []Home{}
	for _, sentHome := range sent {
		if sentHome.ID == "" || !slices.Contains(removed, sentHome.ID) {
			merged = append(merged, sentHome)
		}
	}
	for _, previousHome := range before {
		if slices.Contains(removed, previousHome.ID) {
			continue
		}
		if !slices.ContainsFunc(sent, func(sentHome Home) bool {
			return (previousHome.ID != "" && sentHome.ID == previousHome.ID) || sentHome.Name == previousHome.Name
		}) {
			merged = append(merged, previousHome)
		}
	}
	return merged
}

func homeByName(name string) (Home, bool) {
	for _, home := range loadHomes() {
		if home.Name == name {
			return home, true
		}
	}
	return Home{}, false
}

func freePort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	_, listenedPort, err := net.SplitHostPort(listener.Addr().String())
	return listenedPort, err
}

func childProxy(target *url.URL, token string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(proxyRequest *httputil.ProxyRequest) {
			proxyRequest.SetURL(target) // le Host devient celui de l'enfant, que sa garde anti DNS-rebinding accepte
			// La passerelle a déjà jugé l'origine de la requête (anti CSRF) et ses droits : l'enfant ne reçoit ni Origin ni
			// Sec-Fetch-*, qu'il ne pourrait comparer qu'à sa propre adresse, ni cookie (session, jeton, choix de dossier) ni
			// Authorization. Il exige le jeton de la passerelle.
			for headerName := range proxyRequest.Out.Header {
				if strings.HasPrefix(strings.ToLower(headerName), "sec-fetch-") {
					proxyRequest.Out.Header.Del(headerName)
				}
			}
			proxyRequest.Out.Header.Del("Origin")
			proxyRequest.Out.Header.Del("Cookie")
			proxyRequest.Out.Header.Del("Authorization")
			proxyRequest.Out.Header.Del(ideAuditHeader)
			// Auteur des messages d'équipe : toujours effacé côté client, puis posé d'après le principal authentifié.
			proxyRequest.Out.Header.Del(userHeader)
			if gate := gateFrom(proxyRequest.In); gate != nil && gate.p != nil {
				proxyRequest.Out.Header.Set(userHeader, principalName(gate))
			}
			// Droits de l'appelant sur ce home : l'enfant n'a pas de garde et ne croit cet en-tête qu'avec le jeton d'enfant.
			proxyRequest.Out.Header.Del(rightsHeader)
			if gate := gateFrom(proxyRequest.In); gate != nil && gate.p != nil {
				var list []string
				if homeID, found := homeIDFor(homeFrom(proxyRequest.In)); found && (gate.p.Token || gate.s != nil) {
					if rights, err := gate.rights(homeID); err == nil {
						for _, right := range allRights {
							if rights[right] {
								list = append(list, string(right))
							}
						}
					}
				}
				proxyRequest.Out.Header.Set(rightsHeader, strings.Join(list, ","))
			}
			proxyRequest.Out.Header.Del(guestHeader)
			proxyRequest.Out.Header.Del(authorHeader)
			proxyRequest.Out.Header.Del(inviteHeader)
			proxyRequest.Out.Header.Del(guestHTTPSHeader)
			proxyRequest.Out.Header.Del(guestQuietHeader)
			if sec, _ := proxyRequest.In.Context().Value(guestRelayKey{}).(string); sec != "" {
				proxyRequest.Out.Header.Set(guestHeader, sec)
				if quiet, _ := proxyRequest.In.Context().Value(guestQuietKey{}).(bool); quiet {
					proxyRequest.Out.Header.Set(guestQuietHeader, "1")
				}
			}
			if requestHTTPS(proxyRequest.In) { // l'enfant pose le cookie invité : il doit savoir s'il sera Secure
				proxyRequest.Out.Header.Set(guestHTTPSHeader, "1")
			}
			proxyRequest.Out.Header.Set(childHeader, token)
			proxyRequest.Out = proxyRequest.Out.WithContext(context.WithValue(proxyRequest.Out.Context(), auditIPKey{}, clientIP(proxyRequest.In)))
		},
		ModifyResponse: ideAuditRelay,
	}
}

// startHomeProc lance un gld enfant pour ce dossier et attend qu'il réponde.
func startHomeProc(home Home) (*homeProc, error) {
	self, setupErr := os.Executable()
	if setupErr != nil {
		return nil, setupErr
	}
	port, setupErr := freePort()
	if setupErr != nil {
		return nil, setupErr
	}
	token := newToken()
	cmd := exec.CommandContext(homeCtx, self, "web")
	cmd.Env = append(childEnv(), "CLAUDE_CONFIG_DIR="+home.Dir, "GLD_WEB_PORT="+port, "GLD_WEB_LISTEN=127.0.0.1", childTokenEnv+"="+token)
	if runAsOn() {
		cmd.Env = append(cmd.Env, runAsChildEnv+"=1")
		applyRunAs(cmd, childTokenEnv, "GLD_WEB_PORT", "GLD_WEB_LISTEN", ideMainCfgEnv)
	}
	cmd.Stderr, cmd.Stdout = io.Discard, io.Discard
	if startErr := cmd.Start(); startErr != nil {
		return nil, startErr
	}
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:" + port}
	process := &homeProc{dir: home.Dir, cmd: cmd, port: port, done: make(chan struct{})}
	go func() { cmd.Wait(); close(process.done) }()
	process.proxy = childProxy(target, token)
	for attempt := 0; attempt < 100; attempt++ {
		select {
		case <-process.done:
			return nil, terr("srv.homes.leProcessusEnfantS")
		default:
		}
		if connection, dialErr := net.DialTimeout("tcp", target.Host, 200*time.Millisecond); dialErr == nil {
			connection.Close()
			return process, nil
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
	var environment []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GLD_AUTH_KEY=") {
			environment = append(environment, entry)
		}
	}
	return environment
}

// childEnv : l'environnement du processus principal, sans le jeton ni les réglages d'écoute du serveur.
func childEnv() []string {
	var environment []string
	for _, entry := range environNoAuth() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "GLD_WEB_TOKEN" || key == "GLD_WEB_PORT" || key == "GLD_WEB_LISTEN" || key == "CLAUDE_CONFIG_DIR" || key == "CC_WEB_PORT" || key == childTokenEnv || key == ideMainCfgEnv {
			continue
		}
		environment = append(environment, entry)
	}
	mainConfigDir := cfgDir()
	if inheritedMainConfig := os.Getenv(ideMainCfgEnv); inheritedMainConfig != "" {
		mainConfigDir = inheritedMainConfig
	}
	return append(environment, ideMainCfgEnv+"="+mainConfigDir)
}

// homeFrom : le dossier choisi par ce navigateur ("" : celui du processus principal). Le nom est encodé dans le cookie.
func homeFrom(request *http.Request) string {
	cookie, err := request.Cookie(homeCookie)
	if err != nil {
		return ""
	}
	name, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		return ""
	}
	return name
}

// homeStarting : un démarrage en cours pour un nom ; les appels concurrents attendent ce résultat au lieu de lancer un doublon.
type homeStarting struct {
	done chan struct{}
	p    *homeProc
	err  error
}

// homeProxy : le proxy de l'enfant de ce dossier, lancé au besoin. Le démarrage (jusqu'à dix secondes) se fait hors de
// homesMu, que GET /v1/homes et l'enregistrement des dossiers prennent aussi.
func homeProxy(name string) (http.Handler, error) {
	home, found := homeByName(name)
	if !found {
		return nil, terr("srv.homes.dossierDeConfigurationInconnu")
	}
	homesMu.Lock()
	if running := homeProcs[name]; running != nil {
		select {
		case <-running.done:
			delete(homeProcs, name)
		default:
			homesMu.Unlock()
			return running.proxy, nil
		}
	}
	if pending := homeStartups[name]; pending != nil {
		homesMu.Unlock()
		<-pending.done
		if pending.err != nil {
			return nil, pending.err
		}
		return pending.p.proxy, nil
	}
	starting := &homeStarting{done: make(chan struct{})}
	homeStartups[name] = starting
	homesMu.Unlock()

	process, err := homeStart(home)
	// Le dossier a pu être retiré ou déplacé pendant le démarrage : le processus ne servirait plus le bon dossier.
	current, still := homeByName(name)
	homesMu.Lock()
	delete(homeStartups, name)
	if err == nil && (!still || current.Dir != home.Dir) {
		if process.cmd != nil && process.cmd.Process != nil {
			process.cmd.Process.Kill()
		}
		process, err = nil, terr("srv.homes.dossierDeConfigurationInconnu")
	}
	if err == nil {
		homeProcs[name] = process
	}
	starting.p, starting.err = process, err
	homesMu.Unlock()
	close(starting.done)
	if err != nil {
		return nil, err
	}
	return process.proxy, nil
}

func stopHomes() {
	homesMu.Lock()
	defer homesMu.Unlock()
	for name, process := range homeProcs {
		if process.cmd != nil && process.cmd.Process != nil {
			process.cmd.Process.Kill()
		}
		delete(homeProcs, name)
	}
}

// withHomes : les requêtes d'un navigateur qui a choisi un autre dossier vont à l'enfant correspondant ; /v1/homes* reste local.
func withHomes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		gate := gateFrom(request)
		if gate != nil && gate.local { // routes de la passerelle (connexion, admin, statiques) : jamais relayées
			next.ServeHTTP(responseWriter, request)
			return
		}
		if name := homeFrom(request); name != "" && !strings.HasPrefix(request.URL.Path, "/v1/homes") {
			proxy, err := homeProxy(name)
			if err != nil {
				journal("homes %s : %v", name, err)
				// Garde active : le droit a été vérifié pour ce home, servir un autre home le contournerait.
				if gate != nil || gateActive(request) {
					writeErr(responseWriter, 503, "unavailable", L("srv.homes.indisponible"))
					return
				}
				// dossier retiré ou enfant en panne : retour au dossier courant
				http.SetCookie(responseWriter, &http.Cookie{Name: homeCookie, Path: "/", MaxAge: -1})
				next.ServeHTTP(responseWriter, request)
				return
			}
			if strings.Contains(strings.ToLower(request.Header.Get("Upgrade")), "websocket") && !originIsHost(request) {
				writeErr(responseWriter, 403, "forbidden", L("srv.http_util.refuse"))
				return
			}
			proxy.ServeHTTP(responseWriter, request)
			return
		}
		next.ServeHTTP(responseWriter, request)
	})
}

// originIsHost : l'Origin de la requête désigne ce serveur. CrossOriginProtection laisse passer les GET, dont l'ouverture d'un
// WebSocket : la passerelle en juge ici, avant de retirer l'Origin, et l'enfant se fie ensuite à son jeton (termweb.go).
func originIsHost(request *http.Request) bool {
	origin, err := url.Parse(request.Header.Get("Origin"))
	return err == nil && origin.Host != "" && strings.EqualFold(origin.Host, request.Host)
}

func routesHomes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/homes", func(responseWriter http.ResponseWriter, request *http.Request) {
		current := homeFrom(request)
		homesMu.Lock()
		running := map[string]bool{}
		for name, process := range homeProcs {
			select {
			case <-process.done:
			default:
				running[name] = true
			}
		}
		homesMu.Unlock()
		type row struct {
			Home
			Running bool `json:"running"`
		}
		gate := gateFrom(request)
		rows := []row{}
		for _, home := range loadHomes() {
			if gate.homeAllowed(home.ID) {
				rows = append(rows, row{home, running[home.Name]})
			}
		}
		mainConfigDir := cfgDir()
		if !gate.homeAllowed(mainHomeID) {
			mainConfigDir = ""
		}
		writeJSON(responseWriter, 200, map[string]any{"current": current, "main": mainConfigDir, "mainId": mainHomeID, "homes": rows})
	})
	mux.HandleFunc("PUT /v1/homes", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			Homes   []Home
			Removed []string
		}
		if !decodeBody(responseWriter, request, &body, 1<<16) {
			return
		}
		if body.Homes == nil {
			writeErr(responseWriter, 400, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		before := readHomes()
		list := body.Homes
		if gateFrom(request) != nil {
			// Le nom d'un home existant, peut-être invisible de l'admin, ne se reprend pas sans son identifiant : sinon le
			// home serait repointé vers un autre dossier pour ceux qui y ont des droits.
			for _, sentHome := range body.Homes {
				name := strings.TrimSpace(spaceRe.ReplaceAllString(sentHome.Name, " "))
				if slices.ContainsFunc(before, func(existingHome Home) bool {
					return existingHome.Name == name && (sentHome.ID == "" || sentHome.ID != existingHome.ID)
				}) {
					writeErr(responseWriter, 409, "conflict", L("srv.homes.nomDejaPris", "A1", name))
					return
				}
			}
			list = mergeHomes(before, body.Homes, body.Removed)
		}
		err := saveHomes(list)
		journal("homes-save %d dossier(s) : %v", len(list), errOr(err))
		if err != nil {
			writeErr(responseWriter, 400, "bad_request", err.Error())
			return
		}
		if onHomesRemoved != nil {
			after := readHomes()
			var gone []string
			for _, home := range before {
				if home.ID != "" && !slices.ContainsFunc(after, func(remainingHome Home) bool { return remainingHome.ID == home.ID }) {
					gone = append(gone, home.ID)
				}
			}
			if len(gone) > 0 {
				onHomesRemoved(request, gone)
			}
		}
		keep := map[string]string{}
		for _, home := range list {
			keep[home.Name] = home.Dir
		}
		homesMu.Lock() // un dossier retiré (ou dont le chemin a changé) n'a plus de processus
		for name, process := range homeProcs {
			if dir, kept := keep[name]; !kept || dir != process.dir {
				if process.cmd != nil && process.cmd.Process != nil {
					process.cmd.Process.Kill()
				}
				delete(homeProcs, name)
			}
		}
		homesMu.Unlock()
		responseWriter.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/homes/sync-plans", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ From, To string }
		if !decodeBody(responseWriter, request, &body, 1<<10) {
			return
		}
		items, err := syncPlan(body.From, body.To)
		if err != nil {
			writeErr(responseWriter, 400, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"items": items})
	})
	mux.HandleFunc("POST /v1/homes/syncs", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct {
			From, To string
			Picks    []SyncPick
		}
		if !decodeBody(responseWriter, request, &body, 1<<16) {
			return
		}
		if len(body.Picks) > 2000 {
			writeErr(responseWriter, 400, "too_many_items", L("srv.http_util.requeteInvalide"))
			return
		}
		message, err := syncApply(body.From, body.To, body.Picks)
		journal("homes-sync %q → %q : %d élément(s) : %v", body.From, body.To, len(body.Picks), errOr(err))
		if err != nil {
			writeErr(responseWriter, 400, "bad_request", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"message": message})
	})
	mux.HandleFunc("PUT /v1/homes/current", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{ Name string }
		if !decodeBody(responseWriter, request, &body, 1<<10) {
			return
		}
		if body.Name != "" {
			if _, err := homeProxy(body.Name); err != nil {
				writeErr(responseWriter, 404, "not_found", fmt.Sprint(err))
				return
			}
		}
		cookie := &http.Cookie{Name: homeCookie, Value: url.QueryEscape(body.Name), Path: "/", SameSite: http.SameSiteStrictMode, HttpOnly: true, MaxAge: 365 * 24 * 3600}
		if body.Name == "" {
			cookie.MaxAge = -1
		}
		http.SetCookie(responseWriter, cookie)
		responseWriter.WriteHeader(http.StatusNoContent)
	})
}
