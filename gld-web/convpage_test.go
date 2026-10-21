package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
