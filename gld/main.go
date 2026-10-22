package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
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
	listen := flag.String("listen", env("GLD_WEB_LISTEN", "127.0.0.1"), "adresse d'écoute (défaut 127.0.0.1 ; 0.0.0.0 pour tout le réseau, avec un jeton)")
	showVersion := flag.Bool("version", false, "affiche la version et quitte")
	flag.Parse()
	if *showVersion {
		fmt.Println("gld", version)
		return
	}
	hosts["127.0.0.1:"+port], hosts["localhost:"+port] = true, true

	h := handler()
	local := isLoopback(*listen)
	token := ""
	if !local {
		// Hors localhost : jeton obligatoire, et le Host n'est plus limité à l'adresse locale (le jeton remplace la garde).
		token = os.Getenv("GLD_WEB_TOKEN")
		if token == "" {
			token = newToken()
		} else if err := checkToken(token); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		openHosts = true
		h = withToken(token, h)
	}
	srv := &http.Server{Addr: net.JoinHostPort(*listen, port), Handler: h}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	homeCtx = ctx
	defer stopHomes()
	go func() {
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, "\narrêt.")
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	migrateData()
	go autosaveTabs(2 * time.Minute)
	go watchWaiting(20 * time.Second)
	go func() { _ = syncIndex() }() // index de recherche prêt avant la première requête
	if local {
		fmt.Fprintln(os.Stderr, "http://"+net.JoinHostPort(*listen, port))
	} else {
		fmt.Fprintln(os.Stderr, "⚠ gld écoute sur "+net.JoinHostPort(*listen, port)+" : accès protégé par un jeton, mais en HTTP clair.")
		fmt.Fprintln(os.Stderr, "  Préférez un tunnel (SSH, Tailscale) ou un proxy TLS ; ne l'exposez pas sur Internet.")
		for _, u := range urlsFor(*listen, port, token) {
			fmt.Fprintln(os.Stderr, "  "+u)
		}
	}
	defer removeWorkDir()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
	routesSkills(mux)
	routesSlash(mux)
	routesImages(mux)
	// Conversation dans le navigateur (chat.go).
	mux.HandleFunc("POST /chat", chatHandler)
	mux.HandleFunc("POST /chat-stop", chatStopHandler)
	mux.HandleFunc("POST /new-session", newSessionHandler)
	// Anti DNS-rebinding : seul le Host local est servi (sauf écoute hors localhost, où le jeton le remplace).
	// Anti CSRF : http.CrossOriginProtection (bibliothèque standard) refuse toute requête modifiante venue d'une autre origine.
	guarded := withHomes(http.NewCrossOriginProtection().Handler(mux))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !openHosts && !hosts[r.Host] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		guarded.ServeHTTP(w, r)
	})
}
