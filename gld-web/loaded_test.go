package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestHookLoaded(t *testing.T) {
	use(t.TempDir())
	for _, in := range []string{
		`{"session_id":"s1","file_path":"/h/.claude/rules/a.md","memory_type":"User","load_reason":"session_start"}`,
		`{"session_id":"s2","file_path":"/h/.claude/rules/a.md","memory_type":"User","load_reason":"path_glob_match","globs":["**/*.ts"],"trigger_file_path":"/p/x.ts"}`,
		`{"session_id":"s2","file_path":"/p/CLAUDE.md","memory_type":"Project","load_reason":"session_start"}`,
		`{"session_id":"s2","file_path":"/p/CLAUDE.md","memory_type":"Project","load_reason":"compact"}`,
		`pas du json`,
		`{"session_id":"s3"}`, // sans fichier : ignoré
	} {
		if runLoaded(strings.NewReader(in)) != 0 {
			t.Fatal("le hook ne doit jamais échouer")
		}
	}
	ev := readLoaded()
	if len(ev) != 4 {
		t.Fatalf("%d événements : %+v", len(ev), ev)
	}
	by := loadsByFile(ev)
	if by["/h/.claude/rules/a.md"].Sessions != 2 || by["/p/CLAUDE.md"].Sessions != 1 {
		t.Fatalf("par fichier : %+v", by)
	}
	s2 := loadsOfSession(ev, "s2")
	if len(s2) != 3 || s2[0].Trigger != "/p/x.ts" || len(s2[0].Globs) != 1 {
		t.Fatalf("session s2 : %+v", s2)
	}
	if len(loadsOfSession(ev, "inconnue")) != 0 {
		t.Fatal("session inconnue")
	}
	// rotation : le journal ne grossit pas sans fin
	big := strings.Repeat(`{"at":1,"session":"x","file":"/f"}`+"\n", loadedMax/30+10)
	if err := os.WriteFile(loadedFile(), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	appendLoaded([]byte(`{"at":2,"session":"y","file":"/g"}` + "\n"))
	if st, _ := os.Stat(loadedFile()); st.Size() > loadedMax {
		t.Fatalf("journal non réduit : %d", st.Size())
	}
	if last := readLoaded(); last[len(last)-1].File != "/g" {
		t.Fatal("dernier événement perdu")
	}
}

// Un hook ne doit jamais lancer un serveur : une sous-commande interne inconnue s'arrête avec un code d'erreur.
func TestSousCommandeInterneInconnue(t *testing.T) {
	if code, ok := dispatchCLI([]string{"gld-web", "_nope"}); !ok || code != 2 {
		t.Fatal(code, ok)
	}
}

// Les appels d'aide à claude (brouillons, tokens, analyses) n'enregistrent jamais de session, n'ont aucun outil
// ni réglage, et tournent dans un dossier privé : un oubli ferait apparaître des sessions parasites dans la liste.
func TestAppelsAideSansSession(t *testing.T) {
	cmd := helperCmd(context.Background(), "haiku", "ok")
	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{"-p", "--no-session-persistence", "--strict-mcp-config", "--disable-slash-commands", "--tools "} {
		if !strings.Contains(args, want) {
			t.Errorf("option manquante %q dans : %s", want, args)
		}
	}
	if cmd.Dir != workDir() || cmd.Dir == os.TempDir() {
		t.Fatalf("dossier de travail : %q", cmd.Dir)
	}
}
