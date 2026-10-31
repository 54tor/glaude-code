package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secRig : un home principal et un home « work » dont le dossier parent est le cwd d'un projet du principal.
func secRig(t *testing.T) (cfg, workCfg string) {
	t.Helper()
	base := t.TempDir()
	cfg = filepath.Join(base, "alice", ".claude")
	workCfg = filepath.Join(base, "bob", ".claude")
	use(cfg)
	put(t, filepath.Join(workCfg, "CLAUDE.md"), "secret", old)
	put(t, filepath.Join(workCfg, "rules", "r.md"), "rule", old)
	put(t, filepath.Join(cfg, "gld-web-homes.json"), `{"homes":[{"name":"work","dir":"`+workCfg+`"}]}`, old)
	// un projet du principal lancé depuis le parent du home work
	put(t, filepath.Join(cfg, "projects", "-bob", "s.jsonl"), `{"cwd":"`+filepath.Dir(workCfg)+`"}`+"\n", old)
	return cfg, workCfg
}

func TestMemResolveUnknownDirRefused(t *testing.T) {
	cfg, _ := secRig(t)
	other := filepath.Join(filepath.Dir(cfg), "other")
	put(t, filepath.Join(other, "CLAUDE.md"), "x", old)
	enc := nonAlnum.ReplaceAllString(other, "-") // dossier de transcripts absent de projects/
	for _, key := range []string{"p/" + enc + "/CLAUDE.md", "r/" + enc + "/x.md"} {
		if _, err := memRead(key); err == nil {
			t.Errorf("%s résolu hors des projets connus", key)
		}
		if memSave(key, "pwn") == nil {
			t.Errorf("%s écrit hors des projets connus", key)
		}
	}
	if _, _, _, err := skillBase("p/" + enc + "/x"); err == nil {
		t.Error("skillBase accepte un dossier inconnu")
	}
	if _, _, err := cmdBase("p/" + enc + "/x.md"); err == nil {
		t.Error("cmdBase accepte un dossier inconnu")
	}
	if data, err := os.ReadFile(filepath.Join(other, "CLAUDE.md")); err != nil || string(data) != "x" {
		t.Fatal("fichier modifié", err)
	}
}

func TestMemResolveRefusesOtherHomeConfig(t *testing.T) {
	_, workCfg := secRig(t)
	for _, key := range []string{"p/-bob/.claude/CLAUDE.md", "r/-bob/r.md", "r/-bob/new.md"} {
		if _, err := memRead(key); err == nil {
			t.Errorf("%s lisible", key)
		}
		if memSave(key, "pwn") == nil {
			t.Errorf("%s inscriptible", key)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(workCfg, "CLAUDE.md")); string(data) != "secret" {
		t.Fatal("CLAUDE.md d'un autre home modifié")
	}
	if exists(filepath.Join(workCfg, "rules", "new.md")) {
		t.Fatal("règle créée chez un autre home")
	}
}

func TestGlobBraceBomb(t *testing.T) {
	bomb := strings.Repeat("{*,*}", 40) + "Z"
	if _, err := setPaths("x", []string{bomb}); err == nil {
		t.Error("setPaths accepte une bombe d'accolades")
	}
	if _, err := setPaths("x", []string{strings.Repeat("a", 201)}); err == nil {
		t.Error("setPaths accepte un motif trop long")
	}
	lookup, _ := parseMeta("---\npaths:\n  - \"" + bomb + "\"\n---\n")
	if _, err := globsOf(lookup); err == nil {
		t.Error("globsOf accepte une bombe d'accolades")
	}
	if !globOK("src/**/*.{ts,tsx}") {
		t.Error("motif courant refusé")
	}
}

func TestMemMatchesUnknownDirRefused(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "rules", "ts.md"), "---\npaths:\n  - \"**/*\"\n---\nr\n", old)
	home := t.TempDir()
	put(t, filepath.Join(home, "f.txt"), "x", old)
	if _, _, _, err := memMatches("g/rules/ts.md", nonAlnum.ReplaceAllString(home, "-")); err == nil {
		t.Error("dossier hors projets connus accepté")
	}
	if _, _, _, err := memMatches("g/rules/ts.md", "-absent"); err == nil {
		t.Error("projet absent accepté")
	}
}

func TestMatchesNamesNeedFsRead(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	proj := t.TempDir()
	put(t, filepath.Join(proj, "a.ts"), "x", old)
	put(t, filepath.Join(cfg, "rules", "ts.md"), "---\npaths:\n  - \"*.ts\"\n---\nr\n", old)
	put(t, filepath.Join(cfg, "projects", "-p", "s.jsonl"), `{"cwd":"`+proj+`"}`+"\n", old)
	serveMux := http.NewServeMux()
	routesMemory(serveMux)
	call := func(localGateInfo *gateInfo) string {
		request := httptest.NewRequest("GET", "/v1/memory/matches?key=g/rules/ts.md&dir=-p", nil)
		if localGateInfo != nil {
			request = request.WithContext(context.WithValue(request.Context(), gateKey{}, localGateInfo))
		}
		recorder := httptest.NewRecorder()
		serveMux.ServeHTTP(recorder, request)
		return recorder.Body.String()
	}
	if body := call(nil); !strings.Contains(body, "a.ts") {
		t.Errorf("sans garde : %s", body)
	}
	authStore := testStore(t)
	id, err := authStore.AddUser("alice", testPw, false)
	if err != nil {
		t.Fatal(err)
	}
	authStore.Grant(id, mainHomeID, RightMemRead)
	anotherGateInfo := &gateInfo{p: &Principal{User: &User{ID: id}}, s: authStore}
	if body := call(anotherGateInfo); strings.Contains(body, "a.ts") || !strings.Contains(body, `"total":1`) {
		t.Errorf("sans fs_read : %s", body)
	}
	authStore.Grant(id, mainHomeID, RightFsRead)
	if body := call(anotherGateInfo); !strings.Contains(body, "a.ts") {
		t.Errorf("avec fs_read : %s", body)
	}
}
