package main

import (
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

	t0 := time.Now()
	_, m, _ := call(t, h, "GET", "/api", "")
	if d := time.Since(t0); d > 4*time.Second {
		t.Fatalf("/api a attendu %v", d)
	}
	if len(m["live"].([]any)) != 0 || len(m["groups"].([]any)) != 1 {
		t.Fatalf("/api : %v", m)
	}
	ok(t, h, "/rm", `{"items":[{"dir":"-p","id":"`+id1+`"}]}`, false, "action refusée")
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
	for _, p := range []string{"/trust-pending", "/session-send-info"} {
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
	_, m, raw := call(t, h, "GET", "/git-remotes?dir="+proj, "")
	if d := time.Since(t0); d > 4*time.Second || len(m["remotes"].([]any)) != 0 {
		t.Fatalf("/git-remotes : %v %s", d, raw)
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
