package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchIndex(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	p1 := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	p2 := filepath.Join(cfg, "projects", "-p", id2+".jsonl")
	put(t, p1, `{"type":"user","cwd":"/p","message":{"content":"Déploie Caddy en production"}}`+"\n"+
		`{"type":"assistant","message":{"content":[{"type":"text","text":"caddy redémarré"},{"type":"tool_use","name":"Bash","input":{"command":"caddy reload"}}]}}`, old)
	put(t, p2, `{"type":"user","cwd":"/p","message":{"content":"autre sujet: nginx"}}`, old)

	hits := func(q string) []Hit {
		h, err := searchIndex(q)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	// Casse, accents et préfixe ignorés ; N = messages qui correspondent ; extrait balisé.
	if h := hits("DEPLOIE prod"); len(h) != 1 || h[0].ID != id1 || h[0].N != 1 || h[0].Snip != "\x02Déploie\x03 Caddy en \x02production\x03" {
		t.Fatalf("hits = %+v", h)
	}
	if h := hits("caddy"); len(h) != 1 || h[0].N != 2 {
		t.Fatalf("caddy = %+v", h)
	}
	if len(hits("reload")) != 0 {
		t.Fatal("les entrées d'outils ne doivent pas être indexées")
	}
	if len(hits(`"caddy en"`)) != 1 || len(hits(`"en caddy"`)) != 0 {
		t.Fatal("phrase exacte")
	}
	if len(hits(`", ) AND OR *`)) != 0 {
		t.Fatal("syntaxe FTS non neutralisée")
	}
	// Transcript modifié puis supprimé : l'index suit.
	put(t, p2, `{"type":"user","cwd":"/p","message":{"content":"désormais caddy"}}`, 0)
	if len(hits("caddy")) != 2 {
		t.Fatal("transcript modifié non relu")
	}
	os.Remove(p1)
	if h := hits("caddy"); len(h) != 1 || h[0].ID != id2 {
		t.Fatalf("transcript supprimé : %+v", h)
	}
}

func TestFtsQuery(t *testing.T) {
	for in, want := range map[string]string{
		"":        "",
		"a b":     `"a" "b"*`,
		`"a b" c`: `"a b" "c"*`,
		`c "a b"`: `"c" "a b"`,
		`x"y`:     `"xy"*`,
	} {
		if got := ftsQuery(in); got != want {
			t.Errorf("ftsQuery(%q) = %q, attendu %q", in, got, want)
		}
	}
}
