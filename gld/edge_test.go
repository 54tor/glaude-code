package main

// Tests issus des tests de mutation (gremlins) : chacun fige une limite ou une branche qu'aucun autre test ne vérifiait.

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func near(ratio, localRatio float64) bool { return math.Abs(ratio-localRatio) < 2 }

func mtimeOf(t *testing.T, path string) float64 {
	st, err := os.Stat(path)
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
	var localLine line
	localLine.Message.Content = []byte(`[{"type":"text","text":"bonjour"},{"type":"text","text":"toi"}]`)
	if got := promptOf(&localLine); got != "bonjour toi" {
		t.Fatalf("prompt en blocs : %q", got)
	}
	path := filepath.Join(t.TempDir(), "x")
	for _, content := range []string{"a\nb\n", "a\nb"} {
		os.WriteFile(path, []byte(content), 0o644)
		count := 0
		scan(path, func([]byte) { count++ })
		if count != 2 {
			t.Fatalf("scan %q : %d lignes", content, count)
		}
	}
	if toolArg([]byte(`{"file_path":"","pattern":"x"}`)) != "x" || toolArg([]byte(`{"command":"ls"}`)) != "ls" || toolArg([]byte(`[1]`)) != "[1]" {
		t.Fatal("toolArg")
	}
	if flat([]byte(`[{"text":"a"},{"text":"b"}]`)) != "a\nb" || flat([]byte(`"s"`)) != "s" {
		t.Fatal("flat")
	}
	// Une ligne sans message lisible n'émet rien.
	var localCount int
	relay(strings.NewReader(`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"x"}]}}`+"\n"), func(chatEvent) { localCount++ })
	if localCount != 0 {
		t.Fatal("événement vide émis")
	}
	// Extrait : 60 caractères avant, 100 après la longueur cherchée, marqués « … » s'ils sont coupés.
	_, text := snippet(strings.Repeat("x", 100)+"cible"+strings.Repeat("y", 200), "cible")
	if text != "…"+strings.Repeat("x", 60)+"cible"+strings.Repeat("y", 100)+"…" {
		t.Fatalf("extrait : %q", text)
	}
}

func TestMemeDossier(t *testing.T) {
	dir := t.TempDir()
	lien := filepath.Join(t.TempDir(), "lien")
	os.Symlink(dir, lien)
	if !sameDir(dir, lien) || sameDir(dir, filepath.Join(dir, "absent")) || sameDir(filepath.Join(dir, "absent"), dir) {
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

func TestRelayInformationalWarning(t *testing.T) {
	var evs []chatEvent
	relay(strings.NewReader(`{"type":"system","subtype":"informational","level":"warning","content":"safeguards flagged this session"}`+"\n"+`{"type":"system","subtype":"informational","level":"info","content":"x"}`+"\n"), func(localChatEvent chatEvent) { evs = append(evs, localChatEvent) })
	if len(evs) != 1 || evs[0].Note != "safeguards flagged this session" {
		t.Fatalf("evs = %+v", evs)
	}
}
