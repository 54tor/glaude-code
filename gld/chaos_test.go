package main

import (
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Injection de pannes : un `claude` qui ne répond plus (mise à jour, invite, verrou). Le serveur reste disponible, ne sait pas
// quelles sessions sont ouvertes, et refuse donc ce qui en dépend au lieu de supprimer une session peut-être ouverte.
func TestPanneClaudeBloque(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("pas de sleep")
	}
	h, cfg, _ := web(t)
	home, _ := os.UserHomeDir()
	// sleep lancé en fils (pas exec) : il garde la sortie ouverte après la mort du shell, comme un petit-enfant de claude
	os.WriteFile(filepath.Join(home, ".local", "bin", "claude"), []byte("#!/bin/sh\n"+sleep+" 30\n"), 0o755)
	old := cmdTimeout
	cmdTimeout = 300 * time.Millisecond
	t.Cleanup(func() { cmdTimeout = old })
	// main_test.go coupe le cache de `claude agents` ; /api doit ici relancer un seul relevé, comme en production.
	oldTTL := agentsTTL
	agentsTTL = 1500 * time.Millisecond
	t.Cleanup(func() { agentsTTL = oldTTL })

	t0 := time.Now()
	_, m, _ := call(t, h, "GET", "/v1/state", "")
	if d := time.Since(t0); d > 4*time.Second {
		t.Fatalf("/v1/state a attendu %v", d)
	}
	if len(m["live"].([]any)) != 0 || len(m["groups"].([]any)) != 1 {
		t.Fatalf("/v1/state : %v", m)
	}
	v1err(t, h, "DELETE", "/v1/sessions?item=-p:"+id1, "", 500, "action refusée")
	if !exists(filepath.Join(cfg, "projects", "-p", id1+".jsonl")) {
		t.Fatal("session supprimée sans savoir si elle était ouverte")
	}
}

// Un serveur tmux figé : les routes interrogées en boucle par l'interface répondent quand même, sans panneau.
func TestPanneTmuxBloque(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("pas de sleep")
	}
	h, _, _ := web(t)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\n"+sleep+" 30\n"), 0o755)
	t.Setenv("PATH", bin)
	old := cmdTimeout
	cmdTimeout = 300 * time.Millisecond
	t.Cleanup(func() { cmdTimeout = old })
	for _, p := range []string{"/v1/trust/pending", "/v1/sessions/send-info"} {
		t0 := time.Now()
		code, _, raw := call(t, h, "GET", p, "")
		if d := time.Since(t0); code != 200 || d > 4*time.Second {
			t.Fatalf("%s : %d en %v %s", p, code, d, raw)
		}
	}
}

// Un git qui ne répond plus (dépôt sur un montage réseau figé) : la liste des remotes revient vide au lieu d'attendre.
func TestPanneGitBloque(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("pas de sleep")
	}
	h, _, proj := web(t)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n"+sleep+" 30\n"), 0o755)
	t.Setenv("PATH", bin)
	old := cmdTimeout
	cmdTimeout = 300 * time.Millisecond
	t.Cleanup(func() { cmdTimeout = old })
	t0 := time.Now()
	_, m, raw := call(t, h, "GET", "/v1/git/remotes?dir="+proj, "")
	if d := time.Since(t0); d > 4*time.Second || len(m["remotes"].([]any)) != 0 {
		t.Fatalf("/v1/git/remotes : %v %s", d, raw)
	}
}

// Écriture impossible (dossier en lecture seule, disque plein) : l'erreur remonte, le fichier d'origine reste intact et
// aucun fichier temporaire ne traîne.
func TestPanneEcritureReglages(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignore les droits")
	}
	_, cfg, _ := web(t)
	p := filepath.Join(cfg, "settings.json")
	orig := "{\n  \"model\": \"opus\"\n}\n"
	os.WriteFile(p, []byte(orig), 0o600)
	os.Chmod(cfg, 0o500)
	t.Cleanup(func() { os.Chmod(cfg, 0o755) })
	if _, _, err := configEdit(CfgEdit{File: "u", Op: "set", Path: []string{"model"}, Value: "sonnet"}); err == nil {
		t.Fatal("écriture dans un dossier en lecture seule acceptée")
	}
	os.Chmod(cfg, 0o755)
	if b, _ := os.ReadFile(p); string(b) != orig {
		t.Fatalf("original modifié : %q", b)
	}
	if left, _ := filepath.Glob(filepath.Join(cfg, ".gld-web-*.tmp")); len(left) != 0 {
		t.Fatalf("temporaires restés : %v", left)
	}
}

// Un client qui n'achève jamais ses en-têtes est déconnecté au bout de readHeaderTimeout.
func TestEnTetesAuCompteGouttes(t *testing.T) {
	old := readHeaderTimeout
	readHeaderTimeout = 200 * time.Millisecond
	t.Cleanup(func() { readHeaderTimeout = old })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	srv := newServer("", http.NotFoundHandler())
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n"))
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Read(make([]byte, 512)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("connexion toujours ouverte : %v", err)
	}
}
