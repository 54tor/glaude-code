package main

// Cas d'échec repérés par l'audit du 2026-10-23 : chaque test fige la correction d'une perte de données silencieuse.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Un corps illisible n'est pas une demande de tout vider.
func TestCorbeillePurgeCorpsInvalide(t *testing.T) {
	h, cfg, _ := web(t)
	body := `{"items":[{"dir":"-p","id":"` + id1 + `"}]}`
	ok(t, h, "/session-trash-add", body, true, "")
	status(t, h, "POST", "/session-trash-purge", `{`, 400)
	_, m, _ := call(t, h, "GET", "/session-trash", "")
	if l, _ := m["sessions"].([]any); len(l) != 1 {
		t.Fatalf("corbeille vidée par un corps invalide : %v", m)
	}
	if _, err := os.Stat(filepath.Join(cfg, "gld-web-trash", "sessions", id1, id1+".jsonl")); err != nil {
		t.Fatal("transcript parti de la corbeille")
	}
}

// Restauration : si le dossier des sous-agents existe déjà, ils restent dans la corbeille au lieu d'être effacés.
func TestCorbeilleRestaurationSousAgentsPreserves(t *testing.T) {
	h, cfg, _ := web(t)
	sub := filepath.Join(cfg, "projects", "-p", id1)
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "agent.jsonl"), []byte("{}\n"), 0o600)
	ok(t, h, "/session-trash-add", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, true, "")
	os.MkdirAll(filepath.Join(sub, "autre"), 0o755) // un dossier du même nom réapparaît
	ok(t, h, "/session-trash-restore", `{"ids":["`+id1+`"]}`, false, "")
	if _, err := os.Stat(filepath.Join(cfg, "gld-web-trash", "sessions", id1, "sub", "agent.jsonl")); err != nil {
		t.Fatal("les sous-agents ont été effacés")
	}
}

// Une fiche sans transcript (déplacement interrompu) se purge au lieu de rester listée pour toujours.
func TestCorbeilleFicheFantome(t *testing.T) {
	h, cfg, _ := web(t)
	ok(t, h, "/session-trash-add", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, true, "")
	os.Remove(filepath.Join(cfg, "gld-web-trash", "sessions", id1, id1+".jsonl"))
	ok(t, h, "/session-trash-purge", `{"ids":["`+id1+`"]}`, true, "")
	if _, err := os.Stat(filepath.Join(cfg, "gld-web-trash", "sessions", id1)); err == nil {
		t.Fatal("fiche fantôme toujours là")
	}
}

// Un fichier de sauvegardes illisible n'est jamais réécrit (ses sauvegardes manuelles seraient perdues).
func TestOngletsFichierIllisible(t *testing.T) {
	_, cfg, _ := web(t)
	os.WriteFile(filepath.Join(cfg, "gld-web-tabs.json"), []byte("{pas du json"), 0o600)
	if _, err := readTabs([]byte("{pas du json")); err == nil {
		t.Fatal("readTabs accepte du JSON invalide")
	}
	if _, _, err := saveTabs("", true); err == nil {
		t.Fatal("saveTabs a écrit par-dessus un fichier illisible")
	}
	if b, _ := os.ReadFile(filepath.Join(cfg, "gld-web-tabs.json")); string(b) != "{pas du json" {
		t.Fatal("fichier réécrit")
	}
}

// Même garde pour les notes.
func TestNotesFichierIllisible(t *testing.T) {
	h, cfg, _ := web(t)
	os.WriteFile(filepath.Join(cfg, "gld-web-notes.json"), []byte("["), 0o600)
	status(t, h, "POST", "/notes", `{"key":"d//tmp/x","text":"bonjour"}`, 500)
	if b, _ := os.ReadFile(filepath.Join(cfg, "gld-web-notes.json")); string(b) != "[" {
		t.Fatal("fichier réécrit")
	}
}

// La redirection après ?token= reste locale, même pour un chemin « //hôte ».
func TestJetonRedirectionLocale(t *testing.T) {
	h := withToken("abcdefghijklmnop", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "http://127.0.0.1:8799//evil.example/?token=abcdefghijklmnop", nil))
	if loc := rec.Header().Get("Location"); rec.Code != 303 || loc != "/evil.example/" {
		t.Fatalf("code %d, Location %q", rec.Code, loc)
	}
}

// Les verbes d'attente sont relus seulement quand settings.json change.
func TestVerbesEnCache(t *testing.T) {
	_, cfg, _ := web(t)
	settings := filepath.Join(cfg, "settings.json")
	os.WriteFile(settings, []byte(`{"spinnerVerbs":{"mode":"replace","verbs":["un"]}}`), 0o644)
	if got := spinnerVerbs("fr"); len(got) != 1 || got[0] != "un" {
		t.Fatalf("verbes : %v", got)
	}
	os.WriteFile(settings, []byte(`{"spinnerVerbs":{"mode":"replace","verbs":["deux","trois"]}}`), 0o644)
	if got := spinnerVerbs("fr"); len(got) != 2 {
		t.Fatalf("cache non invalidé : %v", got)
	}
}

// Une lecture de la corbeille ne purge rien ; l'expiration passe par un POST (ou la boucle quotidienne).
func TestCorbeilleExpirationHorsGET(t *testing.T) {
	h, cfg, _ := web(t)
	ok(t, h, "/session-trash-add", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, true, "")
	dir := filepath.Join(cfg, "gld-web-trash", "sessions", id1)
	b, _ := os.ReadFile(filepath.Join(dir, "meta.json"))
	var m TrashedSession
	json.Unmarshal(b, &m)
	m.At = float64(time.Now().Add(-31 * 24 * time.Hour).Unix())
	b, _ = json.Marshal(m)
	os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o600)
	_, got, _ := call(t, h, "GET", "/session-trash", "")
	if l, _ := got["sessions"].([]any); len(l) != 0 {
		t.Fatalf("session expirée encore listée : %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, id1+".jsonl")); err != nil {
		t.Fatal("GET /session-trash a purgé")
	}
	// Une purge d'un autre id expire au passage la session trop vieille.
	ok(t, h, "/session-trash-purge", `{"ids":["`+id2+`"]}`, true, "")
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("session expirée toujours dans la corbeille")
	}
	if _, err := os.Stat(filepath.Join(cfg, "projects", "-p", id1+".jsonl")); err == nil {
		t.Fatal("transcript expiré remis en place au lieu d'être purgé")
	}
}

// Forcer le relevé des limites passe par un POST.
func TestCompteRafraichirEnPOST(t *testing.T) {
	h, _, _ := web(t)
	old := usageStart
	calls := make(chan struct{}, 2)
	usageStart = func() error { calls <- struct{}{}; return nil }
	reset := func() {
		usageMu.Lock()
		usageLast, usageBusy = time.Time{}, false
		usageMu.Unlock()
	}
	defer func() { usageWG.Wait(); usageStart = old; reset() }()
	reset()
	if _, m, _ := call(t, h, "POST", "/account-refresh", "{}"); m["refreshing"] != true {
		t.Fatalf("POST /account-refresh : %v", m)
	}
	<-calls
	usageWG.Wait()
	status(t, h, "GET", "/account-refresh", "", 405)
}

// Deux mises à la corbeille simultanées, sur deux sessions distinctes, aboutissent toutes deux (verrou sessionsMu).
func TestCorbeilleAjoutsConcurrents(t *testing.T) {
	h, cfg, proj := web(t)
	put(t, filepath.Join(cfg, "projects", "-p", id2+".jsonl"), `{"type":"user","cwd":"`+proj+`","message":{"content":"x"}}`+"\n", old)
	var wg sync.WaitGroup
	for _, id := range []string{id1, id2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/session-trash-add", strings.NewReader(`{"items":[{"dir":"-p","id":"`+id+`"}]}`))
			r.Host = testHost
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if !strings.Contains(w.Body.String(), `"ok":true`) {
				t.Errorf("%s : %s", id, w.Body.String())
			}
		}()
	}
	wg.Wait()
	for _, id := range []string{id1, id2} {
		if _, err := os.Stat(filepath.Join(cfg, "gld-web-trash", "sessions", id, id+".jsonl")); err != nil {
			t.Errorf("%s absente de la corbeille", id)
		}
	}
}

// Un jeton fourni par l'utilisateur n'est jamais imprimé ; seul un jeton généré l'est (il faut bien le connaître).
func TestJetonFourniJamaisImprime(t *testing.T) {
	t.Setenv("GLD_WEB_TOKEN", "0123456789abcdef0123")
	tok, gen, _ := takeToken(false)
	if s := tokenShown(tok, gen); s != "$GLD_WEB_TOKEN" {
		t.Fatalf("jeton fourni imprimé : %q", s)
	}
	for _, u := range urlsFor("127.0.0.1", "8799", tokenShown(tok, gen)) {
		if strings.Contains(u, tok) {
			t.Fatalf("jeton fourni dans l'adresse : %q", u)
		}
	}
	tok, gen, _ = takeToken(false)
	if s := tokenShown(tok, gen); s != tok || len(s) != 48 {
		t.Fatalf("jeton généré : %q", s)
	}
}

// /conv sans limit ne charge plus tout le transcript : pages de 1000 messages, textes entiers, more et start pour la suite.
func TestConvSansLimitePagine(t *testing.T) {
	h, cfg, proj := web(t)
	var b strings.Builder
	long := strings.Repeat("é", msgCap) // plus de msgCap octets : coupé avec limit, entier sans
	for i := range 1200 {
		txt := "m" + strconv.Itoa(i)
		if i == 1199 {
			txt = long
		}
		b.WriteString(`{"type":"user","cwd":"` + proj + `","message":{"content":"` + txt + `"}}` + "\n")
	}
	put(t, filepath.Join(cfg, "projects", "-p", id2+".jsonl"), b.String(), old)
	m := status(t, h, "GET", "/conv?dir=-p&id="+id2, "", 200)
	msgs, _ := m["msgs"].([]any)
	if len(msgs) != 1000 || m["more"] != true {
		t.Fatalf("première page : %d messages, more %v", len(msgs), m["more"])
	}
	if last, _ := msgs[999].(map[string]any); last["t"] != long {
		t.Fatal("texte coupé sans limit : l'export perdrait la fin")
	}
	start := int64(m["start"].(float64))
	m = status(t, h, "GET", "/conv?dir=-p&id="+id2+"&before="+strconv.FormatInt(start, 10), "", 200)
	if msgs, _ := m["msgs"].([]any); len(msgs) != 200 || m["more"] != false {
		t.Fatalf("seconde page : %d messages, more %v", len(msgs), m["more"])
	}
}
