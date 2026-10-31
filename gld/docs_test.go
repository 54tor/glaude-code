package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestWrittenPath(t *testing.T) {
	in := func(path string) json.RawMessage {
		data, _ := json.Marshal(map[string]string{"file_path": path})
		return data
	}
	for _, record := range []struct{ name, p, want string }{
		{"Write", "/tmp/r.md", "/tmp/r.md"}, {"Write", "/tmp/r.HTML", "/tmp/r.HTML"}, {"Write", "/tmp/a.svg", ""},
		{"Write", "rel.md", ""}, {"Edit", "/tmp/r.md", ""}, {"Write", "/tmp/x.go", ""},
	} {
		if got := writtenPath(record.name, in(record.p)); got != record.want {
			t.Errorf("%s %s = %q, want %q", record.name, record.p, got, record.want)
		}
	}
}

func TestMentionedDocs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/r.md", []byte("x"), 0o600)
	txt := "Rapport : " + dir + "/r.md et `" + dir + "/r.md` ; https://x.fr" + dir + "/r.md ; " + dir + "/absent.md ; " + dir + "/a.go"
	got := mentionedDocs(txt)
	if len(got) != 1 || got[0] != dir+"/r.md" {
		t.Errorf("got %v", got)
	}
}
