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
	var builder strings.Builder
	for index := 0; index < 60; index++ {
		role := []string{"user", "assistant"}[index%2]
		text := fmt.Sprintf("message %d", index)
		if index%7 == 0 {
			text += " " + strings.Repeat("é", 500) // ligne bien plus longue que les blocs du test
		}
		fmt.Fprintf(&builder, `{"type":"%s","timestamp":"t%d","message":{"content":%q}}`+"\n", role, index, text)
		if index%5 == 0 {
			builder.WriteString(`{"type":"summary","x":1}` + "\n") // ligne sans message
		}
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	want := conv(path, false)
	defer func(old int64) { convChunk = old }(convChunk)
	for _, chunk := range []int64{37, 128, 1000, 1 << 20} {
		convChunk = chunk
		for _, limit := range []int{1, 7, 25, 500} {
			var got []Msg
			var before int64
			for pages := 0; ; pages++ {
				ms, start, more, err := convPage(path, false, before, limit)
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
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"content":"q"}}`+"\n"+line), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, _, _, err := convPage(path, false, 0, 10)
	if err != nil || len(ms) != 2 {
		t.Fatalf("page : %v %d", err, len(ms))
	}
	capMsgs(ms)
	msg := ms[1]
	if msg.Len != len(text) || msg.Cut == 0 || len(msg.T) > msgCap || msg.T+text[msg.Cut:] != text {
		t.Fatalf("coupe : len=%d cut=%d t=%q", msg.Len, msg.Cut, msg.T)
	}
	got, from := msg.T, msg.Cut
	for steps := 0; from != 0; steps++ {
		part, next, err := convText(path, msg.O, msg.N, false, from)
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
	if _, _, err := convText(path, msg.O, 9, false, 0); err == nil {
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
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, _, _, err := convPageMode(path, false, true, 0, 50)
	if err != nil || len(ms) != 3 || ms[1].R != "tool" || ms[1].T != "" || ms[2].T != "fini" {
		t.Fatalf("ancres : %+v %v", ms, err)
	}
	if plain, _, _, _ := convPage(path, false, 0, 50); len(plain) != 2 {
		t.Fatalf("sans ancres : %d messages", len(plain))
	}
	tools, err := convTools(path, ms[1].O, ms[2].O)
	if err != nil || len(tools) != 2 || !strings.HasPrefix(tools[0].T, "Bash") || !strings.HasPrefix(tools[1].T, "↳") {
		t.Fatalf("outils de la plage : %+v %v", tools, err)
	}
}

// Une session de seules commandes (aucun échange) rend une liste vide, pas null : l'interface lit null comme « pas chargé ».
func TestConvPageSeulesCommandes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	line := `{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}` + "\n" +
		`{"type":"user","message":{"content":"<local-command-stdout></local-command-stdout>"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	msgs, _, more, err := convPage(path, false, 0, 50)
	if err != nil || msgs == nil || len(msgs) != 0 || more {
		t.Fatalf("msgs=%#v more=%v err=%v", msgs, more, err)
	}
}
