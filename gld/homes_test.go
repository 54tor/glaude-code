package main

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestHomesEnregistrement(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	pro := t.TempDir()
	if err := saveHomes([]Home{{"Pro", pro}}); err != nil {
		t.Fatal(err)
	}
	if got := loadHomes(); len(got) != 1 || got[0].Name != "Pro" || got[0].Dir != pro {
		t.Fatalf("%+v", got)
	}
	for name, hs := range map[string][]Home{
		"nom vide":            {{"", pro}},
		"nom en double":       {{"A", pro}, {"A", pro}},
		"séparateur":          {{"a/b", pro}},
		"chemin relatif":      {{"A", "rel"}},
		"dossier courant":     {{"A", cfg}},
		"dossier introuvable": {{"A", pro + "/absent"}},
	} {
		if saveHomes(hs) == nil {
			t.Errorf("%s accepté", name)
		}
	}
	if got := loadHomes(); len(got) != 1 {
		t.Fatalf("une liste refusée a modifié le fichier : %+v", got)
	}
}

// Le cookie choisit le processus qui répond ; sans cookie, ou pour /homes, c'est le processus principal.
func TestHomesAiguillage(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	pro := t.TempDir()
	if err := saveHomes([]Home{{"Pro perso", pro}}); err != nil {
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
	if get("/homes", enc) != "principal" {
		t.Fatal("/homes doit rester local")
	}
	if get("/data", url.QueryEscape("inconnu")) != "principal" {
		t.Fatal("un dossier inconnu retombe sur le principal")
	}
}

func TestEnfantExigeLeJetonDeLaPasserelle(t *testing.T) {
	req := func(tok string) *http.Request {
		r := httptest.NewRequest("GET", "/api", nil)
		if tok != "" {
			r.Header.Set(childHeader, tok)
		}
		return r
	}
	if !childAuthorized(req("")) { // processus principal : pas de jeton attendu
		t.Fatal("le processus principal doit tout accepter")
	}
	t.Setenv(childTokenEnv, "secret")
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
	post := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := post("/homes-save", `{"homes":[{"name":"Pro","dir":"`+pro+`"}]}`); !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatal(w.Body.String())
	}
	if w := post("/homes-select", `{"name":"absent"}`); !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Fatal("dossier inconnu choisi")
	}
	if w := post("/homes-select", `{"name":""}`); !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") && !strings.Contains(w.Header().Get("Set-Cookie"), "gld_home=;") {
		t.Fatalf("retour au dossier principal : %q", w.Header().Get("Set-Cookie"))
	}
	if _, err := os.Stat(homesFile()); err != nil {
		t.Fatal(err)
	}
}
