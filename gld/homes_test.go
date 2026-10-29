package main

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestHomesEnregistrement(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	pro := t.TempDir()
	if err := saveHomes([]Home{{Name: "Pro", Dir: pro}}); err != nil {
		t.Fatal(err)
	}
	if got := loadHomes(); len(got) != 1 || got[0].Name != "Pro" || got[0].Dir != pro {
		t.Fatalf("%+v", got)
	}
	for name, hs := range map[string][]Home{
		"nom vide":            {{Name: "", Dir: pro}},
		"nom en double":       {{Name: "A", Dir: pro}, {Name: "A", Dir: pro}},
		"séparateur":          {{Name: "a/b", Dir: pro}},
		"chemin relatif":      {{Name: "A", Dir: "rel"}},
		"dossier courant":     {{Name: "A", Dir: cfg}},
		"dossier introuvable": {{Name: "A", Dir: pro + "/absent"}},
	} {
		if saveHomes(hs) == nil {
			t.Errorf("%s accepté", name)
		}
	}
	if got := loadHomes(); len(got) != 1 {
		t.Fatalf("une liste refusée a modifié le fichier : %+v", got)
	}
}

// Le cookie choisit le processus qui répond ; sans cookie, ou pour /v1/homes, c'est le processus principal.
func TestHomesAiguillage(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	pro := t.TempDir()
	if err := saveHomes([]Home{{Name: "Pro perso", Dir: pro}}); err != nil {
		t.Fatal(err)
	}
	enfant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("enfant")) }))
	defer enfant.Close()
	u, _ := url.Parse(enfant.URL)
	starts := 0
	old := homeStart
	defer func() { homeStart = old; homesMu.Lock(); clear(homeProcs); homesMu.Unlock() }()
	homeStart = func(h Home) (*homeProc, error) {
		starts++
		return &homeProc{dir: h.Dir, proxy: httputil.NewSingleHostReverseProxy(u), done: make(chan struct{})}, nil
	}
	principal := withHomes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("principal")) }))
	get := func(path string, cookie string) string {
		r := httptest.NewRequest("GET", path, nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: homeCookie, Value: cookie})
		}
		w := httptest.NewRecorder()
		principal.ServeHTTP(w, r)
		return w.Body.String()
	}
	if get("/data", "") != "principal" {
		t.Fatal("sans cookie")
	}
	enc := url.QueryEscape("Pro perso")
	first, second := get("/data", enc), get("/data", enc)
	if first != "enfant" || second != "enfant" || starts != 1 {
		t.Fatalf("le processus enfant doit répondre et n'être lancé qu'une fois (%d)", starts)
	}
	ws := func(origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/v1/terminal/ws?t=x", nil)
		r.Host = "127.0.0.1:8799"
		r.Header.Set("Upgrade", "websocket")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.AddCookie(&http.Cookie{Name: homeCookie, Value: enc})
		w := httptest.NewRecorder()
		principal.ServeHTTP(w, r)
		return w
	}
	if w := ws("http://evil.example"); w.Code != 403 {
		t.Fatalf("WebSocket d'une autre origine relayé : %d", w.Code)
	}
	if w := ws(""); w.Code != 403 {
		t.Fatalf("WebSocket sans Origin relayé : %d", w.Code)
	}
	if w := ws("http://127.0.0.1:8799"); w.Body.String() != "enfant" {
		t.Fatalf("WebSocket de la page refusé : %d %s", w.Code, w.Body.String())
	}
	if get("/v1/homes", enc) != "principal" {
		t.Fatal("/v1/homes doit rester local")
	}
	if get("/data", url.QueryEscape("inconnu")) != "principal" {
		t.Fatal("un dossier inconnu retombe sur le principal")
	}
}

func TestEnfantExigeLeJetonDeLaPasserelle(t *testing.T) {
	req := func(tok string) *http.Request {
		r := httptest.NewRequest("GET", "/v1/state", nil)
		if tok != "" {
			r.Header.Set(childHeader, tok)
		}
		return r
	}
	if !childAuthorized(req("")) { // processus principal : pas de jeton attendu
		t.Fatal("le processus principal doit tout accepter")
	}
	childToken = "secret"
	t.Cleanup(func() { childToken = "" })
	if childAuthorized(req("")) || childAuthorized(req("autre")) || !childAuthorized(req("secret")) {
		t.Fatal("un enfant n'accepte que le jeton de sa passerelle")
	}
}

func TestHomesRoutes(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	pro := t.TempDir()
	mux := http.NewServeMux()
	routesHomes(mux)
	put := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", path, strings.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := put("/v1/homes", `{"homes":[{"name":"Pro","dir":"`+pro+`"}]}`); w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	// Mode ouvert : la liste complète renvoyée sans identifiant garde celui du home du même nom.
	id := mustHomeID(t, "Pro")
	if w := put("/v1/homes", `{"homes":[{"name":"Pro","dir":"`+pro+`"}]}`); w.Code != 204 || mustHomeID(t, "Pro") != id {
		t.Fatal(w.Body.String())
	}
	if w := put("/v1/homes/current", `{"name":"absent"}`); w.Code != 404 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatal("dossier inconnu choisi")
	}
	if w := put("/v1/homes/current", `{"name":""}`); !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") && !strings.Contains(w.Header().Get("Set-Cookie"), "gld_home=;") {
		t.Fatalf("retour au dossier principal : %q", w.Header().Get("Set-Cookie"))
	}
	if _, err := os.Stat(homesFile()); err != nil {
		t.Fatal(err)
	}
}

// Le démarrage d'un enfant ne tient pas homesMu, et deux appels simultanés pour le même nom ne lancent qu'un processus.
func TestHomesDemarrageHorsVerrou(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	a, b := t.TempDir(), t.TempDir()
	if err := saveHomes([]Home{{Name: "A", Dir: a}, {Name: "B", Dir: b}}); err != nil {
		t.Fatal(err)
	}
	enfant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer enfant.Close()
	u, _ := url.Parse(enfant.URL)
	gate := make(chan struct{})
	entered := make(chan string, 8)
	var mu sync.Mutex
	starts := map[string]int{}
	old := homeStart
	defer func() { homeStart = old; homesMu.Lock(); clear(homeProcs); homesMu.Unlock() }()
	homeStart = func(h Home) (*homeProc, error) {
		mu.Lock()
		starts[h.Dir]++
		mu.Unlock()
		if h.Dir == a {
			entered <- "A"
			<-gate
		}
		return &homeProc{dir: h.Dir, proxy: httputil.NewSingleHostReverseProxy(u), done: make(chan struct{})}, nil
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := homeProxy("A"); err != nil {
				t.Error(err)
			}
		}()
	}
	<-entered // A démarre, bloqué dans homeStart
	done := make(chan struct{})
	go func() { // ni un autre dossier ni la liste ne doivent attendre A
		defer close(done)
		if _, err := homeProxy("B"); err != nil {
			t.Error(err)
		}
		w := httptest.NewRecorder()
		mux := http.NewServeMux()
		routesHomes(mux)
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/v1/homes", nil))
		if w.Code != 200 {
			t.Errorf("/v1/homes : %d", w.Code)
		}
	}()
	<-done
	close(gate)
	wg.Wait()
	if starts[a] != 1 || starts[b] != 1 {
		t.Fatalf("un seul démarrage par dossier attendu : %v", starts)
	}
}

// Un dossier retiré pendant son démarrage n'est pas enregistré.
func TestHomesRetireePendantDemarrage(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	a := t.TempDir()
	if err := saveHomes([]Home{{Name: "A", Dir: a}}); err != nil {
		t.Fatal(err)
	}
	old := homeStart
	defer func() { homeStart = old; homesMu.Lock(); clear(homeProcs); homesMu.Unlock() }()
	homeStart = func(h Home) (*homeProc, error) {
		if err := saveHomes([]Home{}); err != nil {
			t.Error(err)
		}
		return &homeProc{dir: h.Dir, proxy: &httputil.ReverseProxy{}, done: make(chan struct{})}, nil
	}
	if _, err := homeProxy("A"); err == nil {
		t.Fatal("le dossier retiré ne doit pas être servi")
	}
	homesMu.Lock()
	n := len(homeProcs) + len(homeStartups)
	homesMu.Unlock()
	if n != 0 {
		t.Fatalf("processus ou démarrage resté enregistré : %d", n)
	}
}

func TestEnvironNoAuth(t *testing.T) {
	t.Setenv("GLD_AUTH_KEY", "secret")
	t.Setenv("GLD_AUTH_DIR", "/x")
	for _, env := range [][]string{environNoAuth(), childEnv()} {
		dir := false
		for _, e := range env {
			if strings.HasPrefix(e, "GLD_AUTH_KEY=") {
				t.Error("clé maître transmise")
			}
			dir = dir || e == "GLD_AUTH_DIR=/x"
		}
		if !dir {
			t.Error("GLD_AUTH_DIR perdu : le garde Bash en a besoin")
		}
	}
}
