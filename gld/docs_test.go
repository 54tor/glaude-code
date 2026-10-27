package main

import (
	"encoding/json"
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
