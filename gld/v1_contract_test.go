package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Pages et jetons d'entrée qui restent hors /v1 (navigateur, PWA, liens partagés).
var pageRoutes = map[string]bool{
	"GET /{$}": true, "GET /accueil": true, "GET /toutes": true, "GET /en-cours": true, "GET /dossier/": true, "GET /groupe/": true, "GET /projet/": true,
	"GET /snippets": true, "GET /snippets/": true, "GET /memoire": true, "GET /memoire/": true, "GET /onglets": true,
	"GET /configuration": true, "GET /configuration/": true, "GET /skills": true, "GET /skills/": true, "GET /extensions": true, "GET /plugins": true, "GET /sauvegarde": true,
	"GET /equipes": true, "GET /equipes/": true, "GET /revues": true, "GET /espace/": true, "GET /reglages/": true,
	"GET /assets/": true, "GET /icons/": true, "GET /favicon.ico": true, "GET /manifest.webmanifest": true, "GET /sw.js": true,
	"GET /guest": true, "GET /guest/{handle}": true, "GET /invite/{home}/{token}": true, "POST /invite/{home}/{token}": true,
	"GET /pair": true, "POST /pair": true,
}

func TestRoutesAreV1OrPages(t *testing.T) {
	for pat := range routeRights {
		_, path, _ := strings.Cut(pat, " ")
		if strings.HasPrefix(path, "/v1/") {
			continue
		}
		if !pageRoutes[pat] {
			t.Errorf("%q : route hors /v1 absente de la liste des pages", pat)
		}
	}
	for pat := range pageRoutes {
		if _, ok := routeRights[pat]; !ok {
			t.Errorf("pageRoutes : %q n'existe pas dans routeRights", pat)
		}
	}
}

func TestV1ErrorsAreUniformJSON(t *testing.T) {
	h, _, _ := web(t)
	for _, c := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/v1/sessions/pas-un-id/conversation", "", 400},
		{"DELETE", "/v1/sessions", "", 400},
		{"POST", "/v1/sessions/" + id1 + "/messages", "pas du json", 400},
		{"GET", "/v1/sessions/00000000-0000-0000-0000-000000000000/conversation", "", 404},
		{"PUT", "/v1/keepwarm", `{"dir":"-p","id":"` + id1 + `","on":true}`, 409},
	} {
		code, m, raw := call(t, h, c.method, c.path, c.body)
		e, _ := m["error"].(map[string]any)
		if code != c.status || e == nil || e["code"] == "" || e["message"] == "" {
			t.Errorf("%s %s : %d au lieu de %d, corps %s", c.method, c.path, code, c.status, cut(raw, 200))
		}
		if _, has := m["ok"]; has {
			t.Errorf("%s %s : enveloppe ok résiduelle", c.method, c.path)
		}
	}
	r := httptest.NewRequest("GET", "/v1/state", nil)
	r.Host = "evil.example"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	if e, _ := m["error"].(map[string]any); w.Code != 403 || e == nil || e["code"] != "forbidden" {
		t.Errorf("hôte refusé : %d %s", w.Code, w.Body)
	}
}

// Un handler de /v1 répond par writeErr ; http.Error (texte brut) n'est permis que pour les pages et les jetons d'entrée.
func TestNoHTTPErrorInV1Handlers(t *testing.T) {
	allowed := map[string]bool{
		"listen.go": true, "pair.go": true, "token_page.go": true, "http_util.go": true, // pages, jeton, forbid() (JSON sur /v1)
	}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if s, ok := c.Fun.(*ast.SelectorExpr); ok {
					if p, ok := s.X.(*ast.Ident); ok && p.Name == "http" && s.Sel.Name == "Error" {
						t.Errorf("%s : http.Error interdit, utiliser writeErr", fset.Position(c.Pos()))
					}
				}
			}
			return true
		})
	}
}

// docs/reference/openapi.yaml décrit exactement les routes /v1 enregistrées, avec leur droit.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	b, err := os.ReadFile("../docs/reference/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `yaml:"operationId"`
			Right       string `yaml:"x-gld-right"`
		} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	seen, ids := map[string]bool{}, map[string]bool{}
	for p, ms := range doc.Paths {
		for m, op := range ms {
			pat := strings.ToUpper(m) + " " + p
			seen[pat] = true
			if ids[op.OperationID] || op.OperationID == "" {
				t.Errorf("%s : operationId %q vide ou en double", pat, op.OperationID)
			}
			ids[op.OperationID] = true
			if want, ok := routeRights[pat]; !ok {
				t.Errorf("openapi : %q n'existe pas dans routeRights", pat)
			} else if string(want) != op.Right {
				t.Errorf("openapi : %q droit %q, routeRights %q", pat, op.Right, want)
			}
		}
	}
	for pat := range routeRights {
		if _, path, _ := strings.Cut(pat, " "); strings.HasPrefix(path, "/v1/") && !seen[pat] {
			t.Errorf("openapi : %q manque", pat)
		}
	}
}
