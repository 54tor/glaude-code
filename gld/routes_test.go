package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Les routes de git ne répondent que pour un dossier de sessions ; un push n'accepte qu'un remote du dépôt.
func TestHTTPGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	gitBin, _ := exec.LookPath("git")
	h, _, proj := web(t)
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+filepath.Dir(gitBin))
	bare := filepath.Join(t.TempDir(), "r.git")
	git(t, proj, "init", "-q", "--bare", bare)
	git(t, proj, "init", "-q", "-b", "main", ".")
	git(t, proj, "remote", "add", "origin", bare)
	git(t, proj, "commit", "-q", "--allow-empty", "-m", "un")
	os.WriteFile(filepath.Join(proj, "neuf.txt"), []byte("x"), 0o644)

	etranger := t.TempDir()
	git(t, etranger, "init", "-q")
	os.WriteFile(filepath.Join(etranger, "secret.txt"), []byte("x"), 0o644)
	if _, m, _ := call(t, h, "GET", "/v1/git/remotes?dir="+etranger, ""); len(m["remotes"].([]any)) != 0 {
		t.Fatalf("remotes d'un dossier inconnu : %v", m)
	}
	if _, m, _ := call(t, h, "GET", "/v1/git/changes?dir="+etranger, ""); len(m["files"].([]any)) != 0 {
		t.Fatalf("changements d'un dossier inconnu : %v", m)
	}
	_, m, _ := call(t, h, "GET", "/v1/git/remotes?dir="+proj, "")
	if rs := m["remotes"].([]any); len(rs) != 1 || rs[0].(map[string]any)["name"] != "origin" {
		t.Fatalf("remotes : %v", m)
	}
	_, m, raw := call(t, h, "GET", "/v1/git/changes?dir="+proj, "")
	if fs := m["files"].([]any); len(fs) != 1 || !strings.Contains(raw, "neuf.txt") {
		t.Fatalf("changements : %s", raw)
	}
	if _, m, _ := call(t, h, "GET", "/v1/git/status", ""); m["status"] == nil {
		t.Fatalf("git-status : %v", m)
	}

	status(t, h, "POST", "/v1/git/pushes", `pas du json`, 400)
	status(t, h, "POST", "/v1/git/pushes", `{"dir":"`+etranger+`","remote":"origin"}`, 400)
	v1err(t, h, "POST", "/v1/git/pushes", `{"dir":"`+proj+`","remote":"--force"}`, 502, "remote inconnu")
	v1ok(t, h, "POST", "/v1/git/pushes", `{"dir":"`+proj+`","remote":"origin"}`, "")
	if git(t, bare, "rev-parse", "main") != git(t, proj, "rev-parse", "HEAD") {
		t.Fatal("rien poussé")
	}
}

// La réponse à la question de confiance ne part qu'aux panneaux du dossier demandé : 1 pour oui, 2 pour non.
func TestHTTPTrust(t *testing.T) {
	h, _, _ := web(t)
	old := tmuxRun
	t.Cleanup(func() { tmuxRun = old })
	var sent []string
	down := map[string]bool{}
	tmuxRun = func(_ string, a ...string) (string, error) {
		switch a[0] {
		case "list-panes":
			return "%1\tclaude\t/home/u/neuf\n%2\tclaude\t/home/u/autre\n", nil
		case "capture-pane":
			if down[a[len(a)-1]] { // le curseur a bougé d'un cran
				return "Is this a project you created or one you trust?\n   No, exit\n ❯ Yes, I trust this folder\n", nil
			}
			return "Is this a project you created or one you trust?\n ❯ No, exit\n   Yes, I trust this folder\n", nil
		case "send-keys":
			sent = append(sent, a[2]+"="+a[3])
			down[a[2]] = down[a[2]] || a[3] == "Down"
		}
		return "", nil
	}
	_, m, _ := call(t, h, "GET", "/v1/trust/pending", "")
	if d := strs(m["dirs"]); len(d) != 2 {
		t.Fatalf("en attente : %v", m)
	}
	status(t, h, "POST", "/v1/trust/answers", `{"dir":"relatif","yes":true}`, 400)
	status(t, h, "POST", "/v1/trust/answers", `{`, 400)
	v1err(t, h, "POST", "/v1/trust/answers", `{"dir":"/home/u/ailleurs","yes":true}`, 409, "plus aucune question")
	v1ok(t, h, "POST", "/v1/trust/answers", `{"dir":"/home/u/neuf/","yes":true}`, "")
	v1ok(t, h, "POST", "/v1/trust/answers", `{"dir":"/home/u/autre","yes":false}`, "")
	// oui : curseur sur « No, exit » → Bas, puis Entrée une fois le curseur revu sur « Yes » ; non : le curseur y est déjà → Entrée
	if strings.Join(sent, " ") != "%1=Down %1=Enter %2=Enter" {
		t.Fatalf("touches envoyées : %v", sent)
	}
}

// Une valeur écrite dans les réglages (souvent un secret, dans env) n'apparaît jamais dans le journal.
func TestHTTPConfigJournalSansValeur(t *testing.T) {
	h, cfg, _ := web(t)
	const secret = "sk-ant-SECRET-0123456789"
	status(t, h, "POST", "/v1/config/edits", `{"file":"u","op":"set","path":[]}`, 400)
	status(t, h, "POST", "/v1/config/edits", `{`, 400)
	v1ok(t, h, "POST", "/v1/config/edits", `{"file":"u","op":"set","path":["env","CLE"],"value":"`+secret+`","dry":true}`, "Aperçu")
	if b, _ := os.ReadFile(filepath.Join(cfg, "settings.json")); strings.Contains(string(b), secret) {
		t.Fatal("aperçu écrit sur disque")
	}
	v1ok(t, h, "POST", "/v1/config/edits", `{"file":"u","op":"set","path":["env","CLE"],"value":"`+secret+`"}`, "Enregistré")
	v1ok(t, h, "POST", "/v1/config/edits", `{"file":"u","op":"set","path":["env","CLE"],"value":"`+secret+`"}`, "Aucun changement")
	v1err(t, h, "POST", "/v1/config/edits", `{"file":"u","op":"set","path":["apiKeyHelper"],"value":"x"}`, 400, "pas permise")
	v1ok(t, h, "PUT", "/v1/config/raw", `{"file":"u","text":"{\"env\":{\"API_KEY\":\"`+secret+`\",\"B\":\"1\"}}"}`, "Enregistré")
	status(t, h, "PUT", "/v1/config/raw", `[`, 400)
	// Relu masqué, puis renvoyé tel quel avec le masque : le secret reste sur disque, jamais dans une réponse.
	_, m, raw := call(t, h, "GET", "/v1/config/raw?file=u", "")
	if strings.Contains(raw, secret) || !strings.Contains(raw, "••••••••") {
		t.Fatalf("config-raw : %s", raw)
	}
	_, _, raw = call(t, h, "PUT", "/v1/config/raw", `{"file":"u","text":`+jsonStr(m["text"].(string)+"")+`}`)
	if strings.Contains(raw, secret) {
		t.Fatalf("secret renvoyé : %s", raw)
	}
	if b, _ := os.ReadFile(filepath.Join(cfg, "settings.json")); !strings.Contains(string(b), secret) {
		t.Fatalf("secret perdu à l'aller-retour du masque :\n%s", b)
	}
	if _, _, raw := call(t, h, "GET", "/v1/config/effective", ""); strings.Contains(raw, secret) {
		t.Fatalf("config-effective expose le secret : %s", raw)
	}
	v1err(t, h, "GET", "/v1/config/raw?file=../../etc/passwd", "", 400, "")
	v1ok(t, h, "POST", "/v1/config/permissions/imports", `{"file":"u","text":"{\"permissions\":{\"allow\":[\"Bash(ls)\"]}}","mode":"merge"}`, "Importé")
	status(t, h, "POST", "/v1/config/permissions/imports", `"`, 400)
	v1err(t, h, "POST", "/v1/config/restorations", `{"name":"../settings.json"}`, 400, "")
	status(t, h, "POST", "/v1/config/restorations", `x`, 400)
	status(t, h, "GET", "/v1/config/effective?dir=a/b", "", 400)

	log, err := os.ReadFile(filepath.Join(cfg, "gld-web.log"))
	if err != nil || !strings.Contains(string(log), "config-edit u set [env CLE]") {
		t.Fatalf("journal : %v %s", err, log)
	}
	if strings.Contains(string(log), secret) {
		t.Fatalf("secret journalisé :\n%s", log)
	}
	_, m, raw = call(t, h, "GET", "/v1/config", "")
	for _, k := range []string{"files", "hooks", "issues", "plugins", "projects"} {
		if _, isList := m[k].([]any); !isList {
			t.Fatalf("/v1/config %s n'est pas une liste : %s", k, cut(raw, 300))
		}
	}
}

// Skills par HTTP : création, lecture, copie vers un projet, suppression ; une clé qui sort du dossier est refusée.
func TestHTTPSkills(t *testing.T) {
	h, cfg, _ := web(t)
	status(t, h, "PUT", "/v1/skills/u%2Frevue", `{`, 400)
	v1ok(t, h, "PUT", "/v1/skills/u%2Frevue", `{"text":"---\nname: revue\ndescription: Relit\n---\nx\n","create":true}`, "Enregistré")
	v1err(t, h, "PUT", "/v1/skills/u%2F..%2Fevade", `{"text":"x\n","create":true}`, 400, "")
	if _, err := os.Stat(filepath.Join(cfg, "evade")); err == nil {
		t.Fatal("skill écrit hors du dossier skills")
	}
	_, m, _ := call(t, h, "GET", "/v1/skills/u%2Frevue", "")
	if !strings.Contains(m["text"].(string), "Relit") {
		t.Fatalf("lecture : %v", m)
	}
	v1err(t, h, "GET", "/v1/skills/u%2F..%2F..%2Fx", "", 404, "")
	_, m, raw := call(t, h, "GET", "/v1/skills", "")
	if !strings.Contains(raw, "revue") || m["projects"] == nil {
		t.Fatalf("liste : %s", cut(raw, 300))
	}
	v1err(t, h, "POST", "/v1/skills/copies", `{"key":"u/revue","to":"/nulle/part"}`, 400, "")
	status(t, h, "POST", "/v1/skills/copies", `{`, 400)
	v1ok(t, h, "DELETE", "/v1/skills?item=u/revue", "", "corbeille")
	v1err(t, h, "DELETE", "/v1/skills?item=u/revue", "", 409, "")
	status(t, h, "DELETE", "/v1/skills", "", 400)
	status(t, h, "POST", "/v1/skills/drafts", `{`, 400)
}

// Les corps trop gros sont refusés avant tout appel au modèle.
func TestHTTPPermExplainLimite(t *testing.T) {
	h, _, _ := web(t)
	status(t, h, "POST", "/v1/config/permissions/explanations", `{"kind":"allow","rule":"`+strings.Repeat("x", 5000)+`"}`, 400)
	status(t, h, "POST", "/v1/config/permissions/explanations", `{`, 400)
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Routes d'écriture de la mémoire : chacune n'accepte que les clés de son ressort et modifie le fichier sur disque.
func TestHTTPMemoireEcritures(t *testing.T) {
	h, cfg, _ := web(t)
	regle := filepath.Join(cfg, "rules", "a.md")
	put(t, regle, "# Règle\n\ncorps\n", old)
	entree := filepath.Join(cfg, "projects", "-p", "memory", "x.md")
	put(t, entree, "---\nname: x\ndescription: d\nmetadata:\n  type: user\n---\n\ncorps\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md"), "- [Fantôme](fantome.md) — absent\n", old)
	lire := func(p string) string { b, _ := os.ReadFile(p); return string(b) }

	status(t, h, "PATCH", "/v1/memory/files/m%2F-p%2Fx.md", `{"paths":["src/**"]}`, 400)
	v1ok(t, h, "PATCH", "/v1/memory/files/g%2Frules%2Fa.md", `{"paths":["src/**","*.go"]}`, "Portée")
	if s := lire(regle); !strings.Contains(s, "paths:") || !strings.Contains(s, "src/**") || !strings.Contains(s, "corps") {
		t.Fatalf("portée : %q", s)
	}
	status(t, h, "PATCH", "/v1/memory/files/g%2FCLAUDE.md", `{"type":"user"}`, 400)
	v1ok(t, h, "PATCH", "/v1/memory/files/m%2F-p%2Fx.md", `{"type":"feedback"}`, "Type")
	status(t, h, "PATCH", "/v1/memory/files/m%2F-p%2Fx.md", `{"type":"user","name":"y.md"}`, 400)
	if s := lire(entree); !strings.Contains(s, "type: feedback") || strings.Contains(s, "type: user") {
		t.Fatalf("type : %q", s)
	}
	status(t, h, "POST", "/v1/memory/fixes", `{"key":"p/-p/CLAUDE.md"}`, 400)
	v1err(t, h, "POST", "/v1/memory/fixes", `{"key":"m/-p/absent.md"}`, 400, "")

	m := v1ok(t, h, "PATCH", "/v1/memory/files/g%2Frules%2Fa.md", `{"enabled":false}`, "désactivée")
	if _, err := os.Stat(regle + ".off"); err != nil || m["key"] != "g/rules/a.md.off" {
		t.Fatalf("désactivation : %v %v", err, m)
	}
	v1ok(t, h, "PATCH", "/v1/memory/files/g%2Frules%2Fa.md.off", `{"enabled":true}`, "réactivée")
	v1ok(t, h, "PATCH", "/v1/memory/files/g%2Frules%2Fa.md", `{"enabled":true}`, "réactivée")
	if _, err := os.Stat(regle); err != nil {
		t.Fatal("règle non réactivée")
	}
	v1err(t, h, "PATCH", "/v1/memory/files/g%2FCLAUDE.md", `{"enabled":false}`, 409, "")

	v1ok(t, h, "POST", "/v1/memory/repairs", `{"dir":"-p"}`, "1 ligne(s) retirée(s), 1 ajoutée(s)")
	if s := lire(filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md")); strings.Contains(s, "fantome") || !strings.Contains(s, "x.md") {
		t.Fatalf("index réparé : %q", s)
	}
	v1err(t, h, "POST", "/v1/memory/repairs", `{"dir":"../x"}`, 400, "")
	for _, p := range [][2]string{{"PATCH", "/v1/memory/files/g%2Frules%2Fa.md"}, {"POST", "/v1/memory/fixes"}, {"POST", "/v1/memory/repairs"}, {"POST", "/v1/memory/lints"}, {"POST", "/v1/memory/token-counts"}} {
		status(t, h, p[0], p[1], `{`, 400)
	}
	status(t, h, "POST", "/v1/memory/token-counts", `{"keys":["a","b","c","d","e","f","g"]}`, 400)
	status(t, h, "POST", "/v1/memory/lints", `{"scope":""}`, 400)
	status(t, h, "GET", "/v1/sessions/..%2Fx/loaded", "", 400)
	status(t, h, "GET", "/v1/sessions/..%2Fx/loaded/files/%2Fetc%2Fpasswd", "", 400)
	status(t, h, "GET", "/v1/sessions/11111111-1111-1111-1111-111111111111/loaded/files/%2Fetc%2Fpasswd", "", 404) // chemin jamais chargé par la session
	if _, m, _ := call(t, h, "GET", "/v1/memory/health", ""); m["issues"] == nil {
		t.Fatalf("santé : %v", m)
	}
	if _, m, _ := call(t, h, "GET", "/v1/memory/files?q=corps", ""); m["hits"].(map[string]any)["m/-p/x.md"] == nil {
		t.Fatalf("recherche : %v", m)
	}
}

// Plugins et marketplaces : une opération par méthode ; les entrées invalides sont refusées avant tout appel au CLI.
func TestHTTPPluginsMarketplaces(t *testing.T) {
	h, _, _ := web(t)
	v1err(t, h, "PUT", "/v1/plugins/bad%20id", `{"scope":"user"}`, 422, "")
	v1err(t, h, "PUT", "/v1/plugins/a@b", `{`, 400, "")
	v1err(t, h, "PATCH", "/v1/plugins/a@b", `{"scope":"user"}`, 400, "")
	v1err(t, h, "DELETE", "/v1/plugins/a@b?scope=nope", "", 422, "")
	v1err(t, h, "POST", "/v1/plugins/a@b/updates", `{"scope":"project"}`, 422, "")
	v1err(t, h, "POST", "/v1/marketplaces", `{"source":"/etc"}`, 422, "")
	v1err(t, h, "POST", "/v1/marketplaces", `{`, 400, "")
	v1err(t, h, "DELETE", "/v1/marketplaces/bad%20name", "", 422, "")
	v1err(t, h, "POST", "/v1/marketplaces/bad%20name/updates", "", 422, "")
}
