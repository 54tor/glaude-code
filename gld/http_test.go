package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Valeurs d'origine, capturées avant que les tests ne les remplacent.
var (
	realAlive    = liveSessions
	realWaiting  = waiting
	realOpen     = openSessions
	realMemDraft = memDraft
)

const testHost = "gld.test"

// Faux claude : `agents --json` renvoie $FAKE_AGENTS ; un chat (stream-json) renvoie $FAKE_CHAT et $FAKE_ERR,
// code $FAKE_RC ; sinon (brouillon, --output-format json) $FAKE_DRAFT, ou la 1re ligne de stdin si FAKE_DRAFT=stdin.
const fakeClaude = `#!/bin/sh
case "$1" in agents) printf '%s\n' "$FAKE_AGENTS"; exit 0;; esac
for a; do
	if [ "$a" = stream-json ]; then printf '%s\n' "$FAKE_CHAT"; printf '%s' "$FAKE_ERR" >&2; exit ${FAKE_RC:-0}; fi
done
if [ "$FAKE_DRAFT" = stdin ]; then IFS= read -r l; printf '{"result":"%s"}\n' "$l"; exit 0; fi
printf '%s\n' "$FAKE_DRAFT"
`

// fakeMatin remplace cliMatin : répète ses arguments, échoue si $FAKE_MATIN_RC n'est pas 0.
func fakeMatin(ctx context.Context, args ...string) (string, error) {
	var err error
	if rc := os.Getenv("FAKE_MATIN_RC"); rc != "" && rc != "0" {
		err = errors.New("code " + rc)
	}
	return "gld-matin " + strings.Join(args, " ") + "\n", err
}

// web : une config Claude avec une session (id1) dans un projet qui existe, les vrais appels à claude
// (vers les faux ci-dessus) et le serveur complet derrière ses gardes.
func web(t *testing.T) (h http.Handler, cfg, proj string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Les faux binaires ne sont trouvés que via ~/.local/bin, que withPath ajoute au PATH.
	bin := filepath.Join(home, ".local", "bin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "claude"), []byte(fakeClaude), 0o755)
	realMatin := matin
	matin = fakeMatin
	t.Cleanup(func() { matin = realMatin })
	t.Setenv("PATH", t.TempDir())
	t.Setenv("FAKE_AGENTS", "[]")
	t.Setenv("GLD_WEB_SNIPPETS", "")
	cfg = filepath.Join(home, ".claude")
	use(cfg)
	liveSessions, waiting, openSessions, memDraft = realAlive, realWaiting, realOpen, realMemDraft
	t.Cleanup(func() { liveSessions = func() map[string]bool { return map[string]bool{} } })
	proj = filepath.Join(home, "p")
	os.MkdirAll(proj, 0o755)
	put(t, filepath.Join(cfg, "projects", "-p", id1+".jsonl"),
		`{"type":"user","cwd":"`+proj+`","gitBranch":"main","timestamp":"2026-01-01T00:00:00Z","message":{"content":"Déploie caddy"}}`+"\n"+
			`{"type":"assistant","message":{"content":[{"type":"text","text":"fait"},{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`+"\n", old)
	hosts[testHost] = true
	t.Cleanup(ideCloseAll)
	return handler(), cfg, proj
}

// call : une requête au serveur ; corps JSON décodé dans m quand c'en est un.
func call(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = testHost
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	m := map[string]any{}
	json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m, w.Body.String()
}

// ok : réponse {ok, out} attendue ; out doit contenir want.
func ok(t *testing.T, h http.Handler, path, body string, wantOK bool, want string) map[string]any {
	t.Helper()
	code, m, raw := call(t, h, "POST", path, body)
	if code != 200 || m["ok"] != wantOK || !strings.Contains(raw, want) {
		t.Fatalf("%s %s : %d %s", path, cut(body, 200), code, cut(raw, 300))
	}
	return m
}

// status : seul le code HTTP compte.
func status(t *testing.T, h http.Handler, method, path, body string, want int) map[string]any {
	t.Helper()
	code, m, raw := call(t, h, method, path, body)
	if code != want {
		t.Fatalf("%s %s %s : %d au lieu de %d %s", method, path, cut(body, 200), code, want, cut(raw, 300))
	}
	return m
}

func agents(t *testing.T, ids ...string) {
	var a []map[string]string
	for _, id := range ids {
		a = append(a, map[string]string{"sessionId": id, "status": "idle", "kind": "interactive"})
	}
	b, _ := json.Marshal(a)
	t.Setenv("FAKE_AGENTS", string(b))
}

func TestHTTPGardes(t *testing.T) {
	h, _, _ := web(t)
	r := httptest.NewRequest("GET", "/api", nil)
	r.Host = "evil.com"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("Host étranger servi :", w.Code)
	}
	r = httptest.NewRequest("POST", "/vgroups", strings.NewReader(`{"groups":[],"favs":[]}`))
	r.Host = testHost
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("POST d'une autre origine accepté :", w.Code)
	}
	// L'interface compilée (ui/dist) n'est pas versionnée : sans elle, on ne vérifie pas la page servie.
	if _, err := uiFS.ReadFile("ui/dist/index.html"); err != nil {
		return
	}
	for _, p := range []string{"/", "/en-cours", "/memoire/x"} {
		code, _, raw := call(t, h, "GET", p, "")
		r := httptest.NewRecorder()
		h.ServeHTTP(r, func() *http.Request { q := httptest.NewRequest("GET", p, nil); q.Host = testHost; return q }())
		if code != 200 || !strings.Contains(raw, "<!doctype html>") || !strings.Contains(r.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatalf("page %s : %d", p, code)
		}
	}
	status(t, h, "GET", "/favicon.ico", "", 200)
}

func TestHTTPSessions(t *testing.T) {
	h, cfg, proj := web(t)
	agents(t, id2)
	m := status(t, h, "GET", "/api", "", 200)
	if m["root"] != filepath.Join(cfg, "projects") || len(m["groups"].([]any)) != 1 || m["live"].([]any)[0] != id2 || m["waiting"].([]any)[0] != id2 {
		t.Fatalf("/api : %v", m)
	}
	t.Setenv("FAKE_AGENTS", "illisible")
	if m := status(t, h, "GET", "/api", "", 200); len(m["live"].([]any)) != 0 || len(m["waiting"].([]any)) != 0 {
		t.Fatalf("/api sans claude agents : %v", m)
	}
	t.Setenv("FAKE_AGENTS", "[]")

	// Explorateur : chemin relatif refusé, chemin disparu remonté au parent, liens vers des dossiers suivis.
	status(t, h, "GET", "/ls?path=relatif", "", 400)
	os.MkdirAll(filepath.Join(proj, "b"), 0o755)
	os.MkdirAll(filepath.Join(proj, "a"), 0o755)
	os.WriteFile(filepath.Join(proj, "f.txt"), nil, 0o644)
	os.Symlink(filepath.Join(proj, "a"), filepath.Join(proj, "lien"))
	os.Symlink(filepath.Join(proj, "f.txt"), filepath.Join(proj, "lienf"))
	m = status(t, h, "GET", "/ls?path="+proj+"/disparu/plus", "", 200)
	if m["path"] != proj || m["parent"] != filepath.Dir(proj) || strings.Join(strs(m["dirs"]), ",") != "a,b,lien" {
		t.Fatalf("/ls : %v", m)
	}
	if m := status(t, h, "GET", "/ls", "", 200); m["path"] != filepath.Dir(proj) {
		t.Fatalf("/ls sans chemin : %v", m)
	}
	if m := status(t, h, "GET", "/ls?path=/", "", 200); m["parent"] != "" || m["path"] != "/" {
		t.Fatalf("/ls racine : %v", m)
	}
	os.MkdirAll(filepath.Join(proj, "fermé"), 0o000)
	defer os.Chmod(filepath.Join(proj, "fermé"), 0o755)
	if os.Getuid() != 0 {
		status(t, h, "GET", "/ls?path="+filepath.Join(proj, "fermé"), "", 403)
	}

	// Lecture d'une conversation et de son détail.
	status(t, h, "GET", "/conv?dir=-p&id=../x", "", 404)
	status(t, h, "GET", "/info?dir=-p&id="+id2, "", 404)
	if m := status(t, h, "GET", "/conv?dir=-p&id="+id1, "", 200); len(m["msgs"].([]any)) != 2 {
		t.Fatalf("/conv : %v", m)
	}
	if m := status(t, h, "GET", "/conv?dir=-p&id="+id1+"&tools=1", "", 200); len(m["msgs"].([]any)) != 3 {
		t.Fatalf("/conv avec outils : %v", m)
	}
	if m := status(t, h, "GET", "/info?dir=-p&id="+id1, "", 200); m["branch"] != "main" {
		t.Fatalf("/info : %v", m)
	}

	// Recherche : de 2 à 200 caractères.
	status(t, h, "GET", "/search?q=a", "", 400)
	status(t, h, "GET", "/search?q="+strings.Repeat("a", 201), "", 400)
	status(t, h, "GET", "/search?q=ab", "", 200)
	if m := status(t, h, "GET", "/search?q="+strings.Repeat("é", 200), "", 200); len(m["hits"].([]any)) != 0 {
		t.Fatal("200 caractères : la limite compte des caractères, pas des octets")
	}
	if m := status(t, h, "GET", "/search?q=caddy", "", 200); len(m["hits"].([]any)) != 1 {
		t.Fatalf("/search : %v", m)
	}

	// Renommage : requête, titre, session ouverte, sessions ouvertes inconnues, puis succès.
	status(t, h, "POST", "/rename", "{", 400)
	status(t, h, "POST", "/rename", `{"dir":"-p","id":"`+id1+`","title":"  "}`, 400)
	status(t, h, "POST", "/rename", `{"dir":"-p","id":"`+id1+`","title":"`+strings.Repeat("é", 201)+`"}`, 400)
	agents(t, id1)
	ok(t, h, "/rename", `{"dir":"-p","id":"`+id1+`","title":"x"}`, false, "session ouverte")
	t.Setenv("FAKE_AGENTS", "illisible")
	ok(t, h, "/rename", `{"dir":"-p","id":"`+id1+`","title":"x"}`, false, "action refusée")
	t.Setenv("FAKE_AGENTS", "[]")
	ok(t, h, "/rename", `{"dir":"-p","id":"`+id1+`","title":"`+strings.Repeat("é", 200)+`"}`, true, "Renommée")
	ok(t, h, "/rename", `{"dir":"-p","id":"`+id1+`","title":" Caddy \n prod "}`, true, "Renommée : Caddy prod")
	os.Chmod(filepath.Join(cfg, "projects", "-p", id1+".jsonl"), 0o400)
	if os.Getuid() != 0 {
		ok(t, h, "/rename", `{"dir":"-p","id":"`+id1+`","title":"y"}`, false, "")
	}
	os.Chmod(filepath.Join(cfg, "projects", "-p", id1+".jsonl"), 0o644)

	// Réassignation.
	status(t, h, "POST", "/reassign", `{"items":[]}`, 400)
	status(t, h, "POST", "/reassign", `{"items":[{"dir":"-p","id":"`+id2+`"}]}`, 400)
	ok(t, h, "/reassign", `{"items":[{"dir":"-p","id":"`+id1+`"}],"to":"/nexiste/pas"}`, false, "n'existe pas")
	ok(t, h, "/reassign", `{"items":[{"dir":"-p","id":"`+id1+`"}],"to":"`+filepath.Join(proj, "a")+`"}`, true, "1 session(s) réassignée(s)")
}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestHTTPRm(t *testing.T) {
	h, cfg, _ := web(t)
	put(t, filepath.Join(cfg, "projects", "-p", id2+".jsonl"), `{"cwd":"/p"}`+"\n", old)
	status(t, h, "POST", "/rm", "{", 400)
	t.Setenv("FAKE_AGENTS", "illisible")
	ok(t, h, "/rm", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, false, "action refusée")
	agents(t, id1)
	ok(t, h, "/rm", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, false, "Aucune session supprimée ; 1 session ouverte épargnée")
	ok(t, h, "/rm", `{"items":[{"dir":"-p","id":"`+id1+`"},{"dir":"-p","id":"`+id2+`"}]}`, true, "1 session supprimée ; 1 session ouverte épargnée")
	if exists(filepath.Join(cfg, "projects", "-p", id2+".jsonl")) || !exists(filepath.Join(cfg, "projects", "-p", id1+".jsonl")) {
		t.Fatal("mauvaise session effacée")
	}
}

func TestHTTPLancements(t *testing.T) {
	h, cfg, proj := web(t)
	put(t, filepath.Join(cfg, "projects", "-q", id2+".jsonl"), `{"cwd":"/nexiste/pas"}`+"\n", old)
	os.MkdirAll(filepath.Join(cfg, "projects", "-vide"), 0o755)

	// Ouverture dans un terminal (gld-matin).
	status(t, h, "POST", "/open", "{", 400)
	status(t, h, "POST", "/open", `{"dir":"-p","args":"--x;rm"}`, 400)
	status(t, h, "POST", "/open", `{"dir":"-p","prompt":"sans session"}`, 400)
	status(t, h, "POST", "/open", `{"dir":"-p","id":"`+id3+`"}`, 400)
	for _, d := range []string{"", "a/b", `a\b`, ".."} {
		status(t, h, "POST", "/open", `{"dir":"`+strings.ReplaceAll(d, `\`, `\\`)+`"}`, 400)
	}
	status(t, h, "POST", "/open", `{"dir":"-vide"}`, 404)
	ok(t, h, "/open", `{"dir":"-q"}`, false, "dossier du projet introuvable : /nexiste/pas")
	ok(t, h, "/open", `{"dir":"-p","args":"--model=opus"}`, true, "gld-matin --args --model=opus "+proj)
	ok(t, h, "/open", `{"dir":"-p","id":"`+id1+`","prompt":"continue"}`, true, "--id "+id1+" --prompt continue "+proj)
	t.Setenv("FAKE_MATIN_RC", "1")
	ok(t, h, "/open", `{"dir":"-p","id":"`+id1+`"}`, false, "gld-matin --id")
	t.Setenv("FAKE_MATIN_RC", "0")

	// Nouvelle session à côté d'une autre.
	status(t, h, "POST", "/new-session", "{", 400)
	ok(t, h, "/new-session", `{"dir":"-p","id":"../x","prompt":"p"}`, false, "session invalide")
	ok(t, h, "/new-session", `{"dir":"-p","id":"`+id1+`","prompt":"p","args":"a;b"}`, false, "option refusée")
	ok(t, h, "/new-session", `{"cwd":"relatif","prompt":"p"}`, false, "dossier invalide")
	ok(t, h, "/new-session", `{"cwd":"/n/existe/pas/xyz","prompt":"p"}`, false, "dossier du projet introuvable")
	ok(t, h, "/new-session", `{"dir":"-p","id":"`+id1+`","prompt":" "}`, false, "prompt vide")
	ok(t, h, "/new-session", `{"dir":"-p","id":"`+id1+`","prompt":"`+strings.Repeat("x", 33<<10)+`"}`, false, "trop long")
	ok(t, h, "/new-session", `{"dir":"-q","id":"`+id2+`","prompt":"p"}`, false, "introuvable")
	agents(t, id1)         // ouverte : n'empêche pas d'en démarrer une autre
	var left time.Duration // le lancement est borné : un terminal qui ne rend pas la main n'immobilise pas la requête
	fm := matin
	matin = func(ctx context.Context, args ...string) (string, error) {
		if d, ok := ctx.Deadline(); ok {
			left = time.Until(d)
		}
		return fm(ctx, args...)
	}
	ok(t, h, "/new-session", `{"dir":"-p","id":"`+id1+`","prompt":"suite"}`, true, "gld-matin --prompt suite "+proj)
	matin = fm
	if left < 25*time.Second || left > 30*time.Second {
		t.Fatalf("délai du lancement : %v", left)
	}
	t.Setenv("FAKE_AGENTS", "illisible")
	ok(t, h, "/new-session", `{"dir":"-p","id":"`+id1+`","prompt":"suite"}`, true, "gld-matin")
	t.Setenv("FAKE_AGENTS", "[]")
	// Fusion de mémoire : prompt seul, ou lancé dans une nouvelle session.
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "a.md"), "a", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "b.md"), "b", old)
	keys := `"keys":["m/-p/a.md","m/-p/b.md"]`
	status(t, h, "POST", "/memory-merge", "{", 400)
	ok(t, h, "/memory-merge", `{"keys":["m/-p/a.md"]}`, false, "")
	if m := ok(t, h, "/memory-merge", `{`+keys+`}`, true, "Prompt copié"); !strings.Contains(m["prompt"].(string), "b.md") {
		t.Fatal("prompt de fusion :", m)
	}
	if m := ok(t, h, "/memory-merge", `{`+keys+`,"launch":true,"args":"a;b"}`, false, "option refusée"); m["prompt"] == "" {
		t.Fatal("le prompt doit rester copiable")
	}
	ok(t, h, "/memory-merge", `{`+keys+`,"launch":true,"args":"--model=haiku"}`, true, "gld-matin --args --model=haiku --prompt")
}

func TestHTTPChat(t *testing.T) {
	h, cfg, _ := web(t)
	put(t, filepath.Join(cfg, "projects", "-q", id2+".jsonl"), `{"cwd":"/nexiste/pas"}`+"\n", old)
	for _, c := range [][2]string{
		{"{", "requête invalide"},
		{`{"dir":"-p","id":"` + id3 + `","prompt":"x"}`, "session invalide"},
		{`{"dir":"-p","id":"` + id1 + `","prompt":"x","args":"a;b"}`, "option refusée"},
		{`{"dir":"-p","id":"` + id1 + `","prompt":" "}`, "message vide"},
		{`{"dir":"-p","id":"` + id1 + `","prompt":"x","mode":"bypassPermissions"}`, "mode d'autorisation inconnu"},
		{`{"dir":"-q","id":"` + id2 + `","prompt":"x"}`, "introuvable"},
	} {
		if code, _, raw := call(t, h, "POST", "/chat", c[0]); code != 400 || !strings.Contains(raw, c[1]) {
			t.Fatalf("%s : %d %s", c[0], code, raw)
		}
	}
	agents(t, id1)
	if code, _, raw := call(t, h, "POST", "/chat", `{"dir":"-p","id":"`+id1+`","prompt":"x"}`); code != 400 || !strings.Contains(raw, "déjà ouverte") {
		t.Fatal("session ouverte :", code, raw)
	}
	t.Setenv("FAKE_AGENTS", "[]")

	// Réponse relayée ; option non permise au chat signalée.
	t.Setenv("FAKE_CHAT", `{"type":"assistant","message":{"content":[{"type":"text","text":"Bonjour"}]}}`+"\n"+`{"type":"result","subtype":"success","total_cost_usd":0.5}`)
	code, _, raw := call(t, h, "POST", "/chat", `{"dir":"-p","id":"`+id1+`","prompt":"salut","args":"--model=opus --add-dir=/x","mode":"plan"}`)
	if code != 200 || strings.Count(raw, "\n") != 3 || !strings.Contains(raw, `"note":"Options de Réglages non transmises au chat : --add-dir=/x`) ||
		!strings.Contains(raw, `"t":"Bonjour"`) || !strings.Contains(raw, `"done":true,"ok":true,"cost":0.5`) {
		t.Fatalf("chat : %d %s", code, raw)
	}
	// Échec sans résultat : l'erreur de claude, sinon celle du processus.
	t.Setenv("FAKE_CHAT", "")
	t.Setenv("FAKE_ERR", "option inconnue")
	t.Setenv("FAKE_RC", "1")
	if _, _, raw := call(t, h, "POST", "/chat", `{"dir":"-p","id":"`+id1+`","prompt":"salut"}`); raw != `{"done":true,"error":"option inconnue"}`+"\n" {
		t.Fatalf("chat en échec : %s", raw)
	}
	t.Setenv("FAKE_ERR", "")
	if _, _, raw := call(t, h, "POST", "/chat", `{"dir":"-p","id":"`+id1+`","prompt":"salut"}`); !strings.Contains(raw, `"error":"exit status 1"`) {
		t.Fatalf("chat en échec muet : %s", raw)
	}
	// Résultat en erreur.
	t.Setenv("FAKE_RC", "0")
	t.Setenv("FAKE_CHAT", `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"trop de tours"}`)
	if _, _, raw := call(t, h, "POST", "/chat", `{"dir":"-p","id":"`+id1+`","prompt":"salut"}`); !strings.Contains(raw, `"error":"error_max_turns trop de tours"`) {
		t.Fatalf("résultat en erreur : %s", raw)
	}
	// Un résultat qui n'est ni en erreur ni un succès (sous-type inconnu) n'est pas présenté comme réussi.
	t.Setenv("FAKE_CHAT", `{"type":"result","subtype":"error_during_execution","is_error":false}`)
	if _, _, raw := call(t, h, "POST", "/chat", `{"dir":"-p","id":"`+id1+`","prompt":"salut"}`); strings.Contains(raw, `"ok":true`) || !strings.Contains(raw, `"error":"error_during_execution"`) {
		t.Fatalf("résultat ambigu présenté comme réussi : %s", raw)
	}

	// Un seul tour à la fois par session ; arrêt.
	ok(t, h, "/chat-stop", `{"id":"`+id1+`"}`, false, "rien en cours")
	sleep, err := exec.LookPath("/bin/sleep")
	if err != nil {
		t.Skip("pas de /bin/sleep")
	}
	cmd := exec.Command(sleep, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	chatMu.Lock()
	chatRun[id1] = cmd
	chatMu.Unlock()
	defer func() { chatMu.Lock(); delete(chatRun, id1); chatMu.Unlock() }()
	if code, _, _ := call(t, h, "POST", "/chat", `{"dir":"-p","id":"`+id1+`","prompt":"x"}`); code != 409 {
		t.Fatal("deuxième tour simultané :", code)
	}
	ok(t, h, "/chat-stop", `{"id":"`+id1+`"}`, true, "Arrêt demandé")
	if cmd.Wait() == nil {
		t.Fatal("le processus devait être interrompu")
	}
}

func TestHTTPSnippets(t *testing.T) {
	h, cfg, _ := web(t)
	put(t, filepath.Join(cfg, "commands", "revue.md"), "---\ndescription: Revue\n---\nFais une revue", old)
	if m := status(t, h, "GET", "/snippet-list", "", 200); m["dir"] != filepath.Join(cfg, "commands") || len(m["files"].([]any)) != 1 {
		t.Fatalf("liste : %v", m)
	}
	if m := status(t, h, "GET", "/snippet?key=u/nouveau.md", "", 200); m["missing"] != true {
		t.Fatalf("snippet absent : %v", m)
	}
	status(t, h, "GET", "/snippet?key=u/../x.md", "", 404)
	if m := status(t, h, "GET", "/snippet?key=u/revue.md", "", 200); !strings.Contains(m["text"].(string), "Fais une revue") {
		t.Fatalf("snippet : %v", m)
	}
	status(t, h, "POST", "/snippet-save", "{", 400)
	ok(t, h, "/snippet-save", `{"key":"u/revue.md","text":"x","create":true}`, false, "")
	ok(t, h, "/snippet-save", `{"key":"u/revue.md","text":"v2"}`, true, "Snippet enregistré")
	status(t, h, "POST", "/snippet-draft", "{", 400)
	ok(t, h, "/snippet-draft", `{"model":"gpt","text":"x"}`, false, "")
	t.Setenv("FAKE_DRAFT", `{"result":"`+"```md\\nmis en forme\\n```"+`","total_cost_usd":0.25}`)
	if m := ok(t, h, "/snippet-draft", `{"model":"haiku","text":"x"}`, true, "Mise en forme prête"); m["text"] != "mis en forme\n" || m["cost"] != 0.25 {
		t.Fatalf("brouillon : %v", m)
	}
	ok(t, h, "/snippet-rename", `{"from":"u/revue.md","to":"u/revue2.md"}`, true, "Snippet renommé")
	ok(t, h, "/snippet-rename", `{"from":"u/revue.md","to":"u/x.md"}`, false, "")
	ok(t, h, "/snippet-rename", `{"from":"u/revue2.md","to":"p/a/x.md"}`, false, "")
	ok(t, h, "/snippet-rename", `{"from":"u/revue2.md","to":"u/revue.md"}`, true, "Snippet renommé")
	ok(t, h, "/snippet-rm", `{"key":"u/absent.md"}`, false, "")
	ok(t, h, "/snippet-rm", `{"key":"u/revue.md"}`, true, "Snippet supprimé")

	// Export et import.
	put(t, filepath.Join(cfg, "commands", "a.md"), "A", old)
	status(t, h, "GET", "/snippets-export?scope=../x", "", 400)
	r := httptest.NewRecorder()
	q := httptest.NewRequest("GET", "/snippets-export", nil)
	q.Host = testHost
	h.ServeHTTP(r, q)
	zr, err := zip.NewReader(bytes.NewReader(r.Body.Bytes()), int64(r.Body.Len()))
	if r.Code != 200 || err != nil || len(zr.File) != 1 || !strings.Contains(r.Header().Get("Content-Disposition"), "commandes-personnelles.zip") {
		t.Fatalf("export : %d %v", r.Code, err)
	}
	put(t, filepath.Join(cfg, "projects", "-p", "x"), "", old) // le dossier -p existe : scope valide
	r = httptest.NewRecorder()
	q = httptest.NewRequest("GET", "/snippets-export?scope=-p", nil)
	q.Host = testHost
	h.ServeHTTP(r, q)
	if !strings.Contains(r.Header().Get("Content-Disposition"), `filename=commandes-p.zip`) {
		t.Fatalf("export de projet : %d %s", r.Code, r.Header())
	}

	imp := func(scope, overwrite string, files map[string]string) (int, string) {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		mw.WriteField("scope", scope)
		mw.WriteField("overwrite", overwrite)
		for n, c := range files {
			fw, _ := mw.CreateFormFile("files", n)
			fw.Write([]byte(c))
		}
		mw.Close()
		q := httptest.NewRequest("POST", "/snippets-import", &b)
		q.Host = testHost
		q.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, q)
		return w.Code, w.Body.String()
	}
	if code, _, _ := call(t, h, "POST", "/snippets-import", "pas du multipart"); code != 400 {
		t.Fatal("import invalide :", code)
	}
	if code, raw := imp("../x", "", nil); code != 400 || !strings.Contains(raw, "scope invalide") {
		t.Fatal(code, raw)
	}
	if code, raw := imp("", "", map[string]string{"a.md": "autre", "b.md": "B"}); code != 200 || !strings.Contains(raw, "1 importé(s), 1 déjà présent(s), laissé(s) tel(s) quel(s) : a.md") {
		t.Fatal(code, raw)
	}
	if code, raw := imp("", "1", map[string]string{"a.md": "remplacé"}); code != 200 || !strings.Contains(raw, `"ok":true,"out":"1 importé(s)"`) {
		t.Fatal(code, raw)
	}
	if code, raw := imp("", "", map[string]string{"x.txt": "pas une commande"}); code != 200 || !strings.Contains(raw, `"ok":false`) {
		t.Fatal(code, raw)
	}
}

func TestHTTPMemoire(t *testing.T) {
	h, cfg, _ := web(t)
	put(t, filepath.Join(cfg, "CLAUDE.md"), "global", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md"), "- [A](a.md) — a\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "a.md"), "entrée a", old)
	if m := status(t, h, "GET", "/memory-list", "", 200); len(m["global"].([]any)) != 1 || len(m["projects"].([]any)) != 1 {
		t.Fatalf("liste : %v", m)
	}
	if m := status(t, h, "GET", "/memory-file?key=m/-p/b.md", "", 200); m["missing"] != true {
		t.Fatal("entrée absente :", m)
	}
	status(t, h, "GET", "/memory-file?key=g/settings.json", "", 404)
	if m := status(t, h, "GET", "/memory-file?key=g/CLAUDE.md", "", 200); m["text"] != "global" {
		t.Fatal(m)
	}
	status(t, h, "POST", "/memory-save", "{", 400)
	ok(t, h, "/memory-save", `{"key":"g/settings.json","text":"x"}`, false, "")
	ok(t, h, "/memory-save", `{"key":"m/-p/b.md","text":"---\nname: B\ndescription: bé\n---\nB","index":true}`, true, "Enregistré")
	if b, _ := os.ReadFile(filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md")); !strings.Contains(string(b), "[B](b.md) — bé") {
		t.Fatalf("index : %s", b)
	}
	ok(t, h, "/memory-save", `{"key":"m/-p/c.md","text":"c"}`, true, "Enregistré")
	if b, _ := os.ReadFile(filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md")); strings.Contains(string(b), "c.md") {
		t.Fatal("indexé sans index: true")
	}
	status(t, h, "POST", "/memory-move", "{", 400)
	ok(t, h, "/memory-move", `{"key":"m/-p/a.md","to":"../x"}`, false, "")
	if m := ok(t, h, "/memory-move", `{"key":"m/-p/a.md","to":"global"}`, true, "Déplacé"); m["key"] != "g/rules/a.md" {
		t.Fatal(m)
	}
	status(t, h, "POST", "/memory-draft", "{", 400)
	ok(t, h, "/memory-draft", `{"model":"gpt","instruction":"x"}`, false, "")
	t.Setenv("FAKE_DRAFT", `{"result":"brouillon","total_cost_usd":0.1}`)
	if m := ok(t, h, "/memory-draft", `{"model":"haiku","instruction":"écris","refs":["m/-p/b.md"]}`, true, "Brouillon prêt"); m["text"] != "brouillon\n" {
		t.Fatal(m)
	}
	status(t, h, "POST", "/memory-rm", "{", 400)
	ok(t, h, "/memory-rm", `{"key":"m/-p/absent.md"}`, false, "")
	ok(t, h, "/memory-rm", `{"key":"m/-p/b.md"}`, true, "Supprimé")

	// Corbeille.
	l := status(t, h, "GET", "/trash", "", 200)["items"].([]any)
	if len(l) != 1 {
		t.Fatalf("corbeille : %v", l)
	}
	id := l[0].(map[string]any)["id"].(string)
	status(t, h, "GET", "/trash-file?id=../x", "", 404)
	if m := status(t, h, "GET", "/trash-file?id="+id, "", 200); !strings.Contains(m["text"].(string), "name: B") {
		t.Fatal(m)
	}
	ok(t, h, "/trash-restore", `{"id":"../x"}`, false, "")
	if m := ok(t, h, "/trash-restore", `{"id":"`+id+`"}`, true, "Restauré"); m["key"] != "m/-p/b.md" {
		t.Fatal(m)
	}
	ok(t, h, "/memory-rm", `{"key":"m/-p/b.md"}`, true, "Supprimé")
	if code, m, _ := call(t, h, "POST", "/trash-purge", `{}`); code != 400 || m["ok"] != false {
		t.Fatal("corps vide : la corbeille ne se vide jamais implicitement")
	}
	ok(t, h, "/trash-purge", `{"id":"../x"}`, false, "")
	ok(t, h, "/trash-purge", `{"all":true}`, true, "Corbeille vidée")
	if l := status(t, h, "GET", "/trash", "", 200)["items"].([]any); len(l) != 0 {
		t.Fatal("corbeille non vidée")
	}
}

func TestHTTPOngletsGroupes(t *testing.T) {
	h, _, _ := web(t)
	if m := status(t, h, "GET", "/tabs", "", 200); len(m["saves"].([]any)) != 0 {
		t.Fatal(m)
	}
	ok(t, h, "/tabs-save", `{"name":"x"}`, false, "")
	t.Setenv("FAKE_AGENTS", `[{"sessionId":"`+id1+`","kind":"interactive"},{"sessionId":"`+id2+`","kind":"background"},{"sessionId":"../x","kind":"interactive"}]`)
	ok(t, h, "/tabs-save", `{"name":"soir"}`, true, "1 session(s) sauvegardée(s)")
	saves := status(t, h, "GET", "/tabs", "", 200)["saves"].([]any)
	ok(t, h, "/tabs-rm", `{"at":"jamais"}`, false, "")
	ok(t, h, "/tabs-rm", `{"at":"`+saves[0].(map[string]any)["at"].(string)+`"}`, true, "Sauvegarde supprimée")

	status(t, h, "POST", "/vgroups", "{", 400)
	status(t, h, "POST", "/vgroups", `{"groups":[]}`, 400)
	ok(t, h, "/vgroups", `{"groups":[{"name":"a"}],"favs":[]}`, false, "")
	ok(t, h, "/vgroups", `{"groups":[{"name":"Infra","project":"/p","ids":["`+id1+`"]}],"favs":["`+id1+`"]}`, true, "Groupes enregistrés")
	if m := status(t, h, "GET", "/api", "", 200); len(m["vgroups"].([]any)) != 1 || m["favs"].([]any)[0] != id1 {
		t.Fatal(m)
	}

	ok(t, h, "/vgroups", `{"groups":[],"favs":[],"projects":[{"name":"Site","dirs":["/a","/b/"]}]}`, true, "Groupes enregistrés")
	ok(t, h, "/vgroups", `{"groups":[],"favs":[],"projects":[{"name":"Site","dirs":["rel"]}]}`, false, "")
	ok(t, h, "/vgroups", `{"groups":[],"favs":[],"projects":[{"name":"X","dirs":[]},{"name":"X","dirs":[]}]}`, false, "")
	ok(t, h, "/vgroups", `{"groups":[],"favs":[]}`, true, "Groupes enregistrés") // sans « projects » : conservés
	m := status(t, h, "GET", "/api", "", 200)["projects"].([]any)
	if len(m) != 1 || m[0].(map[string]any)["dirs"].([]any)[1] != "/b" {
		t.Fatal(m)
	}
}

func TestSlashCommands(t *testing.T) {
	cfg := t.TempDir()
	old := root
	t.Cleanup(func() { root = old })
	root = filepath.Join(cfg, "projects")
	os.MkdirAll(filepath.Join(cfg, "commands"), 0o755)
	os.WriteFile(filepath.Join(cfg, "commands", "audit.md"), []byte("---\ndescription: Audit du dépôt\n---\ncorps\n"), 0o644)
	os.MkdirAll(filepath.Join(cfg, "skills", "mienne"), 0o755)
	os.WriteFile(filepath.Join(cfg, "skills", "mienne", "SKILL.md"), []byte("---\nname: mienne\ndescription: Ma skill\n---\nx\n"), 0o644)
	got := map[string]string{}
	for _, c := range slashCommands() {
		got[c.Cmd] = c.Desc
	}
	if got["/audit"] != "Audit du dépôt" || got["/mienne"] != "Ma skill" {
		t.Fatalf("commandes : %v", got)
	}
}

func TestTerminalDesactiveParDefaut(t *testing.T) {
	t.Setenv("GLD_WEB_TERMINAL", "")
	h, _, _ := web(t)
	if r := getRaw(h, "/term-info"); !strings.Contains(r.Body.String(), `"enabled":false`) {
		t.Fatalf("term-info : %s", r.Body.String())
	}
	if r := getRaw(h, "/term-ws?t=x"); r.Code != 404 {
		t.Fatalf("term-ws actif sans réglage : %d", r.Code)
	}
	req := httptest.NewRequest("POST", "/term-open", strings.NewReader(`{"id":"`+id1+`"}`))
	req.Host = testHost
	req.Header.Set("Origin", "http://"+testHost)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("term-open actif sans réglage : %d %s", rec.Code, rec.Body.String())
	}
	t.Setenv("GLD_WEB_TERMINAL", "1")
	if r := getRaw(h, "/term-ws?t=x"); r.Code != 403 || !strings.Contains(r.Body.String(), "forbidden") { // sans Origin
		t.Fatalf("term-ws sans Origin : %d %s", r.Code, r.Body.String())
	}
	// Derrière la passerelle, l'Origin a été vérifié puis retiré : seul le jeton du terminal compte encore.
	childToken = "secret"
	t.Cleanup(func() { childToken = "" })
	req = httptest.NewRequest("GET", "/term-ws?t=x", nil)
	req.Host = testHost
	req.Header.Set(childHeader, "secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "jeton") {
		t.Fatalf("term-ws derrière la passerelle : %d %s", rec.Code, rec.Body.String())
	}
}

func TestSessionsStamp(t *testing.T) {
	h, cfg, _ := web(t)
	_, m, _ := call(t, h, "GET", "/sessions-stamp", "")
	first, _ := m["stamp"].(float64)
	if first == 0 {
		t.Fatal("tampon vide")
	}
	p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	later := time.Now().Add(time.Hour)
	os.Chtimes(p, later, later)
	_, m, _ = call(t, h, "GET", "/sessions-stamp", "")
	if second, _ := m["stamp"].(float64); second <= first {
		t.Fatalf("le tampon n'a pas bougé : %v -> %v", first, second)
	}
}

func TestDossiersEnregistres(t *testing.T) {
	h, _, _ := web(t)
	vide := t.TempDir()
	status(t, h, "POST", "/dirs", `{"op":"add","dir":"relatif"}`, 400)
	status(t, h, "POST", "/dirs", `{"op":"x","dir":"`+vide+`"}`, 400)
	ok(t, h, "/dirs", `{"op":"add","dir":"`+vide+`/inconnu"}`, false, "introuvable")
	ok(t, h, "/dirs", `{"op":"add","dir":"`+vide+`"}`, true, "")
	ok(t, h, "/dirs", `{"op":"add","dir":"`+vide+`/"}`, true, "") // doublon : un seul exemplaire
	_, m, _ := call(t, h, "GET", "/api", "")
	n := 0
	for _, g := range m["groups"].([]any) {
		if g.(map[string]any)["project"] == vide {
			n++
			if len(g.(map[string]any)["sessions"].([]any)) != 0 {
				t.Fatal("le dossier enregistré a des sessions")
			}
		}
	}
	if n != 1 {
		t.Fatalf("dossier vide présent %d fois", n)
	}
	// un client qui ne connaît pas le champ ne l'efface pas
	status(t, h, "POST", "/vgroups", `{"groups":[],"favs":[]}`, 200)
	_, m, _ = call(t, h, "GET", "/api", "")
	if d, _ := m["dirs"].([]any); len(d) != 1 {
		t.Fatalf("dossiers perdus : %v", m["dirs"])
	}
	ok(t, h, "/dirs", `{"op":"remove","dir":"`+vide+`"}`, true, "")
	_, m, _ = call(t, h, "GET", "/api", "")
	if d, _ := m["dirs"].([]any); len(d) != 0 {
		t.Fatalf("retrait sans effet : %v", m["dirs"])
	}
}

func TestRelayUpstreamLimit(t *testing.T) {
	in := `{"type":"system","subtype":"api_retry","attempt":2,"max_retries":10,"retry_delay_ms":1500,"error_status":429}` + "\n" +
		`{"type":"result","subtype":"success","is_error":true,"result":"API Error: 529 Overloaded"}` + "\n"
	var evs []chatEvent
	relay(strings.NewReader(in), func(e chatEvent) { evs = append(evs, e) })
	if len(evs) != 2 || !strings.Contains(evs[0].Note, "429") || !strings.Contains(evs[0].Note, "2/10") || !strings.Contains(evs[1].Error, "529") || evs[1].OK {
		t.Fatalf("%+v", evs)
	}
}

func TestHTTPTrashPurgeAllSparesSessionTrash(t *testing.T) {
	h, cfg, _ := web(t)
	sess := filepath.Join(cfg, "gld-web-trash", "sessions", "dummy-id")
	mem := filepath.Join(cfg, "gld-web-trash", "dummy-mem")
	for _, d := range []string{sess, mem} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(d, "f.txt"), []byte("x"), 0o600)
	}
	ok(t, h, "/trash-purge", `{"all":true}`, true, "Corbeille vidée")
	if _, err := os.Stat(sess); err != nil {
		t.Fatal("la corbeille de sessions (droit exec) a été vidée par /trash-purge :", err)
	}
	if _, err := os.Stat(mem); err == nil {
		t.Fatal("la corbeille mémoire n'a pas été vidée")
	}
}
