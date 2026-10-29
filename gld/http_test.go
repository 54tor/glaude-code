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
	"net/url"
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
	r := httptest.NewRequest("GET", "/v1/state", nil)
	r.Host = "evil.com"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("Host étranger servi :", w.Code)
	}
	r = httptest.NewRequest("PUT", "/v1/vgroups", strings.NewReader(`{"groups":[],"favs":[]}`))
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
	m := status(t, h, "GET", "/v1/state", "", 200)
	if m["root"] != filepath.Join(cfg, "projects") || len(m["groups"].([]any)) != 1 || m["live"].([]any)[0] != id2 || m["waiting"].([]any)[0] != id2 {
		t.Fatalf("/v1/state : %v", m)
	}
	t.Setenv("FAKE_AGENTS", "illisible")
	if m := status(t, h, "GET", "/v1/state", "", 200); len(m["live"].([]any)) != 0 || len(m["waiting"].([]any)) != 0 {
		t.Fatalf("/v1/state sans claude agents : %v", m)
	}
	t.Setenv("FAKE_AGENTS", "[]")

	// Explorateur : chemin relatif refusé, chemin disparu remonté au parent, liens vers des dossiers suivis.
	status(t, h, "GET", "/v1/directories?path=relatif", "", 400)
	os.MkdirAll(filepath.Join(proj, "b"), 0o755)
	os.MkdirAll(filepath.Join(proj, "a"), 0o755)
	os.WriteFile(filepath.Join(proj, "f.txt"), nil, 0o644)
	os.Symlink(filepath.Join(proj, "a"), filepath.Join(proj, "lien"))
	os.Symlink(filepath.Join(proj, "f.txt"), filepath.Join(proj, "lienf"))
	m = status(t, h, "GET", "/v1/directories?path="+proj+"/disparu/plus", "", 200)
	if m["path"] != proj || m["parent"] != filepath.Dir(proj) || strings.Join(strs(m["dirs"]), ",") != "a,b,lien" {
		t.Fatalf("/v1/directories : %v", m)
	}
	if m := status(t, h, "GET", "/v1/directories", "", 200); m["path"] != filepath.Dir(proj) {
		t.Fatalf("/v1/directories sans chemin : %v", m)
	}
	if m := status(t, h, "GET", "/v1/directories?path=/", "", 200); m["parent"] != "" || m["path"] != "/" {
		t.Fatalf("/v1/directories racine : %v", m)
	}
	os.MkdirAll(filepath.Join(proj, "fermé"), 0o000)
	defer os.Chmod(filepath.Join(proj, "fermé"), 0o755)
	if os.Getuid() != 0 {
		status(t, h, "GET", "/v1/directories?path="+filepath.Join(proj, "fermé"), "", 403)
	}

	// Lecture d'une conversation et de son détail.
	status(t, h, "GET", "/v1/sessions/..%2Fx/conversation?dir=-p", "", 400)
	status(t, h, "GET", "/v1/info?dir=-p&id="+id2, "", 404)
	if m := status(t, h, "GET", "/v1/sessions/"+id1+"/conversation?dir=-p", "", 200); len(m["msgs"].([]any)) != 2 {
		t.Fatalf("conversation : %v", m)
	}
	if m := status(t, h, "GET", "/v1/sessions/"+id1+"/conversation?dir=-p&tools=1", "", 200); len(m["msgs"].([]any)) != 3 {
		t.Fatalf("conversation avec outils : %v", m)
	}
	if m := status(t, h, "GET", "/v1/info?dir=-p&id="+id1, "", 200); m["branch"] != "main" {
		t.Fatalf("/v1/info : %v", m)
	}

	// Recherche : de 2 à 200 caractères.
	status(t, h, "GET", "/v1/sessions?q=a", "", 400)
	status(t, h, "GET", "/v1/sessions?q="+strings.Repeat("a", 201), "", 400)
	status(t, h, "GET", "/v1/sessions?q=ab", "", 200)
	if m := status(t, h, "GET", "/v1/sessions?q="+strings.Repeat("é", 200), "", 200); len(m["hits"].([]any)) != 0 {
		t.Fatal("200 caractères : la limite compte des caractères, pas des octets")
	}
	if m := status(t, h, "GET", "/v1/sessions?q=caddy", "", 200); len(m["hits"].([]any)) != 1 {
		t.Fatalf("recherche : %v", m)
	}

	// Renommage : requête, titre, session ouverte, sessions ouvertes inconnues, puis succès.
	status(t, h, "PATCH", "/v1/sessions/"+id1+"?dir=-p", "{", 400)
	status(t, h, "PATCH", "/v1/sessions/"+id1+"?dir=-p", `{"title":"  "}`, 400)
	status(t, h, "PATCH", "/v1/sessions/"+id1+"?dir=-p", `{"title":"`+strings.Repeat("é", 201)+`"}`, 400)
	agents(t, id1)
	v1err(t, h, "PATCH", "/v1/sessions/"+id1+"?dir=-p", `{"title":"x"}`, 409, "session ouverte")
	t.Setenv("FAKE_AGENTS", "illisible")
	v1err(t, h, "PATCH", "/v1/sessions/"+id1+"?dir=-p", `{"title":"x"}`, 503, "action refusée")
	t.Setenv("FAKE_AGENTS", "[]")
	v1ok(t, h, "PATCH", "/v1/sessions/"+id1+"?dir=-p", `{"title":"`+strings.Repeat("é", 200)+`"}`, "Renommée")
	v1ok(t, h, "PATCH", "/v1/sessions/"+id1+"?dir=-p", `{"title":" Caddy \n prod "}`, "Renommée : Caddy prod")
	os.Chmod(filepath.Join(cfg, "projects", "-p", id1+".jsonl"), 0o400)
	if os.Getuid() != 0 {
		v1err(t, h, "PATCH", "/v1/sessions/"+id1+"?dir=-p", `{"title":"y"}`, 500, "")
	}
	os.Chmod(filepath.Join(cfg, "projects", "-p", id1+".jsonl"), 0o644)

	// Réassignation.
	status(t, h, "PATCH", "/v1/sessions", `{"items":[]}`, 400)
	status(t, h, "PATCH", "/v1/sessions", `{"items":[{"dir":"-p","id":"`+id2+`"}]}`, 400)
	v1err(t, h, "PATCH", "/v1/sessions", `{"items":[{"dir":"-p","id":"`+id1+`"}],"to":"/nexiste/pas"}`, 409, "n'existe pas")
	v1ok(t, h, "PATCH", "/v1/sessions", `{"items":[{"dir":"-p","id":"`+id1+`"}],"to":"`+filepath.Join(proj, "a")+`"}`, "1 session(s) réassignée(s)")
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
	status(t, h, "DELETE", "/v1/sessions", "", 400)
	t.Setenv("FAKE_AGENTS", "illisible")
	v1err(t, h, "DELETE", "/v1/sessions?item=-p:"+id1, "", 500, "action refusée")
	agents(t, id1)
	v1err(t, h, "DELETE", "/v1/sessions?item=-p:"+id1, "", 409, "Aucune session supprimée ; 1 session ouverte épargnée")
	v1ok(t, h, "DELETE", "/v1/sessions?item=-p:"+id1+"&item=-p:"+id2, "", "1 session supprimée ; 1 session ouverte épargnée")
	if exists(filepath.Join(cfg, "projects", "-p", id2+".jsonl")) || !exists(filepath.Join(cfg, "projects", "-p", id1+".jsonl")) {
		t.Fatal("mauvaise session effacée")
	}
}

func TestHTTPLancements(t *testing.T) {
	h, cfg, proj := web(t)
	put(t, filepath.Join(cfg, "projects", "-q", id2+".jsonl"), `{"cwd":"/nexiste/pas"}`+"\n", old)
	os.MkdirAll(filepath.Join(cfg, "projects", "-vide"), 0o755)

	// Ouverture dans un terminal (gld-matin).
	status(t, h, "PUT", "/v1/sessions/"+id1+"/process?dir=-p", "{", 400)
	status(t, h, "PUT", "/v1/projects/-p/process", `{"args":"--x;rm"}`, 400)
	status(t, h, "PUT", "/v1/projects/-p/process", `{"prompt":"sans session"}`, 400)
	status(t, h, "PUT", "/v1/sessions/"+id3+"/process?dir=-p", `{}`, 404)
	for _, d := range []string{"a/b", `a\b`} {
		status(t, h, "PUT", "/v1/projects/"+url.PathEscape(d)+"/process", `{}`, 400)
	}
	status(t, h, "PUT", "/v1/projects/-vide/process", `{}`, 404)
	v1err(t, h, "PUT", "/v1/projects/-q/process", `{}`, 404, "dossier du projet introuvable : /nexiste/pas")
	v1ok(t, h, "PUT", "/v1/projects/-p/process", `{"args":"--model=opus"}`, "gld-matin --args --model=opus "+proj)
	v1ok(t, h, "PUT", "/v1/sessions/"+id1+"/process?dir=-p", `{"prompt":"continue"}`, "--id "+id1+" --prompt continue "+proj)
	t.Setenv("FAKE_MATIN_RC", "1")
	v1err(t, h, "PUT", "/v1/sessions/"+id1+"/process?dir=-p", `{}`, 500, "gld-matin --id")
	t.Setenv("FAKE_MATIN_RC", "0")

	// Nouvelle session à côté d'une autre.
	status(t, h, "POST", "/v1/sessions", "{", 400)
	v1err(t, h, "POST", "/v1/sessions", `{"dir":"-p","id":"../x","prompt":"p"}`, 400, "session invalide")
	v1err(t, h, "POST", "/v1/sessions", `{"dir":"-p","id":"`+id1+`","prompt":"p","args":"a;b"}`, 400, "option refusée")
	v1err(t, h, "POST", "/v1/sessions", `{"cwd":"relatif","prompt":"p"}`, 400, "dossier invalide")
	v1err(t, h, "POST", "/v1/sessions", `{"cwd":"/n/existe/pas/xyz","prompt":"p"}`, 404, "dossier du projet introuvable")
	v1err(t, h, "POST", "/v1/sessions", `{"dir":"-p","id":"`+id1+`","prompt":" "}`, 400, "prompt vide")
	v1err(t, h, "POST", "/v1/sessions", `{"dir":"-p","id":"`+id1+`","prompt":"`+strings.Repeat("x", 33<<10)+`"}`, 400, "trop long")
	v1err(t, h, "POST", "/v1/sessions", `{"dir":"-q","id":"`+id2+`","prompt":"p"}`, 404, "introuvable")
	agents(t, id1)         // ouverte : n'empêche pas d'en démarrer une autre
	var left time.Duration // le lancement est borné : un terminal qui ne rend pas la main n'immobilise pas la requête
	fm := matin
	matin = func(ctx context.Context, args ...string) (string, error) {
		if d, ok := ctx.Deadline(); ok {
			left = time.Until(d)
		}
		return fm(ctx, args...)
	}
	v1ok(t, h, "POST", "/v1/sessions", `{"dir":"-p","id":"`+id1+`","prompt":"suite"}`, "gld-matin --prompt suite "+proj)
	matin = fm
	if left < 25*time.Second || left > 30*time.Second {
		t.Fatalf("délai du lancement : %v", left)
	}
	t.Setenv("FAKE_AGENTS", "illisible")
	v1ok(t, h, "POST", "/v1/sessions", `{"dir":"-p","id":"`+id1+`","prompt":"suite"}`, "gld-matin")
	t.Setenv("FAKE_AGENTS", "[]")
	// Fusion de mémoire : prompt seul, ou lancé dans une nouvelle session.
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "a.md"), "a", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "b.md"), "b", old)
	keys := `"keys":["m/-p/a.md","m/-p/b.md"]`
	v1err(t, h, "POST", "/v1/memory/merges", "{", 400, "")
	v1err(t, h, "POST", "/v1/memory/merges", `{"keys":["m/-p/a.md"]}`, 400, "")
	if m := v1ok(t, h, "POST", "/v1/memory/merges", `{`+keys+`}`, "Prompt copié"); !strings.Contains(m["prompt"].(string), "b.md") {
		t.Fatal("prompt de fusion :", m)
	}
	v1err(t, h, "POST", "/v1/memory/merges", `{`+keys+`,"launch":true,"args":"a;b"}`, 400, "option refusée")
	v1ok(t, h, "POST", "/v1/memory/merges", `{`+keys+`,"launch":true,"args":"--model=haiku"}`, "gld-matin --args --model=haiku --prompt")
}

func TestHTTPChat(t *testing.T) {
	h, cfg, _ := web(t)
	put(t, filepath.Join(cfg, "projects", "-q", id2+".jsonl"), `{"cwd":"/nexiste/pas"}`+"\n", old)
	for _, c := range [][3]string{
		{id1, "{", "requête invalide"},
		{id1, `{"prompt":"x","args":"a;b"}`, "option refusée"},
		{id1, `{"prompt":" "}`, "message vide"},
		{id1, `{"prompt":"x","mode":"bypassPermissions"}`, "mode d'autorisation inconnu"},
	} {
		v1err(t, h, "POST", "/v1/sessions/"+c[0]+"/chat?dir=-p", c[1], 400, c[2])
	}
	v1err(t, h, "POST", "/v1/sessions/"+id2+"/chat?dir=-q", `{"prompt":"x"}`, 404, "introuvable")
	v1err(t, h, "POST", "/v1/sessions/"+id3+"/chat?dir=-p", `{"prompt":"x"}`, 404, "session invalide")
	v1err(t, h, "POST", "/v1/sessions/..%2Fx/chat?dir=-p", `{"prompt":"x"}`, 400, "id invalide")
	agents(t, id1)
	v1err(t, h, "POST", "/v1/sessions/"+id1+"/chat?dir=-p", `{"prompt":"x"}`, 400, "déjà ouverte")
	t.Setenv("FAKE_AGENTS", "[]")

	// Réponse relayée ; option non permise au chat signalée.
	t.Setenv("FAKE_CHAT", `{"type":"assistant","message":{"content":[{"type":"text","text":"Bonjour"}]}}`+"\n"+`{"type":"result","subtype":"success","total_cost_usd":0.5}`)
	code, _, raw := call(t, h, "POST", "/v1/sessions/"+id1+"/chat?dir=-p", `{"prompt":"salut","args":"--model=opus --add-dir=/x","mode":"plan"}`)
	if code != 200 || strings.Count(raw, "\n") != 3 || !strings.Contains(raw, `"note":"Options de Réglages non transmises au chat : --add-dir=/x`) ||
		!strings.Contains(raw, `"t":"Bonjour"`) || !strings.Contains(raw, `"done":true,"ok":true,"cost":0.5`) {
		t.Fatalf("chat : %d %s", code, raw)
	}
	// Échec sans résultat : l'erreur de claude, sinon celle du processus.
	t.Setenv("FAKE_CHAT", "")
	t.Setenv("FAKE_ERR", "option inconnue")
	t.Setenv("FAKE_RC", "1")
	if _, _, raw := call(t, h, "POST", "/v1/sessions/"+id1+"/chat?dir=-p", `{"prompt":"salut"}`); raw != `{"done":true,"error":"option inconnue"}`+"\n" {
		t.Fatalf("chat en échec : %s", raw)
	}
	t.Setenv("FAKE_ERR", "")
	if _, _, raw := call(t, h, "POST", "/v1/sessions/"+id1+"/chat?dir=-p", `{"prompt":"salut"}`); !strings.Contains(raw, `"error":"exit status 1"`) {
		t.Fatalf("chat en échec muet : %s", raw)
	}
	// Résultat en erreur.
	t.Setenv("FAKE_RC", "0")
	t.Setenv("FAKE_CHAT", `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"trop de tours"}`)
	if _, _, raw := call(t, h, "POST", "/v1/sessions/"+id1+"/chat?dir=-p", `{"prompt":"salut"}`); !strings.Contains(raw, `"error":"error_max_turns trop de tours"`) {
		t.Fatalf("résultat en erreur : %s", raw)
	}
	// Un résultat qui n'est ni en erreur ni un succès (sous-type inconnu) n'est pas présenté comme réussi.
	t.Setenv("FAKE_CHAT", `{"type":"result","subtype":"error_during_execution","is_error":false}`)
	if _, _, raw := call(t, h, "POST", "/v1/sessions/"+id1+"/chat?dir=-p", `{"prompt":"salut"}`); strings.Contains(raw, `"ok":true`) || !strings.Contains(raw, `"error":"error_during_execution"`) {
		t.Fatalf("résultat ambigu présenté comme réussi : %s", raw)
	}

	// Un seul tour à la fois par session ; arrêt.
	v1err(t, h, "DELETE", "/v1/sessions/"+id1+"/chat", "", 404, "rien en cours")
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
	if code, _, _ := call(t, h, "POST", "/v1/sessions/"+id1+"/chat?dir=-p", `{"prompt":"x"}`); code != 409 {
		t.Fatal("deuxième tour simultané :", code)
	}
	v1ok(t, h, "DELETE", "/v1/sessions/"+id1+"/chat", "", "Arrêt demandé")
	if cmd.Wait() == nil {
		t.Fatal("le processus devait être interrompu")
	}
}

func TestHTTPSnippets(t *testing.T) {
	h, cfg, _ := web(t)
	put(t, filepath.Join(cfg, "commands", "revue.md"), "---\ndescription: Revue\n---\nFais une revue", old)
	if m := status(t, h, "GET", "/v1/snippets", "", 200); m["dir"] != filepath.Join(cfg, "commands") || len(m["files"].([]any)) != 1 {
		t.Fatalf("liste : %v", m)
	}
	if m := status(t, h, "GET", "/v1/snippets/u%2Fnouveau.md", "", 200); m["missing"] != true {
		t.Fatalf("snippet absent : %v", m)
	}
	status(t, h, "GET", "/v1/snippets/u%2F..%2Fx.md", "", 404)
	if m := status(t, h, "GET", "/v1/snippets/u%2Frevue.md", "", 200); !strings.Contains(m["text"].(string), "Fais une revue") {
		t.Fatalf("snippet : %v", m)
	}
	v1err(t, h, "PUT", "/v1/snippets/u%2Frevue.md", "{", 400, "")
	v1err(t, h, "PUT", "/v1/snippets/u%2Frevue.md", `{"text":"x","create":true}`, 400, "")
	v1ok(t, h, "PUT", "/v1/snippets/u%2Frevue.md", `{"text":"v2"}`, "Snippet enregistré")
	v1err(t, h, "POST", "/v1/snippets/drafts", "{", 400, "")
	v1err(t, h, "POST", "/v1/snippets/drafts", `{"model":"gpt","text":"x"}`, 500, "")
	t.Setenv("FAKE_DRAFT", `{"result":"`+"```md\\nmis en forme\\n```"+`","total_cost_usd":0.25}`)
	if m := v1ok(t, h, "POST", "/v1/snippets/drafts", `{"model":"haiku","text":"x"}`, "Mise en forme prête"); m["text"] != "mis en forme\n" || m["cost"] != 0.25 {
		t.Fatalf("brouillon : %v", m)
	}
	v1ok(t, h, "PATCH", "/v1/snippets/u%2Frevue.md", `{"to":"u/revue2.md"}`, "Snippet renommé")
	v1err(t, h, "PATCH", "/v1/snippets/u%2Frevue.md", `{"to":"u/x.md"}`, 400, "")
	v1err(t, h, "PATCH", "/v1/snippets/u%2Frevue2.md", `{"to":"p/a/x.md"}`, 400, "")
	v1ok(t, h, "PATCH", "/v1/snippets/u%2Frevue2.md", `{"to":"u/revue.md"}`, "Snippet renommé")
	v1err(t, h, "DELETE", "/v1/snippets?item=u/absent.md", "", 409, "")
	v1err(t, h, "DELETE", "/v1/snippets", "", 400, "")
	v1ok(t, h, "DELETE", "/v1/snippets?item=u/revue.md", "", "Snippet supprimé")

	// Export et import.
	put(t, filepath.Join(cfg, "commands", "a.md"), "A", old)
	status(t, h, "GET", "/v1/snippets/export?scope=../x", "", 400)
	r := httptest.NewRecorder()
	q := httptest.NewRequest("GET", "/v1/snippets/export", nil)
	q.Host = testHost
	h.ServeHTTP(r, q)
	zr, err := zip.NewReader(bytes.NewReader(r.Body.Bytes()), int64(r.Body.Len()))
	if r.Code != 200 || err != nil || len(zr.File) != 1 || !strings.Contains(r.Header().Get("Content-Disposition"), "commandes-personnelles.zip") {
		t.Fatalf("export : %d %v", r.Code, err)
	}
	put(t, filepath.Join(cfg, "projects", "-p", "x"), "", old) // le dossier -p existe : scope valide
	r = httptest.NewRecorder()
	q = httptest.NewRequest("GET", "/v1/snippets/export?scope=-p", nil)
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
		q := httptest.NewRequest("POST", "/v1/snippets/imports", &b)
		q.Host = testHost
		q.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, q)
		return w.Code, w.Body.String()
	}
	if code, _, _ := call(t, h, "POST", "/v1/snippets/imports", "pas du multipart"); code != 400 {
		t.Fatal("import invalide :", code)
	}
	if code, raw := imp("../x", "", nil); code != 400 || !strings.Contains(raw, "scope invalide") {
		t.Fatal(code, raw)
	}
	if code, raw := imp("", "", map[string]string{"a.md": "autre", "b.md": "B"}); code != 200 || !strings.Contains(raw, "1 importé(s), 1 déjà présent(s), laissé(s) tel(s) quel(s) : a.md") {
		t.Fatal(code, raw)
	}
	if code, raw := imp("", "1", map[string]string{"a.md": "remplacé"}); code != 200 || !strings.Contains(raw, `{"message":"1 importé(s)"}`) {
		t.Fatal(code, raw)
	}
	if code, raw := imp("", "", map[string]string{"x.txt": "pas une commande"}); code != 400 || !strings.Contains(raw, `"error"`) {
		t.Fatal(code, raw)
	}
}

func TestHTTPMemoire(t *testing.T) {
	h, cfg, _ := web(t)
	put(t, filepath.Join(cfg, "CLAUDE.md"), "global", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md"), "- [A](a.md) — a\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "a.md"), "entrée a", old)
	if m := status(t, h, "GET", "/v1/memory/files", "", 200); len(m["global"].([]any)) != 1 || len(m["projects"].([]any)) != 1 {
		t.Fatalf("liste : %v", m)
	}
	if m := status(t, h, "GET", "/v1/memory/files/m%2F-p%2Fb.md", "", 200); m["missing"] != true {
		t.Fatal("entrée absente :", m)
	}
	status(t, h, "GET", "/v1/memory/files/g%2Fsettings.json", "", 404)
	if m := status(t, h, "GET", "/v1/memory/files/g%2FCLAUDE.md", "", 200); m["text"] != "global" {
		t.Fatal(m)
	}
	v1err(t, h, "PUT", "/v1/memory/files/m%2F-p%2Fb.md", "{", 400, "")
	v1err(t, h, "PUT", "/v1/memory/files/g%2Fsettings.json", `{"text":"x"}`, 400, "")
	v1ok(t, h, "PUT", "/v1/memory/files/m%2F-p%2Fb.md", `{"text":"---\nname: B\ndescription: bé\n---\nB","index":true}`, "Enregistré")
	if b, _ := os.ReadFile(filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md")); !strings.Contains(string(b), "[B](b.md) — bé") {
		t.Fatalf("index : %s", b)
	}
	v1ok(t, h, "PUT", "/v1/memory/files/m%2F-p%2Fc.md", `{"text":"c"}`, "Enregistré")
	if b, _ := os.ReadFile(filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md")); strings.Contains(string(b), "c.md") {
		t.Fatal("indexé sans index: true")
	}
	if m := v1ok(t, h, "PATCH", "/v1/memory/files/m%2F-p%2Fb.md", `{"name":"bb.md"}`, "Renommé"); m["key"] != "m/-p/bb.md" {
		t.Fatal(m)
	}
	if b, _ := os.ReadFile(filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md")); !strings.Contains(string(b), "[B](bb.md) — bé") || strings.Contains(string(b), "(b.md)") {
		t.Fatalf("index après renommage : %s", b)
	}
	v1err(t, h, "PATCH", "/v1/memory/files/m%2F-p%2Fbb.md", `{"name":"../x.md"}`, 409, "")
	v1err(t, h, "PATCH", "/v1/memory/files/m%2F-p%2Fbb.md", `{"name":"MEMORY.md"}`, 409, "")
	v1ok(t, h, "PATCH", "/v1/memory/files/m%2F-p%2Fbb.md", `{"name":"b.md"}`, "Renommé")
	v1err(t, h, "PATCH", "/v1/memory/files/m%2F-p%2Fa.md", "{", 400, "")
	v1err(t, h, "PATCH", "/v1/memory/files/m%2F-p%2Fa.md", `{"to":"../x"}`, 409, "")
	if m := v1ok(t, h, "PATCH", "/v1/memory/files/m%2F-p%2Fa.md", `{"to":"global"}`, "Déplacé"); m["key"] != "g/rules/a.md" {
		t.Fatal(m)
	}
	v1err(t, h, "POST", "/v1/memory/drafts", "{", 400, "")
	v1err(t, h, "POST", "/v1/memory/drafts", `{"model":"gpt","instruction":"x"}`, 500, "")
	t.Setenv("FAKE_DRAFT", `{"result":"brouillon","total_cost_usd":0.1}`)
	if m := v1ok(t, h, "POST", "/v1/memory/drafts", `{"model":"haiku","instruction":"écris","refs":["m/-p/b.md"]}`, "Brouillon prêt"); m["text"] != "brouillon\n" {
		t.Fatal(m)
	}
	v1err(t, h, "DELETE", "/v1/memory/files", "", 400, "")
	v1err(t, h, "DELETE", "/v1/memory/files?item=m%2F-p%2Fabsent.md", "", 404, "")
	if m := v1ok(t, h, "DELETE", "/v1/memory/files?item=m%2F-p%2Fabsent.md&item=m%2F-p%2Fb.md", "", "Supprimé"); len(m["done"].([]any)) != 1 || len(m["failed"].([]any)) != 1 {
		t.Fatal("rapport partiel", m)
	}

	// Corbeille.
	l := status(t, h, "GET", "/v1/trash/memory", "", 200)["items"].([]any)
	if len(l) != 1 {
		t.Fatalf("corbeille : %v", l)
	}
	id := l[0].(map[string]any)["id"].(string)
	status(t, h, "GET", "/v1/trash/memory/..%2Fx/file", "", 404)
	if m := status(t, h, "GET", "/v1/trash/memory/"+id+"/file", "", 200); !strings.Contains(m["text"].(string), "name: B") {
		t.Fatal(m)
	}
	v1err(t, h, "PATCH", "/v1/trash/memory/..%2Fx", `{"trashed":false}`, 409, "")
	if m := v1ok(t, h, "PATCH", "/v1/trash/memory/"+id, `{"trashed":false}`, "Restauré"); m["key"] != "m/-p/b.md" {
		t.Fatal(m)
	}
	v1ok(t, h, "DELETE", "/v1/memory/files?item=m%2F-p%2Fb.md", "", "Supprimé")
	if code, m, _ := call(t, h, "DELETE", "/v1/trash/memory", ""); code != 400 || m["error"] == nil {
		t.Fatal("corps vide : la corbeille ne se vide jamais implicitement")
	}
	v1err(t, h, "DELETE", "/v1/trash/memory?item=..%2Fx", "", 409, "")
	v1ok(t, h, "DELETE", "/v1/trash/memory?all=1", "", "Corbeille vidée")
	if l := status(t, h, "GET", "/v1/trash/memory", "", 200)["items"].([]any); len(l) != 0 {
		t.Fatal("corbeille non vidée")
	}
}

func TestHTTPOngletsGroupes(t *testing.T) {
	h, _, _ := web(t)
	if m := status(t, h, "GET", "/v1/tabs", "", 200); len(m["saves"].([]any)) != 0 {
		t.Fatal(m)
	}
	v1err(t, h, "PUT", "/v1/tabs", `{"name":"x"}`, 500, "")
	t.Setenv("FAKE_AGENTS", `[{"sessionId":"`+id1+`","kind":"interactive"},{"sessionId":"`+id2+`","kind":"background"},{"sessionId":"../x","kind":"interactive"}]`)
	v1ok(t, h, "PUT", "/v1/tabs", `{"name":"soir"}`, "1 session(s) sauvegardée(s)")
	saves := status(t, h, "GET", "/v1/tabs", "", 200)["saves"].([]any)
	v1err(t, h, "DELETE", "/v1/tabs/jamais", "", 404, "")
	v1ok(t, h, "DELETE", "/v1/tabs/"+url.PathEscape(saves[0].(map[string]any)["at"].(string)), "", "Sauvegarde supprimée")

	status(t, h, "PUT", "/v1/vgroups", "{", 400)
	status(t, h, "PUT", "/v1/vgroups", `{"groups":[]}`, 400)
	v1err(t, h, "PUT", "/v1/vgroups", `{"groups":[{"name":"a"}],"favs":[]}`, 500, "")
	status(t, h, "PUT", "/v1/vgroups", `{"groups":[{"name":"Infra","project":"/p","ids":["`+id1+`"]}],"favs":["`+id1+`"]}`, 204)
	if m := status(t, h, "GET", "/v1/state", "", 200); len(m["vgroups"].([]any)) != 1 || m["favs"].([]any)[0] != id1 {
		t.Fatal(m)
	}

	status(t, h, "PUT", "/v1/vgroups", `{"groups":[],"favs":[],"projects":[{"name":"Site","dirs":["/a","/b/"]}]}`, 204)
	v1err(t, h, "PUT", "/v1/vgroups", `{"groups":[],"favs":[],"projects":[{"name":"Site","dirs":["rel"]}]}`, 500, "")
	v1err(t, h, "PUT", "/v1/vgroups", `{"groups":[],"favs":[],"projects":[{"name":"X","dirs":[]},{"name":"X","dirs":[]}]}`, 500, "")
	status(t, h, "PUT", "/v1/vgroups", `{"groups":[],"favs":[]}`, 204) // sans « projects » : conservés
	m := status(t, h, "GET", "/v1/state", "", 200)["projects"].([]any)
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
	if r := getRaw(h, "/v1/terminal"); !strings.Contains(r.Body.String(), `"enabled":false`) {
		t.Fatalf("term-info : %s", r.Body.String())
	}
	if r := getRaw(h, "/v1/terminal/ws?t=x"); r.Code != 404 {
		t.Fatalf("term-ws actif sans réglage : %d", r.Code)
	}
	req := httptest.NewRequest("POST", "/v1/terminal", strings.NewReader(`{"id":"`+id1+`"}`))
	req.Host = testHost
	req.Header.Set("Origin", "http://"+testHost)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("term-open actif sans réglage : %d %s", rec.Code, rec.Body.String())
	}
	t.Setenv("GLD_WEB_TERMINAL", "1")
	if r := getRaw(h, "/v1/terminal/ws?t=x"); r.Code != 403 || !strings.Contains(r.Body.String(), "forbidden") { // sans Origin
		t.Fatalf("term-ws sans Origin : %d %s", r.Code, r.Body.String())
	}
	// Derrière la passerelle, l'Origin a été vérifié puis retiré : seul le jeton du terminal compte encore.
	childToken = "secret"
	t.Cleanup(func() { childToken = "" })
	req = httptest.NewRequest("GET", "/v1/terminal/ws?t=x", nil)
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
	_, m, _ := call(t, h, "GET", "/v1/state/stamp", "")
	first, _ := m["stamp"].(float64)
	if first == 0 {
		t.Fatal("tampon vide")
	}
	p := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	later := time.Now().Add(time.Hour)
	os.Chtimes(p, later, later)
	_, m, _ = call(t, h, "GET", "/v1/state/stamp", "")
	if second, _ := m["stamp"].(float64); second <= first {
		t.Fatalf("le tampon n'a pas bougé : %v -> %v", first, second)
	}
}

func TestDossiersEnregistres(t *testing.T) {
	h, _, _ := web(t)
	vide := t.TempDir()
	status(t, h, "POST", "/v1/directories", `{"dir":"relatif"}`, 400)
	status(t, h, "DELETE", "/v1/directories?dir=relatif", "", 400)
	v1err(t, h, "POST", "/v1/directories", `{"dir":"`+vide+`/inconnu"}`, 404, "introuvable")
	v1ok(t, h, "POST", "/v1/directories", `{"dir":"`+vide+`"}`, "")
	v1ok(t, h, "POST", "/v1/directories", `{"dir":"`+vide+`/"}`, "") // doublon : un seul exemplaire
	_, m, _ := call(t, h, "GET", "/v1/state", "")
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
	status(t, h, "PUT", "/v1/vgroups", `{"groups":[],"favs":[]}`, 204)
	_, m, _ = call(t, h, "GET", "/v1/state", "")
	if d, _ := m["dirs"].([]any); len(d) != 1 {
		t.Fatalf("dossiers perdus : %v", m["dirs"])
	}
	v1ok(t, h, "DELETE", "/v1/directories?dir="+url.QueryEscape(vide), "", "")
	_, m, _ = call(t, h, "GET", "/v1/state", "")
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
	v1ok(t, h, "DELETE", "/v1/trash/memory?all=1", "", "Corbeille vidée")
	if _, err := os.Stat(sess); err != nil {
		t.Fatal("la corbeille de sessions (droit exec) a été vidée par /trash-purge :", err)
	}
	if _, err := os.Stat(mem); err == nil {
		t.Fatal("la corbeille mémoire n'a pas été vidée")
	}
}

func TestDossiersEnfants(t *testing.T) {
	h, _, _ := web(t)
	racine := t.TempDir()
	for _, n := range []string{"a", "b", ".cache"} {
		if err := os.Mkdir(filepath.Join(racine, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(racine, "f.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	v1ok(t, h, "POST", "/v1/directories", `{"dir":"`+racine+`","children":true}`, "")
	v1ok(t, h, "POST", "/v1/directories", `{"dir":"`+racine+`","children":true}`, "") // rejeu : pas de doublon
	_, m, _ := call(t, h, "GET", "/v1/state", "")
	if d, _ := m["dirs"].([]any); len(d) != 2 || d[0] != filepath.Join(racine, "a") || d[1] != filepath.Join(racine, "b") {
		t.Fatalf("dirs = %v", m["dirs"])
	}
}
