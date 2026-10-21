package main

// Tests issus des tests de mutation (gremlins) : chacun fige une limite ou une branche qu'aucun autre test ne vérifiait.

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 2 }

func mtimeOf(t *testing.T, p string) float64 {
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return float64(st.ModTime().UnixNano()) / 1e9
}

func TestEnvEtTexte(t *testing.T) {
	t.Setenv("GLD_TEST_X", "v")
	if env("GLD_TEST_X", "d") != "v" || env("GLD_TEST_ABSENT", "d") != "d" {
		t.Fatal("env")
	}
	var l line
	l.Message.Content = []byte(`[{"type":"text","text":"bonjour"},{"type":"text","text":"toi"}]`)
	if got := promptOf(&l); got != "bonjour toi" {
		t.Fatalf("prompt en blocs : %q", got)
	}
	p := filepath.Join(t.TempDir(), "x")
	for _, c := range []string{"a\nb\n", "a\nb"} {
		os.WriteFile(p, []byte(c), 0o644)
		n := 0
		scan(p, func([]byte) { n++ })
		if n != 2 {
			t.Fatalf("scan %q : %d lignes", c, n)
		}
	}
	if toolArg([]byte(`{"file_path":"","pattern":"x"}`)) != "x" || toolArg([]byte(`{"command":"ls"}`)) != "ls" || toolArg([]byte(`[1]`)) != "[1]" {
		t.Fatal("toolArg")
	}
	if flat([]byte(`[{"text":"a"},{"text":"b"}]`)) != "a\nb" || flat([]byte(`"s"`)) != "s" {
		t.Fatal("flat")
	}
	// Une ligne sans message lisible n'émet rien.
	var n int
	relay(strings.NewReader(`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"x"}]}}`+"\n"), func(chatEvent) { n++ })
	if n != 0 {
		t.Fatal("événement vide émis")
	}
	// Extrait : 60 caractères avant, 100 après la longueur cherchée, marqués « … » s'ils sont coupés.
	_, s := snippet(strings.Repeat("x", 100)+"cible"+strings.Repeat("y", 200), "cible")
	if s != "…"+strings.Repeat("x", 60)+"cible"+strings.Repeat("y", 100)+"…" {
		t.Fatalf("extrait : %q", s)
	}
}

func TestMemeDossier(t *testing.T) {
	d := t.TempDir()
	lien := filepath.Join(t.TempDir(), "lien")
	os.Symlink(d, lien)
	if !sameDir(d, lien) || sameDir(d, filepath.Join(d, "absent")) || sameDir(filepath.Join(d, "absent"), d) {
		t.Fatal("sameDir")
	}
	cfg := t.TempDir()
	use(cfg)
	put(t, filepath.Join(cfg, "CLAUDE.md"), "g", old)
	ailleurs := filepath.Join(t.TempDir(), "CLAUDE.md")
	os.Symlink(filepath.Join(cfg, "CLAUDE.md"), ailleurs)
	autre := filepath.Join(t.TempDir(), "CLAUDE.md")
	put(t, autre, "g", old)
	if !globalClaude(ailleurs) || globalClaude(autre) || globalClaude(filepath.Join(cfg, "absent", "CLAUDE.md")) {
		t.Fatal("globalClaude")
	}
}
