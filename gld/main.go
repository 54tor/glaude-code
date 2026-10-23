package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
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
	openHosts bool // écoute hors localhost (listen.go) : le jeton remplace la garde sur le Host
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
	showVersion := flag.Bool("version", false, tr(cliLang(), "cli.main.flagVersion", nil))
	flag.Parse()
	if *showVersion {
		fmt.Println("gld", version)
		return
	}
	hosts["127.0.0.1:"+port], hosts["localhost:"+port], hosts[net.JoinHostPort(*listen, port)] = true, true, true

	local := isLoopback(*listen)
	token, generated, err := takeToken(local)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Une base illisible ou une clé qui ne la déchiffre pas arrête le serveur : repartir en mode ouvert contournerait les comptes.
	if authSt, err = startAuth(); err != nil {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.authRefused", map[string]any{"A1": err.Error()}))
		os.Exit(1)
	}
	if authMode(authSt) {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.authMode", nil))
	}
	tokenPrincipal = tokenForChildren(token)
	h := handler()
	if !local {
		openHosts = true // hors localhost, le Host n'est plus limité à l'adresse locale : le jeton remplace la garde
	}
	if token != "" { // obligatoire hors localhost ; en local, exigé dès que GLD_WEB_TOKEN est fixé (machine partagée)
		h = withTokenOpen(token, h)
	}
	srv := newServer(net.JoinHostPort(*listen, port), h)
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
	go expireTrashLoop(24 * time.Hour)
	go func() { _ = syncIndex() }() // index de recherche prêt avant la première requête
	if local && token == "" {
		fmt.Fprintln(os.Stderr, "http://"+net.JoinHostPort(*listen, port))
	} else if local {
		fmt.Fprintln(os.Stderr, "http://"+net.JoinHostPort(*listen, port)+"/?token="+tokenShown(token, generated))
	} else {
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.plainHTTP", map[string]any{"A1": net.JoinHostPort(*listen, port)}))
		fmt.Fprintln(os.Stderr, tr(cliLang(), "cli.main.preferTunnel", nil))
		for _, u := range urlsFor(*listen, port, tokenShown(token, generated)) {
			fmt.Fprintln(os.Stderr, "  "+u)
		}
	}
	defer removeWorkDir()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	<-done
}

// authSt : base des comptes de la passerelle ; nil dans un home enfant (et dans les tests qui ne l'ouvrent pas).
var authSt *authStore

// startAuth ouvre la base des comptes ; jamais dans un enfant, qui ne sert que sa passerelle.
func startAuth() (*authStore, error) {
	if childToken != "" {
		return nil, nil
	}
	return openAuthStore()
}

// authMode : jugé à chaque requête, pour qu'un premier compte créé par la ligne de commande s'applique sans redémarrage.
func authMode(s *authStore) bool { return s != nil && childToken == "" && s.HasUsers() }

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
	routesSlash(mux)
	routesImages(mux)
	// Conversation dans le navigateur (chat.go).
	mux.HandleFunc("POST /chat", chatHandler)
	mux.HandleFunc("POST /chat-stop", chatStopHandler)
	mux.HandleFunc("POST /new-session", newSessionHandler)
	// Anti DNS-rebinding : seul le Host local est servi (sauf écoute hors localhost, où le jeton le remplace).
	// Anti CSRF : http.CrossOriginProtection (bibliothèque standard) refuse toute requête modifiante venue d'une autre origine.
	// Le choix de dossier de configuration (homes.go) vient après ces gardes : la passerelle juge la requête d'origine avant de la transmettre.
	// En mode multi-utilisateur, le garde des comptes s'insère entre ces gardes et le choix du dossier.
	st := authSt
	var gated http.Handler
	if st != nil {
		routesAuth(mux, st, newThrottle(st.db))
		gated = http.NewCrossOriginProtection().Handler(authGate(mux, st, withAuthWatch(withHomes(mux))))
	}
	guarded := http.NewCrossOriginProtection().Handler(withHomes(mux))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !openHosts && !hosts[r.Host] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !childAuthorized(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if gated != nil && authMode(st) {
			warnXFF(r)
			gated.ServeHTTP(w, r)
			return
		}
		guarded.ServeHTTP(w, r)
	})
}

var readHeaderTimeout = 10 * time.Second

// newServer : avec --listen, le serveur est joignable du réseau ; un client qui envoie ses en-têtes au compte-gouttes
// (slowloris) ne garde pas une connexion ouverte au-delà de ce délai, avant même la vérification du jeton. IdleTimeout
// ferme de même une connexion keep-alive qui ne redemande rien (sans effet sur une réponse en cours ni sur le WebSocket).
func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: readHeaderTimeout, IdleTimeout: 2 * time.Minute}
}
