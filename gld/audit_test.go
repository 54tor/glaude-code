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
	handler, cfg, _ := web(t)
	body := `{"items":[{"dir":"-p","id":"` + id1 + `"}]}`
	v1ok(t, handler, "POST", "/v1/trash/sessions", body, "")
	status(t, handler, "DELETE", "/v1/trash/sessions", "", 400)
	_, lookup, _ := call(t, handler, "GET", "/v1/trash/sessions", "")
	if values, _ := lookup["sessions"].([]any); len(values) != 1 {
		t.Fatalf("corbeille vidée par un corps invalide : %v", lookup)
	}
	if _, err := os.Stat(filepath.Join(cfg, "gld-web-trash", "sessions", id1, id1+".jsonl")); err != nil {
		t.Fatal("transcript parti de la corbeille")
	}
}

// Restauration : si le dossier des sous-agents existe déjà, ils restent dans la corbeille au lieu d'être effacés.
func TestCorbeilleRestaurationSousAgentsPreserves(t *testing.T) {
	handler, cfg, _ := web(t)
	sub := filepath.Join(cfg, "projects", "-p", id1)
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "agent.jsonl"), []byte("{}\n"), 0o600)
	v1ok(t, handler, "POST", "/v1/trash/sessions", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, "")
	os.MkdirAll(filepath.Join(sub, "autre"), 0o755) // un dossier du même nom réapparaît
	v1err(t, handler, "PATCH", "/v1/trash/sessions?item="+id1, `{"trashed":false}`, 409, "")
	if _, err := os.Stat(filepath.Join(cfg, "gld-web-trash", "sessions", id1, "sub", "agent.jsonl")); err != nil {
		t.Fatal("les sous-agents ont été effacés")
	}
}

// Une fiche sans transcript (déplacement interrompu) se purge au lieu de rester listée pour toujours.
func TestCorbeilleFicheFantome(t *testing.T) {
	handler, cfg, _ := web(t)
	v1ok(t, handler, "POST", "/v1/trash/sessions", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, "")
	os.Remove(filepath.Join(cfg, "gld-web-trash", "sessions", id1, id1+".jsonl"))
	v1ok(t, handler, "DELETE", "/v1/trash/sessions?item="+id1, "", "")
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
	if data, _ := os.ReadFile(filepath.Join(cfg, "gld-web-tabs.json")); string(data) != "{pas du json" {
		t.Fatal("fichier réécrit")
	}
}

// Même garde pour les notes.
func TestNotesFichierIllisible(t *testing.T) {
	handler, cfg, _ := web(t)
	os.WriteFile(filepath.Join(cfg, "gld-web-notes.json"), []byte("["), 0o600)
	status(t, handler, "PUT", "/v1/notes", `{"key":"d//tmp/x","text":"bonjour"}`, 500)
	if data, _ := os.ReadFile(filepath.Join(cfg, "gld-web-notes.json")); string(data) != "[" {
		t.Fatal("fichier réécrit")
	}
}

// La redirection après ?token= reste locale, même pour un chemin « //hôte ».
func TestJetonRedirectionLocale(t *testing.T) {
	handler := withToken("abcdefghijklmnop", http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "http://127.0.0.1:8799//evil.example/?token=abcdefghijklmnop", nil))
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
	handler, cfg, _ := web(t)
	v1ok(t, handler, "POST", "/v1/trash/sessions", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, "")
	dir := filepath.Join(cfg, "gld-web-trash", "sessions", id1)
	data, _ := os.ReadFile(filepath.Join(dir, "meta.json"))
	var trashedSession TrashedSession
	json.Unmarshal(data, &trashedSession)
	trashedSession.At = float64(time.Now().Add(-31 * 24 * time.Hour).Unix())
	data, _ = json.Marshal(trashedSession)
	os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o600)
	_, got, _ := call(t, handler, "GET", "/v1/trash/sessions", "")
	if values, _ := got["sessions"].([]any); len(values) != 0 {
		t.Fatalf("session expirée encore listée : %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, id1+".jsonl")); err != nil {
		t.Fatal("GET /v1/trash/sessions a purgé")
	}
	// Une purge d'un autre id expire au passage la session trop vieille.
	v1ok(t, handler, "DELETE", "/v1/trash/sessions?item="+id2, "", "")
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("session expirée toujours dans la corbeille")
	}
	if _, err := os.Stat(filepath.Join(cfg, "projects", "-p", id1+".jsonl")); err == nil {
		t.Fatal("transcript expiré remis en place au lieu d'être purgé")
	}
}

// Forcer le relevé des limites passe par un POST.
func TestCompteRafraichirEnPOST(t *testing.T) {
	handler, _, _ := web(t)
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
	if _, lookup, _ := call(t, handler, "POST", "/v1/account/refreshes", "{}"); lookup["refreshing"] != true {
		t.Fatalf("POST /v1/account/refreshes : %v", lookup)
	}
	<-calls
	usageWG.Wait()
	status(t, handler, "GET", "/v1/account/refreshes", "", 405)
	if _, lookup, _ := call(t, handler, "GET", "/v1/account?refresh=1", ""); lookup["refreshing"] == true {
		t.Fatalf("GET /v1/account?refresh=1 force encore le relevé : %v", lookup)
	}
}

// Deux mises à la corbeille simultanées, sur deux sessions distinctes, aboutissent toutes deux (verrou sessionsMu).
func TestCorbeilleAjoutsConcurrents(t *testing.T) {
	handler, cfg, proj := web(t)
	put(t, filepath.Join(cfg, "projects", "-p", id2+".jsonl"), `{"type":"user","cwd":"`+proj+`","message":{"content":"x"}}`+"\n", old)
	var wg sync.WaitGroup
	for _, id := range []string{id1, id2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request := httptest.NewRequest("POST", "/v1/trash/sessions", strings.NewReader(`{"items":[{"dir":"-p","id":"`+id+`"}]}`))
			request.Host = testHost
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != 201 {
				t.Errorf("%s : %s", id, recorder.Body.String())
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
	if text := tokenShown(tok, gen); text != "$GLD_WEB_TOKEN" {
		t.Fatalf("jeton fourni imprimé : %q", text)
	}
	for _, rawURL := range urlsFor("http", "127.0.0.1", "8799", tokenShown(tok, gen)) {
		if strings.Contains(rawURL, tok) {
			t.Fatalf("jeton fourni dans l'adresse : %q", rawURL)
		}
	}
	tok, gen, _ = takeToken(false)
	if text := tokenShown(tok, gen); text != tok || len(text) != 48 {
		t.Fatalf("jeton généré : %q", text)
	}
}

// /conv sans limit ne charge plus tout le transcript : pages de 1000 messages, textes entiers, more et start pour la suite.
func TestConvSansLimitePagine(t *testing.T) {
	handler, cfg, proj := web(t)
	var builder strings.Builder
	long := strings.Repeat("é", msgCap) // plus de msgCap octets : coupé avec limit, entier sans
	for index := range 1200 {
		txt := "m" + strconv.Itoa(index)
		if index == 1199 {
			txt = long
		}
		builder.WriteString(`{"type":"user","cwd":"` + proj + `","message":{"content":"` + txt + `"}}` + "\n")
	}
	put(t, filepath.Join(cfg, "projects", "-p", id2+".jsonl"), builder.String(), old)
	lookup := status(t, handler, "GET", "/v1/sessions/"+id2+"/conversation?dir=-p", "", 200)
	msgs, _ := lookup["msgs"].([]any)
	if len(msgs) != 1000 || lookup["more"] != true {
		t.Fatalf("première page : %d messages, more %v", len(msgs), lookup["more"])
	}
	if last, _ := msgs[999].(map[string]any); last["t"] != long {
		t.Fatal("texte coupé sans limit : l'export perdrait la fin")
	}
	start := int64(lookup["start"].(float64))
	lookup = status(t, handler, "GET", "/v1/sessions/"+id2+"/conversation?dir=-p&before="+strconv.FormatInt(start, 10), "", 200)
	if msgs, _ := lookup["msgs"].([]any); len(msgs) != 200 || lookup["more"] != false {
		t.Fatalf("seconde page : %d messages, more %v", len(msgs), lookup["more"])
	}
}

// Réassignation : si la réécriture d'un sous-agent échoue, rien n'a été remplacé (ni le transcript principal, ni de .tmp
// laissé derrière) et la session reste où elle était.
func TestReassignationAtomique(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(dst, 0o755)
	enc := nonAlnum.ReplaceAllString(src, "-")
	jl := filepath.Join(cfg, "projects", enc, id1+".jsonl")
	orig := `{"cwd":"` + src + `"}` + "\n"
	put(t, jl, orig, old)
	agent := filepath.Join(cfg, "projects", enc, id1, "subagents", "a.jsonl")
	put(t, agent, orig, old)
	os.MkdirAll(agent+".tmp", 0o755) // le .tmp du sous-agent ne peut pas être créé
	if _, err := reassign([]item{{enc, id1}}, dst); err == nil {
		t.Fatal("échec de réécriture non signalé")
	}
	if data, _ := os.ReadFile(jl); string(data) != orig {
		t.Fatalf("transcript principal réécrit malgré l'échec :\n%s", data)
	}
	if _, err := os.Stat(jl + ".tmp"); err == nil {
		t.Fatal(".tmp du transcript principal laissé derrière")
	}
	if _, err := os.Stat(filepath.Join(cfg, "projects", nonAlnum.ReplaceAllString(dst, "-"), id1+".jsonl")); err == nil {
		t.Fatal("session déplacée malgré l'échec")
	}
}
