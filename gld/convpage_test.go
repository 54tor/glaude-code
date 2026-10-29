package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// Pagination par la fin : quels que soient la taille des blocs lus et celle des pages, les pages mises bout à bout
// redonnent exactement la conversation entière, dans l'ordre, sans doublon ni trou (lignes plus longues qu'un bloc comprises).
func TestConvPage(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		role := []string{"user", "assistant"}[i%2]
		text := fmt.Sprintf("message %d", i)
		if i%7 == 0 {
			text += " " + strings.Repeat("é", 500) // ligne bien plus longue que les blocs du test
		}
		fmt.Fprintf(&b, `{"type":"%s","timestamp":"t%d","message":{"content":%q}}`+"\n", role, i, text)
		if i%5 == 0 {
			b.WriteString(`{"type":"summary","x":1}` + "\n") // ligne sans message
		}
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	want := conv(p, false)
	defer func(old int64) { convChunk = old }(convChunk)
	for _, chunk := range []int64{37, 128, 1000, 1 << 20} {
		convChunk = chunk
		for _, limit := range []int{1, 7, 25, 500} {
			var got []Msg
			var before int64
			for pages := 0; ; pages++ {
				ms, start, more, err := convPage(p, false, before, limit)
				if err != nil || pages > 100 {
					t.Fatal(err, pages)
				}
				got = append(slices.Clone(ms), got...)
				if !more {
					break
				}
				if before != 0 && start >= before {
					t.Fatalf("chunk %d limit %d : la page ne recule pas (%d >= %d)", chunk, limit, start, before)
				}
				before = start
			}
			if !slices.Equal(got, want) {
				t.Fatalf("chunk %d limit %d : %d messages au lieu de %d", chunk, limit, len(got), len(want))
			}
		}
	}
}

// Message démesuré : la page n'en donne que le début, et les morceaux de GET .../conversation/text mis bout à bout redonnent le texte
// exact, sans couper un caractère multioctet.
func TestConvTextChunks(t *testing.T) {
	defer func(old int) { msgCap = old }(msgCap)
	msgCap = 10
	text := "début " + strings.Repeat("é€x", 20) + " fin"
	line := fmt.Sprintf(`{"type":"assistant","message":{"content":%q}}`+"\n", text)
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(`{"type":"user","message":{"content":"q"}}`+"\n"+line), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, _, _, err := convPage(p, false, 0, 10)
	if err != nil || len(ms) != 2 {
		t.Fatalf("page : %v %d", err, len(ms))
	}
	capMsgs(ms)
	m := ms[1]
	if m.Len != len(text) || m.Cut == 0 || len(m.T) > msgCap || m.T+text[m.Cut:] != text {
		t.Fatalf("coupe : len=%d cut=%d t=%q", m.Len, m.Cut, m.T)
	}
	got, from := m.T, m.Cut
	for steps := 0; from != 0; steps++ {
		part, next, err := convText(p, m.O, m.N, false, from)
		if err != nil || steps > 100 || !utf8.ValidString(part) || part == "" {
			t.Fatalf("morceau à %d : %v %q", from, err, part)
		}
		got, from = got+part, next
	}
	if got != text {
		t.Fatalf("recollé : %q", got)
	}
	if ms[0].Cut != 0 || ms[0].Len != 0 {
		t.Fatalf("message court coupé : %+v", ms[0])
	}
	if _, _, err := convText(p, m.O, 9, false, 0); err == nil {
		t.Fatal("rang hors ligne accepté")
	}
}

// Outils masqués : un appel laisse une ancre vide (pas son résultat) ; les outils d'une plage se relisent à la demande.
func TestConvAnchorsEtOutils(t *testing.T) {
	lines := []string{
		`{"type":"user","timestamp":"t0","message":{"content":"bonjour"}}`,
		`{"type":"assistant","timestamp":"t1","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","timestamp":"t2","message":{"content":[{"type":"tool_result","content":"a b"}]}}`,
		`{"type":"assistant","timestamp":"t3","message":{"content":[{"type":"text","text":"fini"}]}}`,
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, _, _, err := convPageMode(p, false, true, 0, 50)
	if err != nil || len(ms) != 3 || ms[1].R != "tool" || ms[1].T != "" || ms[2].T != "fini" {
		t.Fatalf("ancres : %+v %v", ms, err)
	}
	if plain, _, _, _ := convPage(p, false, 0, 50); len(plain) != 2 {
		t.Fatalf("sans ancres : %d messages", len(plain))
	}
	tools, err := convTools(p, ms[1].O, ms[2].O)
	if err != nil || len(tools) != 2 || !strings.HasPrefix(tools[0].T, "Bash") || !strings.HasPrefix(tools[1].T, "↳") {
		t.Fatalf("outils de la plage : %+v %v", tools, err)
	}
}

// Une session de seules commandes (aucun échange) rend une liste vide, pas null : l'interface lit null comme « pas chargé ».
func TestConvPageSeulesCommandes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	l := `{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}` + "\n" +
		`{"type":"user","message":{"content":"<local-command-stdout></local-command-stdout>"}}` + "\n"
	if err := os.WriteFile(p, []byte(l), 0o600); err != nil {
		t.Fatal(err)
	}
	msgs, _, more, err := convPage(p, false, 0, 50)
	if err != nil || msgs == nil || len(msgs) != 0 || more {
		t.Fatalf("msgs=%#v more=%v err=%v", msgs, more, err)
	}
}
