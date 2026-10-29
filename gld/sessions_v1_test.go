package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Un fichier chargé se lit par un segment encodé (%2F) : le mux le décode après le découpage, sans redirection.
func TestSessionLoadedFileEncodedPath(t *testing.T) {
	h, cfg, _ := web(t)
	f := filepath.Join(cfg, "rules-x.md")
	os.WriteFile(f, []byte("# règle"), 0o600)
	os.WriteFile(loadedFile(), []byte(`{"at":1,"session":"`+id1+`","file":"`+f+`"}`+"\n"), 0o600)
	m := status(t, h, "GET", "/v1/sessions/"+id1+"/loaded/files/"+url.PathEscape(f), "", 200)
	if m["text"] != "# règle" {
		t.Fatalf("texte : %v", m)
	}
	v1err(t, h, "GET", "/v1/sessions/"+id1+"/loaded/files/"+url.PathEscape(filepath.Join(cfg, "autre.md")), "", 404, "")
}

// Toute erreur d'une route de la session suit {"error":{"code","message"}} avec le vrai statut.
func TestSessionRoutesUniformErrors(t *testing.T) {
	h, _, _ := web(t)
	for _, c := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/v1/sessions/pas-un-id/live", "", 400},
		{"GET", "/v1/sessions/" + id3 + "/conversation?dir=-p", "", 404},
		{"GET", "/v1/sessions/" + id1 + "/tasks/x/output?dir=-p", "", 404},
		{"POST", "/v1/sessions/" + id1 + "/messages", "{", 400},
		{"POST", "/v1/sessions/" + id1 + "/keystrokes", "{", 400},
		{"DELETE", "/v1/sessions?item=sans-separateur", "", 400},
		{"DELETE", "/v1/sessions?item=-p:" + id3, "", 400},
		{"DELETE", "/v1/sessions/" + id1 + "/conversation?dir=-p", "", 400},
		{"GET", "/v1/sessions/" + id1 + "/conversation/images/x?dir=-p", "", 404},
		{"POST", "/v1/uploads/images", "pas une image", 415},
		{"PATCH", "/v1/trash/sessions?item=" + id1, `{"trashed":true}`, 400},
		{"DELETE", "/v1/trash/sessions", "", 400},
		{"DELETE", "/v1/trash/sessions?all=1&item=" + id1, "", 400},
		{"PATCH", "/v1/sessions/" + id1 + "/queue/x", `{"state":"sending"}`, 400},
	} {
		v1err(t, h, c.method, c.path, c.body, c.status, "")
	}
	many := strings.Repeat("&item=-p:"+id1, itemMax+1)
	v1err(t, h, "DELETE", "/v1/sessions?"+many[1:], "", 400, "")
}
