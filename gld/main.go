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

var (
	root      string
	port      = env("GLD_WEB_PORT", env("CC_WEB_PORT", "8799")) // CC_* : anciens noms, encore lus
	idRe      = regexp.MustCompile(`^[0-9a-f-]{36}$`)
	hosts     = map[string]bool{}
	openHosts bool // écoute hors localhost (listen.go) : le jeton, ou en mode multi-utilisateur la session, remplace la garde sur le Host
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	home, _ := os.UserHomeDir()
	root = filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "projects")
	if code, ok := dispatchCLI(os.Args); ok { // gld-matin, gld-trier, gld-rm, gld-nommer : ni serveur ni port
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
	flag.Parse()
	if *showVersion {
		fmt.Println("gld", version)
		return
	}
	hosts["127.0.0.1:"+port], hosts["localhost:"+port], hosts[net.JoinHostPort(*listen, port)] = true, true, true

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
	st, on, err := authState()
	if err != nil {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.authRefused", map[string]any{"A1": err.Error()}))
		os.Exit(1)
	}
	if on {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.authMode", nil))
	}
	if dir, derr := authDir(); st != nil && derr == nil && keyFromFile.Load() {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.keyFromFile", map[string]any{"A1": filepath.Join(dir, "auth.key")}))
	}
	if err := checkRunAsSeparation(); err != nil {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.runAsRefused", map[string]any{"A1": err.Error()}))
		os.Exit(1)
	}
	if runAsOn() {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.runAsOn", map[string]any{"A1": runAsHome()}))
	}
	if _, err := installBuiltinRules(); err != nil { // non bloquant : le serveur marche sans, seules les règles manquent
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.rulesFailed", map[string]any{"A1": err.Error()}))
	}
	tokenPrincipal = tokenForChildren(token, generated)
	hostToken = token // obligatoire hors localhost ; en local, exigé dès que GLD_WEB_TOKEN est fixé (machine partagée)
	h := handler()
	if !local {
		openHosts = true // hors localhost, le Host n'est plus limité : le jeton (mode ouvert) ou la session (comptes) protège du DNS-rebinding
	}
	srv := newServer(net.JoinHostPort(*listen, port), h)
	srv.RegisterOnShutdown(ideCloseAll) // sinon un flux /v1/fs/watch ouvert fait attendre Shutdown jusqu'à son délai
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	homeCtx = ctx
	defer stopHomes()
	done := make(chan struct{}) // Shutdown attend les requêtes en vol : main ne doit pas sortir avant lui
	go func() {
		defer close(done)
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, "\n"+tr(cliLang(), "cli.main.stopped", nil))
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	migrateData()
	go autosaveTabs(2 * time.Minute)
	go watchWaiting(20 * time.Second)
	go keepWarmLoop(30 * time.Second)
	if queueSt != nil {
		go queueLoop(ctx, queueSt)
	}
	go expireTrashLoop(24 * time.Hour)
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
		if on && generated { // le jeton généré ne vaut rien en mode multi-utilisateur : la connexion par compte le remplace
			shown = ""
		}
		for _, u := range urlsFor(scheme, *listen, port, shown) {
			fmt.Fprintln(os.Stderr, "  "+u)
		}
	}
	defer removeWorkDir()
	serve := srv.ListenAndServe
	if useTLS {
		serve = func() error { return srv.ListenAndServeTLS(*tlsCert, *tlsKey) }
	}
	if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
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
func purgeDue(st *authStore, now time.Time) {
	if !purgeAt.IsZero() && now.Sub(purgeAt) < eventsPurgeEvery {
		return
	}
	purgeAt = now
	if _, err := st.PurgeEvents(eventsKeep); err != nil {
		journal("auth: purge du journal d'authentification : %v", err)
	}
	th := newThrottle(st.db)
	th.now = func() time.Time { return now }
	if _, err := th.Purge(); err != nil {
		journal("auth: purge des compteurs d'essais : %v", err)
	}
	if _, err := st.PurgeSessions(); err != nil {
		journal("auth: purge des sessions expirées : %v", err)
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
	fi, err := os.Lstat(path)
	if err != nil {
		switch {
		case !errors.Is(err, fs.ErrNotExist):
			return false, err
		case authSeen:
			return false, errAuthGone
		}
		return false, nil
	}
	if authSt != nil && !os.SameFile(authFI, fi) {
		authSt.db.Close()
		authSt, authFI = nil, nil
		return false, errAuthReplaced
	}
	if authSt == nil {
		st, err := openAuthStore()
		if err != nil {
			return false, err
		}
		if authFI, err = os.Lstat(path); err != nil {
			st.db.Close()
			return false, err
		}
		authSt, purgeAt = st, time.Time{}
	}
	on, err := authSt.AccountsOn()
	if err == nil && !on {
		authSeen = false
	}
	return on, err
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
func reqMode(r *http.Request) (*http.Request, authDecision) {
	if d, ok := r.Context().Value(modeKey{}).(authDecision); ok {
		return r, d
	}
	st, on, err := authStateFn()
	d := authDecision{st, on, err}
	return r.WithContext(context.WithValue(r.Context(), modeKey{}, d)), d
}

var xffWarned sync.Once

// warnXFF : derrière un proxy non déclaré, tous les clients partagent son adresse (limitation des essais, adresses connues).
func warnXFF(r *http.Request) {
	if os.Getenv("GLD_TRUSTED_PROXIES") != "" || r.Header.Get("X-Forwarded-For") == "" {
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if a, err := netip.ParseAddr(host); err == nil && (a.IsLoopback() || a.IsPrivate()) {
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
	routesTrust(mux)
	routesTrashSessions(mux)
	routesCleanupWarn(mux)
	routesNotes(mux)
	routesKeepWarm(mux)
	routesSkills(mux)
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
	chainFor := func(st *authStore) *authChain {
		chainMu.Lock()
		defer chainMu.Unlock()
		c := chains[st]
		if c == nil {
			c = &authChain{api: http.NewServeMux()}
			routesAdmin(c.api, routesAuth(c.api, st, newThrottle(st.db)))
			c.gated = http.NewCrossOriginProtection().Handler(authGate(mux, st, withAuthWatch(withHomes(mux))))
			chains[st] = c
		}
		return c
	}
	mountAuth := func() {
		for pat := range routeRights {
			if strings.Contains(pat, " /v1/auth/") || strings.Contains(pat, " /v1/admin/") {
				mux.HandleFunc(pat, func(w http.ResponseWriter, r *http.Request) {
					// Sans compte, /v1/auth/* n'existe pas : l'interface n'affiche pas de connexion que personne ne peut passer.
					if _, d := reqMode(r); d.on {
						chainFor(d.st).api.ServeHTTP(w, r)
						return
					}
					http.NotFound(w, r)
				})
			}
		}
	}
	guarded := http.NewCrossOriginProtection().Handler(withHomes(mux))
	var host http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, d := reqMode(r)
		if d.err != nil {
			gateDeny(w, 503, "srv.auth.baseInutilisable")
			return
		}
		if d.st != nil {
			mountOnce.Do(mountAuth)
		}
		if d.on {
			warnXFF(r)
			chainFor(d.st).gated.ServeHTTP(w, r)
			return
		}
		guarded.ServeHTTP(w, r)
	})
	if hostToken != "" {
		host = withTokenOpen(hostToken, host)
	}
	// Le jeton hôte passe après la garde invité : un invité n'en a pas, et un cookie invité invalide retombe sur lui.
	guest := guestGate(mux, host)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !openHosts && !hosts[r.Host] {
			forbid(w, r)
			return
		}
		if !childAuthorized(r) {
			forbid(w, r)
			return
		}
		guest.ServeHTTP(w, r)
	})
}

// hostToken : le jeton hôte, vérifié par handler après les gardes Host et invité ("" : aucun).
var hostToken string

var readHeaderTimeout = 10 * time.Second

// newServer : avec --listen, le serveur est joignable du réseau ; un client qui envoie ses en-têtes au compte-gouttes
// (slowloris) ne garde pas une connexion ouverte au-delà de ce délai, avant même la vérification du jeton. IdleTimeout
// ferme de même une connexion keep-alive qui ne redemande rien (sans effet sur une réponse en cours ni sur le WebSocket).
func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: readHeaderTimeout, IdleTimeout: 2 * time.Minute}
}
