package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fsq(base, dir, path string) string {
	return base + "?dir=" + url.QueryEscape(dir) + "&path=" + url.QueryEscape(path)
}

func names(lookup map[string]any) (out []string) {
	for _, value := range lookup["entries"].([]any) {
		lookup := value.(map[string]any)
		out = append(out, lookup["type"].(string)+":"+lookup["name"].(string))
	}
	return
}

func TestFsTreeListsDirsFirst(t *testing.T) {
	handler, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, "zdir"), 0o755)
	os.Mkdir(filepath.Join(proj, "Adir"), 0o755)
	os.WriteFile(filepath.Join(proj, "b.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(proj, "A.txt"), []byte("x"), 0o644)
	code, lookup, raw := call(t, handler, "GET", fsq("/v1/fs/tree", proj, ""), "")
	if code != 200 {
		t.Fatal(code, raw)
	}
	if got := strings.Join(names(lookup), ","); got != "dir:Adir,dir:zdir,file:A.txt,file:b.txt" || lookup["truncated"] != false {
		t.Fatal(got, raw)
	}
	os.WriteFile(filepath.Join(proj, "Adir", "in.txt"), nil, 0o644)
	_, lookup, _ = call(t, handler, "GET", fsq("/v1/fs/tree", proj, "Adir"), "")
	if got := strings.Join(names(lookup), ","); got != "file:in.txt" {
		t.Fatal(got)
	}
	if counter, _, _ := call(t, handler, "GET", fsq("/v1/fs/tree", proj, "b.txt"), ""); counter != 400 {
		t.Fatal("file as dir", counter)
	}
}

func TestFsTreeSymlinkListedAsSymlink(t *testing.T) {
	handler, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, "real"), 0o755)
	os.WriteFile(filepath.Join(proj, "real", "f.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(proj, "plain.txt"), []byte("x"), 0o644)
	os.Symlink("real", filepath.Join(proj, "lnkdir"))
	os.Symlink("plain.txt", filepath.Join(proj, "lnkfile"))
	_, lookup, raw := call(t, handler, "GET", fsq("/v1/fs/tree", proj, ""), "")
	got := strings.Join(names(lookup), ",")
	if !strings.Contains(got, "symlink:lnkdir") || !strings.Contains(got, "symlink:lnkfile") {
		t.Fatal(got)
	}
	if counter, _, _ := call(t, handler, "GET", fsq("/v1/fs/tree", proj, "lnkdir"), ""); counter != 403 {
		t.Fatal("tree through link", counter, raw)
	}
	if counter, _, raw := call(t, handler, "GET", fsq("/v1/fs/entries/content", proj, "lnkfile"), ""); counter != 403 || strings.Contains(raw, "content") {
		t.Fatal("file through link", counter, raw)
	}
}

func TestFsTreeTruncates(t *testing.T) {
	handler, _, proj := web(t)
	old := ideMaxEntries
	ideMaxEntries = 3
	t.Cleanup(func() { ideMaxEntries = old })
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		os.WriteFile(filepath.Join(proj, name), nil, 0o644)
	}
	_, lookup, _ := call(t, handler, "GET", fsq("/v1/fs/tree", proj, ""), "")
	if len(lookup["entries"].([]any)) != 3 || lookup["truncated"] != true {
		t.Fatal(lookup)
	}
}

func TestFsFileRoundTrip(t *testing.T) {
	handler, _, proj := web(t)
	os.MkdirAll(filepath.Join(proj, "d"), 0o755)
	os.WriteFile(filepath.Join(proj, "d", "é.txt"), []byte("héllo"), 0o644)
	_, m1, raw := call(t, handler, "GET", fsq("/v1/fs/entries/content", proj, "d/é.txt"), "")
	if m1["content"] != "héllo" || m1["etag"] == "" || m1["size"].(float64) != 6 {
		t.Fatal(raw)
	}
	_, m2, _ := call(t, handler, "GET", fsq("/v1/fs/entries/content", proj, "d/é.txt"), "")
	if m1["etag"] != m2["etag"] {
		t.Fatal("etag unstable")
	}
}

func TestFsFileRefusals(t *testing.T) {
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "bin"), []byte("ab\x00cd"), 0o644)
	os.WriteFile(filepath.Join(proj, "big"), []byte(strings.Repeat("a", ideMaxFile+1)), 0o644)
	os.WriteFile(filepath.Join(proj, "bad"), []byte("a\xffb"), 0o644)
	os.Mkdir(filepath.Join(proj, "sub"), 0o755)
	os.Symlink("bin", filepath.Join(proj, "lnk"))
	for _, record := range []struct {
		dir, p string
		want   int
	}{
		{proj, "bin", 415}, {proj, "bad", 415}, {proj, "big", 413}, {proj, "lnk", 403},
		{proj, "../x", 400}, {proj, "/etc/passwd", 400}, {proj, "", 400}, {proj, "sub", 400},
		{proj, "absent", 404}, {t.TempDir(), "x", 404},
	} {
		code, lookup, raw := call(t, handler, "GET", fsq("/v1/fs/entries/content", record.dir, record.p), "")
		if code != record.want || errCode(lookup) == "" || strings.Contains(raw, proj) {
			t.Errorf("%q: %d %s", record.p, code, raw)
		}
	}
}

func TestFsFileCRLFExact(t *testing.T) {
	handler, _, proj := web(t)
	os.WriteFile(filepath.Join(proj, "w.txt"), []byte("a\r\nb\r\n"), 0o644)
	_, lookup, _ := call(t, handler, "GET", fsq("/v1/fs/entries/content", proj, "w.txt"), "")
	if lookup["content"] != "a\r\nb\r\n" {
		t.Fatalf("%q", lookup["content"])
	}
}

func TestFsReadRequiresFsRead(t *testing.T) {
	gateRig := newGateRig(t)
	no := gateRig.user(t, "sans", false)
	gateRig.grant(t, no, "", RightMemRead)
	yes := gateRig.user(t, "avec", false)
	gateRig.grant(t, yes, "", RightFsRead)
	for _, path := range []string{"/v1/fs/tree", "/v1/fs/entries/content"} {
		if recorder := gateRig.do("GET", path, &no, "", ""); recorder.Code != 403 {
			t.Errorf("%s sans fs_read : %d", path, recorder.Code)
		}
		if recorder := gateRig.do("GET", path, &yes, "", ""); recorder.Code != 200 || !reached(recorder) {
			t.Errorf("%s avec fs_read : %d", path, recorder.Code)
		}
	}
}

func TestFsTreeHidesDotGit(t *testing.T) {
	handler, _, proj := web(t)
	os.Mkdir(filepath.Join(proj, ".git"), 0o755)
	os.WriteFile(filepath.Join(proj, "a.txt"), []byte("x"), 0o644)
	_, lookup, raw := call(t, handler, "GET", fsq("/v1/fs/tree", proj, ""), "")
	if got := strings.Join(names(lookup), ","); got != "file:a.txt" {
		t.Fatal(got, raw)
	}
}
