package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Interface React/shadcn compilée (ui/dist, non versionnée : `make install` la construit, le dossier garde un .gitkeep pour que go:embed compile).
//
//go:embed all:ui/dist
var uiFS embed.FS

// Injectée à la compilation (-ldflags "-X main.version=v1.2.3") ; « dev » hors release.
var version = "dev"

const (
	shutdownTimeout   = 5 * time.Second
	tabsAutosaveEvery = 2 * time.Minute
	waitingWatchEvery = 20 * time.Second
	titleLoopEvery    = 30 * time.Second
	keepWarmEvery     = 30 * time.Second
	trashExpireEvery  = 24 * time.Hour
	serverIdleTimeout = 2 * time.Minute
)

var (
	root      string
	port      = env("GLD_WEB_PORT", env("CC_WEB_PORT", "8799")) // CC_* : anciens noms, encore lus
	idRe      = regexp.MustCompile(`^[0-9a-f-]{36}$`)
	hosts     = map[string]bool{}
	openHosts bool // écoute hors localhost (listen.go) : le jeton, ou en mode multi-utilisateur la session, remplace la garde sur le Host
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	home, _ := os.UserHomeDir()
	root = filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "projects")
	if code, handled := dispatchCLI(os.Args); handled { // gld-matin, gld-trier, gld-rm, gld-nommer : ni serveur ni port
		os.Exit(code)
	}
	if len(os.Args) > 1 && os.Args[1] == "web" { // « gld web » : le serveur, nommé explicitement (sans argument, c'est déjà lui)
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}
	listen := flag.String("listen", env("GLD_WEB_LISTEN", "127.0.0.1"), tr(cliLang(), "cli.main.flagListen", nil))
	tokenFlag := flag.String("token", "", tr(cliLang(), "cli.main.flagToken", nil))
	showVersion := flag.Bool("version", false, tr(cliLang(), "cli.main.flagVersion", nil))
	publicURL := flag.String("public-url", "", tr(cliLang(), "cli.main.flagPublicURL", nil))
	tlsCert := flag.String("tls-cert", env("GLD_WEB_TLS_CERT", ""), tr(cliLang(), "cli.main.flagTLSCert", nil))
	tlsKey := flag.String("tls-key", env("GLD_WEB_TLS_KEY", ""), tr(cliLang(), "cli.main.flagTLSKey", nil))
	locations := flag.String("locations", env("GLD_WEB_LOCATIONS", ""), tr(cliLang(), "cli.main.flagLocations", nil))
	flag.Parse()
	setBrowseLocations(*locations)
	if *showVersion {
		fmt.Println("gld", version)
		return
	}
	hosts["127.0.0.1:"+port] = true
	hosts["localhost:"+port] = true
	hosts[net.JoinHostPort(*listen, port)] = true

	local := isLoopback(*listen)
	useTLS, err := tlsPair(*tlsCert, *tlsKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	if *publicURL != "" {
		os.Setenv("GLD_PUBLIC_URL", *publicURL)
	}
	if *tokenFlag != "" { // ponytail: l'argument reste visible dans ps ; GLD_WEB_TOKEN ou un fichier si la machine est partagée
		os.Setenv("GLD_WEB_TOKEN", *tokenFlag)
	}
	token, generated, err := takeToken(local)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Une base illisible ou une clé qui ne la déchiffre pas arrête le serveur : repartir en mode ouvert contournerait les comptes.
	accountStore, accountsOn, err := authState()
	if err != nil {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.authRefused", map[string]any{"A1": err.Error()}))
		os.Exit(1)
	}
	if accountsOn {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.authMode", nil))
	}
	if dir, dirErr := authDir(); accountStore != nil && dirErr == nil && keyFromFile.Load() {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.keyFromFile", map[string]any{"A1": filepath.Join(dir, "auth.key")}))
	}
	if runAsErr := checkRunAsSeparation(); runAsErr != nil {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.runAsRefused", map[string]any{"A1": runAsErr.Error()}))
		os.Exit(1)
	}
	if runAsOn() {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.runAsOn", map[string]any{"A1": runAsHome()}))
	}
	if _, rulesErr := installBuiltinRules(); rulesErr != nil { // non bloquant : le serveur marche sans, seules les règles manquent
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.rulesFailed", map[string]any{"A1": rulesErr.Error()}))
	}
	tokenPrincipal = tokenForChildren(token, generated)
	hostToken = token // obligatoire hors localhost ; en local, exigé dès que GLD_WEB_TOKEN est fixé (machine partagée)
	rootHandler := handler()
	if !local {
		openHosts = true // hors localhost, le Host n'est plus limité : le jeton (mode ouvert) ou la session (comptes) protège du DNS-rebinding
	}
	server := newServer(net.JoinHostPort(*listen, port), rootHandler)
	server.RegisterOnShutdown(ideCloseAll) // sinon un flux /v1/fs/watch ouvert fait attendre Shutdown jusqu'à son délai
	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	homeCtx = shutdownContext
	defer stopHomes()
	done := make(chan struct{}) // Shutdown attend les requêtes en vol : main ne doit pas sortir avant lui
	go func() {
		defer close(done)
		<-shutdownContext.Done()
		fmt.Fprintln(os.Stderr, "\n"+tr(cliLang(), "cli.main.stopped", nil))
		shutdownTimeoutContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		server.Shutdown(shutdownTimeoutContext)
	}()
	migrateData()
	go autosaveTabs(tabsAutosaveEvery)
	go watchWaiting(waitingWatchEvery)
	go titleLoop(titleLoopEvery)
	go keepWarmLoop(keepWarmEvery)
	if queueSt != nil {
		go queueLoop(shutdownContext, queueSt)
	}
	go expireTrashLoop(trashExpireEvery)
	go func() { _ = syncIndex() }() // index de recherche prêt avant la première requête
	if local && token == "" {
		fmt.Fprintln(os.Stderr, scheme+"://"+net.JoinHostPort(*listen, port))
	} else if local {
		fmt.Fprintln(os.Stderr, scheme+"://"+net.JoinHostPort(*listen, port)+"/?token="+tokenShown(token, generated))
	} else {
		if !useTLS {
			fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.plainHTTP", map[string]any{"A1": net.JoinHostPort(*listen, port)}))
		}
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.preferTunnel", nil))
		shown := tokenShown(token, generated)
		if accountsOn && generated { // le jeton généré ne vaut rien en mode multi-utilisateur : la connexion par compte le remplace
			shown = ""
		}
		for _, link := range urlsFor(scheme, *listen, port, shown) {
			fmt.Fprintln(os.Stderr, "  "+link)
		}
	}
	defer removeWorkDir()
	serve := server.ListenAndServe
	if useTLS {
		serve = func() error { return server.ListenAndServeTLS(*tlsCert, *tlsKey) }
	}
	if serveErr := serve(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, serveErr)
		os.Exit(1)
	}
	<-done
}

// État du mode multi-utilisateur, relu au plus toutes les modeEvery. Tant que auth.db n'existe pas (créée par `gld user add`),
// rien n'est créé ni ouvert : ni dossier, ni base, ni clé, ni coffre-fort.
var (
	modeNow   = time.Now
	modeEvery = 2 * time.Second
	authMu    sync.Mutex
	authSt    *authStore // nil tant que la base n'existe pas, et toujours dans un home enfant
	authErr   error      // base inutilisable : le service est refusé, jamais rendu en mode ouvert
	authUsers bool
	authSeen  bool        // des comptes ont existé : seule une table vide lue avec succès rend le mode ouvert
	authAt    time.Time   // dernier contrôle ; zéro : à refaire
	authFI    os.FileInfo // fichier ouvert : une base remplacée ne doit pas être servie depuis l'ancien inode
	purgeAt   time.Time   // dernière purge d'auth_events ; zéro à chaque ouverture de base
)

var (
	errAuthGone     = errors.New("auth.db disparue alors que des comptes existaient")
	errAuthReplaced = errors.New("auth.db remplacée : base rouverte au prochain contrôle")
)

// authState : la base (ouverte au premier contrôle qui la trouve), le mode actif (au moins un compte) et l'échec éventuel.
// Toute erreur ferme le service (503) : une base illisible ne doit jamais ouvrir l'interface sans compte.
func authState() (*authStore, bool, error) {
	if childToken != "" {
		return nil, false, nil
	}
	authMu.Lock()
	defer authMu.Unlock()
	now := modeNow()
	if !authAt.IsZero() && now.Sub(authAt) < modeEvery {
		return authSt, authUsers, authErr
	}
	authAt, authErr = now, nil
	authUsers, authErr = authCheck()
	if authErr != nil {
		journal("auth: base des comptes inutilisable : %v", authErr)
		authUsers = false
	}
	if authUsers {
		authSeen = true
		warnGuard(true, os.Stderr)
	}
	if authErr == nil && authSt != nil {
		purgeDue(authSt, now)
	}
	return authSt, authUsers, authErr
}

// purgeDue purge auth_events, les lignes mortes de throttle et les sessions expirées à l'ouverture de la base puis au plus
// une fois par 24 h ; appelée sous authMu.
func purgeDue(accountStore *authStore, now time.Time) {
	if !purgeAt.IsZero() && now.Sub(purgeAt) < eventsPurgeEvery {
		return
	}
	purgeAt = now
	if _, eventsErr := accountStore.PurgeEvents(eventsKeep); eventsErr != nil {
		journal("auth: purge du journal d'authentification : %v", eventsErr)
	}
	throttler := newThrottle(accountStore.db)
	throttler.now = func() time.Time { return now }
	if _, throttleErr := throttler.Purge(); throttleErr != nil {
		journal("auth: purge des compteurs d'essais : %v", throttleErr)
	}
	if _, sessionsErr := accountStore.PurgeSessions(); sessionsErr != nil {
		journal("auth: purge des sessions expirées : %v", sessionsErr)
	}
}

func authCheck() (bool, error) {
	dir, err := authDir()
	if err != nil { // sans dossier de configuration, `gld user add` n'aurait pas pu créer la base non plus
		if authSeen {
			return false, err
		}
		return false, nil
	}
	path := filepath.Join(dir, "auth.db")
	fileInfo, err := os.Lstat(path)
	if err != nil {
		switch {
		case !errors.Is(err, fs.ErrNotExist):
			return false, err
		case authSeen:
			return false, errAuthGone
		}
		return false, nil
	}
	if authSt != nil && !os.SameFile(authFI, fileInfo) {
		authSt.db.Close()
		authSt, authFI = nil, nil
		return false, errAuthReplaced
	}
	if authSt == nil {
		openedStore, storeErr := openAuthStore()
		if storeErr != nil {
			return false, storeErr
		}
		if authFI, storeErr = os.Lstat(path); storeErr != nil {
			openedStore.db.Close()
			return false, storeErr
		}
		authSt, purgeAt = openedStore, time.Time{}
	}
	accountsOn, err := authSt.AccountsOn()
	if err == nil && !accountsOn {
		authSeen = false
	}
	return accountsOn, err
}

type authDecision struct {
	st  *authStore
	on  bool
	err error
}

type modeKey struct{}

var authStateFn = authState

// reqMode : le mode est décidé une seule fois par requête, au premier garde qui la voit ; les suivants relisent cette
// décision, sans quoi l'expiration du cache entre deux gardes pourrait sauter le jeton puis servir en mode ouvert.
func reqMode(request *http.Request) (*http.Request, authDecision) {
	if cached, found := request.Context().Value(modeKey{}).(authDecision); found {
		return request, cached
	}
	accountStore, accountsOn, err := authStateFn()
	decision := authDecision{accountStore, accountsOn, err}
	return request.WithContext(context.WithValue(request.Context(), modeKey{}, decision)), decision
}

var xffWarned sync.Once

// warnXFF : derrière un proxy non déclaré, tous les clients partagent son adresse (limitation des essais, adresses connues).
func warnXFF(request *http.Request) {
	if os.Getenv("GLD_TRUSTED_PROXIES") != "" || request.Header.Get("X-Forwarded-For") == "" {
		return
	}
	host, _, splitErr := net.SplitHostPort(request.RemoteAddr)
	if splitErr != nil {
		host = request.RemoteAddr
	}
	if address, parseErr := netip.ParseAddr(host); parseErr == nil && (address.IsLoopback() || address.IsPrivate()) {
		xffWarned.Do(func() {
			fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.xffUntrusted", nil))
			journal("auth: X-Forwarded-For reçu de %s sans GLD_TRUSTED_PROXIES", host)
		})
	}
}

// handler : toutes les routes, derrière les gardes Host et CSRF.
func handler() http.Handler {
	mux := http.NewServeMux()
	routesStatic(mux)
	routesSessions(mux)
	routesSnippets(mux)
	routesMemory(mux)
	routesTabs(mux)
	routesConfig(mux)
	routesConfigRaw(mux)
	routesConfigPerm(mux)
	routesPermHelp(mux)
	routesHomes(mux)
	routesGit(mux)
	routesGitPush(mux)
	routesUpdate(mux)
	routesTrust(mux)
	routesTrashSessions(mux)
	routesCleanupWarn(mux)
	routesNotes(mux)
	routesKeepWarm(mux)
	routesBuiltinRules(mux)
	routesSkills(mux)
	routesBackup(mux)
	routesIDE(mux)
	routesSlash(mux)
	routesImages(mux)
	routesDocs(mux)
	routesPair(mux)
	routesQueue(mux)
	routesShare(mux)
	routesGuest(mux)
	routesTeams(mux)
	// Conversation dans le navigateur (chat.go).
	mux.HandleFunc("POST /v1/sessions/{id}/chat", chatHandler)
	mux.HandleFunc("DELETE /v1/sessions/{id}/chat", chatStopHandler)
	mux.HandleFunc("POST /v1/sessions", newSessionHandler)
	// Anti DNS-rebinding : seul le Host local est servi (sauf écoute hors localhost, où le jeton, ou la session en mode
	// multi-utilisateur, le remplace).
	// Anti CSRF : http.CrossOriginProtection (bibliothèque standard) refuse toute requête modifiante venue d'une autre origine.
	// Le choix de dossier de configuration (homes.go) vient après ces gardes : la passerelle juge la requête d'origine avant de la transmettre.
	// En mode multi-utilisateur, le garde des comptes s'insère entre ces gardes et le choix du dossier. Les routes /v1/auth/* et /v1/admin/* ne
	// sont montées qu'une fois la base trouvée, et suivent la base ouverte (rouverte si le fichier est remplacé).
	type authChain struct {
		api   *http.ServeMux
		gated http.Handler
	}
	var (
		mountOnce sync.Once
		chainMu   sync.Mutex
		chains    = map[*authStore]*authChain{}
	)
	chainFor := func(accountStore *authStore) *authChain {
		chainMu.Lock()
		defer chainMu.Unlock()
		chain := chains[accountStore]
		if chain == nil {
			chain = &authChain{api: http.NewServeMux()}
			routesAdmin(chain.api, routesAuth(chain.api, accountStore, newThrottle(accountStore.db)))
			chain.gated = http.NewCrossOriginProtection().Handler(authGate(mux, accountStore, withAuthWatch(withHomes(mux))))
			chains[accountStore] = chain
		}
		return chain
	}
	mountAuth := func() {
		for pattern := range routeRights {
			isAccountRoute := strings.Contains(pattern, " /v1/auth/") || strings.Contains(pattern, " /v1/admin/")
			if isAccountRoute {
				mux.HandleFunc(pattern, func(responseWriter http.ResponseWriter, request *http.Request) {
					// Sans compte, /v1/auth/* n'existe pas : l'interface n'affiche pas de connexion que personne ne peut passer.
					if _, decision := reqMode(request); decision.on {
						chainFor(decision.st).api.ServeHTTP(responseWriter, request)
						return
					}
					http.NotFound(responseWriter, request)
				})
			}
		}
	}
	guarded := http.NewCrossOriginProtection().Handler(withHomes(mux))
	var host http.Handler = http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		request, decision := reqMode(request)
		if decision.err != nil {
			gateDeny(responseWriter, http.StatusServiceUnavailable, "srv.auth.baseInutilisable")
			return
		}
		if decision.st != nil {
			mountOnce.Do(mountAuth)
		}
		if decision.on {
			warnXFF(request)
			chainFor(decision.st).gated.ServeHTTP(responseWriter, request)
			return
		}
		guarded.ServeHTTP(responseWriter, request)
	})
	if hostToken != "" {
		host = withTokenOpen(hostToken, host)
	}
	// Le jeton hôte passe après la garde invité : un invité n'en a pas, et un cookie invité invalide retombe sur lui.
	guest := guestGate(mux, host)
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if !openHosts && !hosts[request.Host] {
			forbid(responseWriter, request)
			return
		}
		if !childAuthorized(request) {
			forbid(responseWriter, request)
			return
		}
		guest.ServeHTTP(responseWriter, request)
	})
}

// hostToken : le jeton hôte, vérifié par handler après les gardes Host et invité ("" : aucun).
var hostToken string

var readHeaderTimeout = 10 * time.Second

// newServer : avec --listen, le serveur est joignable du réseau ; un client qui envoie ses en-têtes au compte-gouttes
// (slowloris) ne garde pas une connexion ouverte au-delà de ce délai, avant même la vérification du jeton. IdleTimeout
// ferme de même une connexion keep-alive qui ne redemande rien (sans effet sur une réponse en cours ni sur le WebSocket).
func newServer(addr string, rootHandler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           rootHandler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
}
