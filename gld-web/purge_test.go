package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	id1 = "11111111-1111-1111-1111-111111111111" // supprimée
	id2 = "22222222-2222-2222-2222-222222222222" // supprimée
	id3 = "33333333-3333-3333-3333-333333333333" // gardée : cite l'instantané 111 et le collage aaaa…
	old = 48 * time.Hour
)

func put(t *testing.T, p, content string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(-age)
	os.Chtimes(p, ts, ts)
}

// fixture : une config Claude avec deux sessions à supprimer et une à garder, des restes partagés
// (collages, instantanés de shell) et un memory/ qui doit survivre.
func fixture(t *testing.T) string {
	cfg := t.TempDir()
	p := filepath.Join(cfg, "projects", "-p")
	put(t, filepath.Join(p, id1+".jsonl"), `{"cwd":"/p","m":"snapshot-bash-111-aaa.sh"}`+"\n", old)
	put(t, filepath.Join(p, id1, "subagents", "s.jsonl"), `{"m":"snapshot-bash-222-bbb.sh"}`+"\n", old)
	put(t, filepath.Join(p, id2+".jsonl"), `{"m":"snapshot-bash-111-aaa.sh snapshot-bash-999-new.sh"}`+"\n", old)
	put(t, filepath.Join(p, id3+".jsonl"), `{"m":"snapshot-bash-111-aaa.sh aaaaaaaaaaaaaaaa"}`+"\n", old)
	put(t, filepath.Join(p, "memory", "MEMORY.md"), "garder", old)
	for _, d := range []string{"file-history/" + id1 + "/x", "session-env/" + id1 + "/y", "uploads/" + id1 + "/z",
		"debug/" + id1 + ".txt", "tasks/session-11111111/t", "teams/session-11111111/t", "tasks/session-33333333/t"} {
		put(t, filepath.Join(cfg, d), "x", old)
	}
	put(t, filepath.Join(cfg, "jobs", "11111111", "state.json"), `{"sessionId":"`+id1+`"}`, old)
	put(t, filepath.Join(cfg, "jobs", "22222222", "state.json"), `{"sessionId":"autre"}`, old) // même préfixe, autre session
	put(t, filepath.Join(cfg, "history.jsonl"),
		`{"sessionId":"`+id1+`","pastedContents":{"1":{"contentHash":"aaaaaaaaaaaaaaaa"}}}`+"\n"+
			`{"sessionId":"`+id2+`","pastedContents":{"1":{"contentHash":"bbbbbbbbbbbbbbbb"}}}`+"\n"+
			`{"sessionId":"`+id3+`","pastedContents":{"1":{"contentHash":"cccccccccccccccc"}}}`+"\n", old)
	for _, h := range []string{"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "cccccccccccccccc"} {
		put(t, filepath.Join(cfg, "paste-cache", h+".txt"), "collage", old)
	}
	put(t, filepath.Join(cfg, "shell-snapshots", "snapshot-bash-111-aaa.sh"), "s", old)
	put(t, filepath.Join(cfg, "shell-snapshots", "snapshot-bash-222-bbb.sh"), "s", old)
	put(t, filepath.Join(cfg, "shell-snapshots", "snapshot-bash-999-new.sh"), "s", time.Minute) // tout frais
	return cfg
}

func use(cfg string) {
	root = filepath.Join(cfg, "projects")
	liveSessions = func() map[string]bool { return map[string]bool{} }
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestPurge(t *testing.T) {
	cfg := fixture(t)
	use(cfg)
	n, spared, notes, err := purgeSessions([]item{{"-p", id1}, {"-p", id2}})
	if err != nil || n != 2 || spared != 0 {
		t.Fatalf("n=%d spared=%d err=%v", n, spared, err)
	}
	// Le compte rendu dit exactement ce qui a été effacé.
	if got := strings.Join(notes, "\n"); !strings.Contains(got, "2 ligne(s) de prompt") ||
		!strings.Contains(got, "1 collage(s)") || !strings.Contains(got, "1 instantané(s)") {
		t.Errorf("notes :\n%s", got)
	}
	gone := []string{"projects/-p/" + id1 + ".jsonl", "projects/-p/" + id1, "projects/-p/" + id2 + ".jsonl",
		"file-history/" + id1, "session-env/" + id1, "uploads/" + id1, "debug/" + id1 + ".txt",
		"tasks/session-11111111", "teams/session-11111111", "jobs/11111111",
		"paste-cache/bbbbbbbbbbbbbbbb.txt", "shell-snapshots/snapshot-bash-222-bbb.sh"}
	kept := []string{"projects/-p/" + id3 + ".jsonl", "projects/-p/memory/MEMORY.md", "tasks/session-33333333",
		"jobs/22222222",                            // même préfixe, autre session (state.json tranche)
		"paste-cache/aaaaaaaaaaaaaaaa.txt",         // encore cité par la session 3
		"paste-cache/cccccccccccccccc.txt",         // jamais lié aux sessions supprimées
		"shell-snapshots/snapshot-bash-111-aaa.sh", // encore cité par la session 3
		"shell-snapshots/snapshot-bash-999-new.sh"} // trop récent pour être un reste
	for _, p := range gone {
		if exists(filepath.Join(cfg, p)) {
			t.Errorf("devrait être effacé : %s", p)
		}
	}
	for _, p := range kept {
		if !exists(filepath.Join(cfg, p)) {
			t.Errorf("devrait rester : %s", p)
		}
	}
	h, _ := os.ReadFile(filepath.Join(cfg, "history.jsonl"))
	if strings.Count(string(h), "\n") != 1 || !strings.Contains(string(h), id3) {
		t.Errorf("history.jsonl : il ne devrait rester que la ligne de la session 3 :\n%s", h)
	}
}

// Un hachage encore cité doit être reconnu quels que soient ses chiffres : sinon son collage serait effacé.
func TestHexTokens(t *testing.T) {
	got := map[string]bool{}
	hexTokens([]byte(`x 0123456789abcdef "fedcba98765432100000000000000009" 0123456789ABCDEF 123456789abcdef 9999999999999999`), got)
	want := []string{"0123456789abcdef", "fedcba9876543210", "0000000000000009", "9999999999999999"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("jeton manquant : %s", w)
		}
	}
	if len(got) != len(want) { // ni majuscules, ni jeton de 15 caractères
		t.Errorf("jetons : %v", got)
	}
}

func TestSessionVivanteEpargnee(t *testing.T) {
	cfg := fixture(t)
	use(cfg)
	liveSessions = func() map[string]bool { return map[string]bool{id1: true} }
	n, spared, _, _ := purgeSessions([]item{{"-p", id1}, {"-p", id2}})
	if n != 1 || spared != 1 || !exists(filepath.Join(cfg, "projects", "-p", id1+".jsonl")) {
		t.Fatalf("n=%d spared=%d : la session vivante doit rester", n, spared)
	}
}

func TestSafeRemove(t *testing.T) {
	cfg := fixture(t)
	use(cfg)
	for _, p := range []string{filepath.Dir(cfg), filepath.Join(cfg, "projects", "-p", "memory"),
		filepath.Join(cfg, "projects", "-p", "memory", "MEMORY.md"), filepath.Join(cfg, "..", "x"), cfg} {
		if safeRemove(p) == nil {
			t.Errorf("refus attendu pour %s", p)
		}
	}
}

// tree : chemin relatif → contenu (« d » pour un dossier), pour comparer deux arborescences.
func tree(t *testing.T, dir string) map[string]string {
	m := map[string]string{}
	filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		rel, _ := filepath.Rel(dir, p)
		if e.IsDir() {
			m[rel] = "d"
		} else {
			b, _ := os.ReadFile(p)
			m[rel] = string(b)
		}
		return nil
	})
	return m
}

// TestParitéAvecBash : le nettoyage Go doit laisser exactement la même arborescence que gld-lib.sh.
func TestParitéAvecBash(t *testing.T) {
	lib, _ := filepath.Abs("../bin/gld-lib.sh")
	if _, err := os.Stat(lib); err != nil || runtime.GOOS == "windows" {
		t.Skip("gld-lib.sh ou bash indisponible")
	}
	for _, tool := range []string{"bash", "jq", "comm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " indisponible")
		}
	}
	a, b := fixture(t), fixture(t)
	script := `. "` + lib + `"; cc_tmp; : > "$GLD_TMP/snap"; : > "$GLD_TMP/del"
for id in ` + id1 + ` ` + id2 + `; do
  d="$GLD_ROOT/projects/-p"
  snapshots_cites "$d/$id.jsonl" "$d/$id" >> "$GLD_TMP/snap"
  purger_session "$d" "$id" >/dev/null; echo "$id" >> "$GLD_TMP/del"
done
solder "$GLD_TMP/del" "$GLD_TMP/snap"`
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+a)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash : %v\n%s", err, out)
	}
	use(b)
	if _, _, _, err := purgeSessions([]item{{"-p", id1}, {"-p", id2}}); err != nil {
		t.Fatal(err)
	}
	ta, tb := tree(t, a), tree(t, b)
	for p, c := range ta {
		if d, ok := tb[p]; !ok {
			t.Errorf("bash laisse %s, pas Go", p)
		} else if !bytes.Equal([]byte(c), []byte(d)) {
			t.Errorf("contenu différent : %s", p)
		}
	}
	for p := range tb {
		if _, ok := ta[p]; !ok {
			t.Errorf("Go laisse %s, pas bash", p)
		}
	}
}
