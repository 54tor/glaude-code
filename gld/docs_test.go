package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestWrittenPath(t *testing.T) {
	in := func(p string) json.RawMessage { b, _ := json.Marshal(map[string]string{"file_path": p}); return b }
	for _, c := range []struct{ name, p, want string }{
		{"Write", "/tmp/r.md", "/tmp/r.md"}, {"Write", "/tmp/r.HTML", "/tmp/r.HTML"}, {"Write", "/tmp/a.svg", ""},
		{"Write", "rel.md", ""}, {"Edit", "/tmp/r.md", ""}, {"Write", "/tmp/x.go", ""},
	} {
		if got := writtenPath(c.name, in(c.p)); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.name, c.p, got, c.want)
		}
	}
}

func TestMentionedDocs(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(d+"/r.md", []byte("x"), 0o600)
	txt := "Rapport : " + d + "/r.md et `" + d + "/r.md` ; https://x.fr" + d + "/r.md ; " + d + "/absent.md ; " + d + "/a.go"
	got := mentionedDocs(txt)
	if len(got) != 1 || got[0] != d+"/r.md" {
		t.Errorf("got %v", got)
	}
}
