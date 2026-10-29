package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fsq(base, dir, p string) string {
	return base + "?dir=" + url.QueryEscape(dir) + "&path=" + url.QueryEscape(p)
}

func names(m map[string]any) (out []string) {
	for _, e := range m["entries"].([]any) {
		x := e.(map[string]any)
		out = append(out, x["type"].(string)+":"+x["name"].(string))
	}
	return
}

func TestFsTreeListsDirsFirst(t *testing.T) {
	h, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, "zdir"), 0o755)
	os.Mkdir(filepath.Join(proj, "Adir"), 0o755)
	os.WriteFile(filepath.Join(proj, "b.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(proj, "A.txt"), []byte("x"), 0o644)
	code, m, raw := call(t, h, "GET", fsq("/v1/fs/tree", proj, ""), "")
	if code != 200 {
		t.Fatal(code, raw)
	}
	if got := strings.Join(names(m), ","); got != "dir:Adir,dir:zdir,file:A.txt,file:b.txt" || m["truncated"] != false {
		t.Fatal(got, raw)
	}
	os.WriteFile(filepath.Join(proj, "Adir", "in.txt"), nil, 0o644)
	_, m, _ = call(t, h, "GET", fsq("/v1/fs/tree", proj, "Adir"), "")
	if got := strings.Join(names(m), ","); got != "file:in.txt" {
		t.Fatal(got)
	}
	if c, _, _ := call(t, h, "GET", fsq("/v1/fs/tree", proj, "b.txt"), ""); c != 400 {
		t.Fatal("file as dir", c)
	}
}

func TestFsTreeSymlinkListedAsSymlink(t *testing.T) {
	h, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, "real"), 0o755)
	os.WriteFile(filepath.Join(proj, "real", "f.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(proj, "plain.txt"), []byte("x"), 0o644)
	os.Symlink("real", filepath.Join(proj, "lnkdir"))
	os.Symlink("plain.txt", filepath.Join(proj, "lnkfile"))
	_, m, raw := call(t, h, "GET", fsq("/v1/fs/tree", proj, ""), "")
	got := strings.Join(names(m), ",")
	if !strings.Contains(got, "symlink:lnkdir") || !strings.Contains(got, "symlink:lnkfile") {
		t.Fatal(got)
	}
	if c, _, _ := call(t, h, "GET", fsq("/v1/fs/tree", proj, "lnkdir"), ""); c != 403 {
		t.Fatal("tree through link", c, raw)
	}
	if c, _, raw := call(t, h, "GET", fsq("/v1/fs/entries/content", proj, "lnkfile"), ""); c != 403 || strings.Contains(raw, "content") {
		t.Fatal("file through link", c, raw)
	}
}

func TestFsTreeTruncates(t *testing.T) {
	h, _, proj := web(t)
	old := ideMaxEntries
	ideMaxEntries = 3
	t.Cleanup(func() { ideMaxEntries = old })
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		os.WriteFile(filepath.Join(proj, n), nil, 0o644)
	}
	_, m, _ := call(t, h, "GET", fsq("/v1/fs/tree", proj, ""), "")
	if len(m["entries"].([]any)) != 3 || m["truncated"] != true {
		t.Fatal(m)
	}
}

func TestFsFileRoundTrip(t *testing.T) {
	h, _, proj := web(t)
	os.MkdirAll(filepath.Join(proj, "d"), 0o755)
	os.WriteFile(filepath.Join(proj, "d", "é.txt"), []byte("héllo"), 0o644)
	_, m1, raw := call(t, h, "GET", fsq("/v1/fs/entries/content", proj, "d/é.txt"), "")
	if m1["content"] != "héllo" || m1["etag"] == "" || m1["size"].(float64) != 6 {
		t.Fatal(raw)
	}
	_, m2, _ := call(t, h, "GET", fsq("/v1/fs/entries/content", proj, "d/é.txt"), "")
	if m1["etag"] != m2["etag"] {
		t.Fatal("etag unstable")
	}
}

func TestFsFileRefusals(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "bin"), []byte("ab\x00cd"), 0o644)
	os.WriteFile(filepath.Join(proj, "big"), []byte(strings.Repeat("a", ideMaxFile+1)), 0o644)
	os.WriteFile(filepath.Join(proj, "bad"), []byte("a\xffb"), 0o644)
	os.Mkdir(filepath.Join(proj, "sub"), 0o755)
	os.Symlink("bin", filepath.Join(proj, "lnk"))
	for _, c := range []struct {
		dir, p string
		want   int
	}{
		{proj, "bin", 415}, {proj, "bad", 415}, {proj, "big", 413}, {proj, "lnk", 403},
		{proj, "../x", 400}, {proj, "/etc/passwd", 400}, {proj, "", 400}, {proj, "sub", 400},
		{proj, "absent", 404}, {t.TempDir(), "x", 404},
	} {
		code, m, raw := call(t, h, "GET", fsq("/v1/fs/entries/content", c.dir, c.p), "")
		if code != c.want || errCode(m) == "" || strings.Contains(raw, proj) {
			t.Errorf("%q: %d %s", c.p, code, raw)
		}
	}
}

func TestFsFileCRLFExact(t *testing.T) {
	h, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "w.txt"), []byte("a\r\nb\r\n"), 0o644)
	_, m, _ := call(t, h, "GET", fsq("/v1/fs/entries/content", proj, "w.txt"), "")
	if m["content"] != "a\r\nb\r\n" {
		t.Fatalf("%q", m["content"])
	}
}

func TestFsReadRequiresFsRead(t *testing.T) {
	g := newGateRig(t)
	no := g.user(t, "sans", false)
	g.grant(t, no, "", RightMemRead)
	yes := g.user(t, "avec", false)
	g.grant(t, yes, "", RightFsRead)
	for _, p := range []string{"/v1/fs/tree", "/v1/fs/entries/content"} {
		if w := g.do("GET", p, &no, "", ""); w.Code != 403 {
			t.Errorf("%s sans fs_read : %d", p, w.Code)
		}
		if w := g.do("GET", p, &yes, "", ""); w.Code != 200 || !reached(w) {
			t.Errorf("%s avec fs_read : %d", p, w.Code)
		}
	}
}

func TestFsTreeHidesDotGit(t *testing.T) {
	h, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, ".git"), 0o755)
	os.WriteFile(filepath.Join(proj, "a.txt"), []byte("x"), 0o644)
	_, m, raw := call(t, h, "GET", fsq("/v1/fs/tree", proj, ""), "")
	if got := strings.Join(names(m), ","); got != "file:a.txt" {
		t.Fatal(got, raw)
	}
}
