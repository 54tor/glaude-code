package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSearchIndex(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	p1 := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	p2 := filepath.Join(cfg, "projects", "-p", id2+".jsonl")
	put(t, p1, `{"type":"user","cwd":"/p","message":{"content":"Déploie Caddy en production"}}`+"\n"+
		`{"type":"assistant","message":{"content":[{"type":"text","text":"caddy redémarré"},{"type":"tool_use","name":"Bash","input":{"command":"caddy reload"}}]}}`, old)
	put(t, p2, `{"type":"user","cwd":"/p","message":{"content":"autre sujet: nginx"}}`, old)

	hits := func(query string) []Hit {
		hits, err := searchIndex(query)
		if err != nil {
			t.Fatal(err)
		}
		return hits
	}
	// Casse, accents et préfixe ignorés ; N = messages qui correspondent ; extrait balisé.
	if localHits := hits("DEPLOIE prod"); len(localHits) != 1 || localHits[0].ID != id1 || localHits[0].N != 1 || localHits[0].Snip != "\x02Déploie\x03 Caddy en \x02production\x03" {
		t.Fatalf("hits = %+v", localHits)
	}
	if localHits := hits("caddy"); len(localHits) != 1 || localHits[0].N != 2 {
		t.Fatalf("caddy = %+v", localHits)
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
	put(t, p2, `{"type":"user","cwd":"/p","message":{"content":"désormais caddy"}}`, 2*indexSettle)
	if len(hits("caddy")) != 2 {
		t.Fatal("transcript modifié non relu")
	}
	os.Remove(p1)
	if localHits := hits("caddy"); len(localHits) != 1 || localHits[0].ID != id2 {
		t.Fatalf("transcript supprimé : %+v", localHits)
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

func TestSearchIndexSettle(t *testing.T) {
	cfg := t.TempDir()
	use(cfg)
	now := time.Now()
	idxNow = func() time.Time { return now }
	t.Cleanup(func() { idxNow = time.Now })
	path := filepath.Join(cfg, "projects", "-p", id1+".jsonl")
	msg := func(word string) string { return `{"type":"user","cwd":"/p","message":{"content":"` + word + `"}}` }
	hits := func(query string) int {
		hits, err := searchIndex(query)
		if err != nil {
			t.Fatal(err)
		}
		return len(hits)
	}
	// Première indexation : immédiate, même pour un transcript tout frais.
	put(t, path, msg("alpha"), 0)
	if hits("alpha") != 1 {
		t.Fatal("un transcript inconnu doit être indexé tout de suite")
	}
	// Déjà indexé, puis modifié à l'instant : pas relu.
	put(t, path, msg("bravo"), 0)
	if hits("bravo") != 0 || hits("alpha") != 1 {
		t.Fatal("un transcript modifié depuis moins de indexSettle ne doit pas être relu")
	}
	// Une fois le délai écoulé : relu.
	now = now.Add(indexSettle + time.Second)
	if hits("bravo") != 1 || hits("alpha") != 0 {
		t.Fatal("un transcript stabilisé doit être relu")
	}
}

func TestSearchIndexPerHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	prev := idxFile
	t.Cleanup(func() { idxFile = prev })
	argument, body := t.TempDir(), t.TempDir()
	if idxFileFor(filepath.Join(argument, "projects")) == idxFileFor(filepath.Join(body, "projects")) {
		t.Fatal("deux homes partagent le même fichier d'index")
	}
	use(argument)
	idxFile = func() string { return idxFileFor(root) }
	put(t, filepath.Join(argument, "projects", "-p", id1+".jsonl"), `{"type":"user","cwd":"/p","message":{"content":"secretdehomea"}}`, old)
	if hits, err := searchIndex("secretdehomea"); err != nil || len(hits) != 1 {
		t.Fatalf("home A : %v %v", hits, err)
	}
	use(body)
	idxFile = func() string { return idxFileFor(root) }
	if hits, err := searchIndex("secretdehomea"); err != nil || len(hits) != 0 {
		t.Fatalf("la recherche du home B voit le home A : %v %v", hits, err)
	}
}
