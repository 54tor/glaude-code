package main

// Plusieurs dossiers de configuration de Claude Code (CLAUDE_CONFIG_DIR : un compte pro, un compte perso…).
// gld lit un seul dossier par processus : le processus principal est la passerelle. Chaque autre dossier est servi par un gld enfant
// (même binaire, CLAUDE_CONFIG_DIR et port propres, écoute locale) et le cookie gld_home désigne celui qui répond. Tout le
// reste (sessions, mémoire, réglages, terminal) fonctionne donc tel quel pour chaque dossier, sans état partagé.

import (
	"context"
	"encoding/json"
	"errors"
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
}

type homeProc struct {
	dir   string
	cmd   *exec.Cmd
	port  string
	proxy *httputil.ReverseProxy
	done  chan struct{} // fermé quand le processus a fini
}

var (
	homesMu    sync.Mutex
	homeProcs  = map[string]*homeProc{}
	homeCtx    = context.Background() // remplacé par le contexte d'arrêt du serveur
	homeNameRe = regexp.MustCompile(`^[^\x00-\x1f/\\]+$`)
	homeStart  = startHomeProc // remplaçable dans les tests
)

func homesFile() string { return filepath.Join(cfgDir(), "gld-web-homes.json") }

func loadHomes() []Home {
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

// saveHomes valide la liste puis remplace le fichier (écriture atomique).
func saveHomes(hs []Home) error {
	seen := map[string]bool{}
	for i := range hs {
		h := &hs[i]
		h.Name = strings.TrimSpace(spaceRe.ReplaceAllString(h.Name, " "))
		if h.Name == "" || utf8.RuneCountInString(h.Name) > 40 || !homeNameRe.MatchString(h.Name) || seen[h.Name] {
			return errors.New("nom de dossier de configuration vide, trop long, avec un caractère interdit ou en double")
		}
		seen[h.Name] = true
		h.Dir = filepath.Clean(h.Dir)
		if !filepath.IsAbs(h.Dir) || h.Dir == cfgDir() {
			return errors.New("« " + h.Name + " » : un chemin absolu, différent du dossier de configuration courant, est attendu")
		}
		if fi, err := os.Stat(h.Dir); err != nil || !fi.IsDir() {
			return errors.New("« " + h.Name + " » : dossier introuvable")
		}
	}
	if len(hs) > 20 {
		return errors.New("trop de dossiers de configuration (20 au plus)")
	}
	b, _ := json.MarshalIndent(map[string]any{"homes": hs}, "", "  ")
	tmp := homesFile() + ".tmp"
	if err := writeSynced(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, homesFile())
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
	p.proxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target) // le Host devient celui de l'enfant, que sa garde anti DNS-rebinding accepte
			// La passerelle a déjà jugé l'origine de la requête (anti CSRF) : l'enfant ne reçoit ni Origin ni Sec-Fetch-*, qu'il
			// ne pourrait comparer qu'à sa propre adresse, ni le cookie (jeton, choix de dossier). Il exige le jeton de la passerelle.
			for k := range r.Out.Header {
				if strings.HasPrefix(strings.ToLower(k), "sec-fetch-") {
					r.Out.Header.Del(k)
				}
			}
			r.Out.Header.Del("Origin")
			r.Out.Header.Del("Cookie")
			r.Out.Header.Set(childHeader, token)
		},
	}
	for i := 0; i < 100; i++ {
		select {
		case <-p.done:
			return nil, errors.New("le processus enfant s'est arrêté")
		default:
		}
		if c, err := net.DialTimeout("tcp", target.Host, 200*time.Millisecond); err == nil {
			c.Close()
			return p, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	cmd.Process.Kill()
	return nil, errors.New("le processus enfant ne répond pas")
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

// homeProxy : le proxy de l'enfant de ce dossier, lancé au besoin.
func homeProxy(name string) (http.Handler, error) {
	h, ok := homeByName(name)
	if !ok {
		return nil, errors.New("dossier de configuration inconnu")
	}
	homesMu.Lock()
	defer homesMu.Unlock()
	if p := homeProcs[name]; p != nil {
		select {
		case <-p.done:
			delete(homeProcs, name)
		default:
			return p.proxy, nil
		}
	}
	p, err := homeStart(h)
	if err != nil {
		return nil, err
	}
	homeProcs[name] = p
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
		if name := homeFrom(r); name != "" && !strings.HasPrefix(r.URL.Path, "/homes") {
			p, err := homeProxy(name)
			if err != nil { // dossier retiré ou enfant en panne : retour au dossier courant
				http.SetCookie(w, &http.Cookie{Name: homeCookie, Path: "/", MaxAge: -1})
				journal("homes %s : %v", name, err)
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
		rows := []row{}
		for _, h := range loadHomes() {
			rows = append(rows, row{h, running[h.Name]})
		}
		writeJSON(w, 200, map[string]any{"current": cur, "main": cfgDir(), "homes": rows})
	})
	mux.HandleFunc("POST /homes-save", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Homes []Home }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req) != nil || req.Homes == nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
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
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Dossiers de configuration enregistrés"})
	})
	mux.HandleFunc("POST /homes-sync-plan", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ From, To string }
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
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
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
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
			writeJSON(w, 400, map[string]any{"ok": false, "out": "requête invalide"})
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
		writeJSON(w, 200, map[string]any{"ok": true, "out": "Dossier de configuration choisi"})
	})
}
