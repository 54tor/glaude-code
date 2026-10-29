package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// fsReq traduit une opération IDE décrite par ses champs (dir, path, from, to, id…) en méthode, URL /v1 et corps.
func fsReq(op string, v map[string]any) (method, target string, body map[string]any) {
	q := url.Values{}
	body = map[string]any{}
	for k, x := range v {
		body[k] = x
	}
	if d, ok := body["dir"].(string); ok {
		q.Set("dir", d)
		delete(body, "dir")
	}
	switch op {
	case "save":
		q.Set("path", body["path"].(string))
		delete(body, "path")
		return "PUT", "/v1/fs/entries/content?" + q.Encode(), body
	case "new":
		return "POST", "/v1/fs/entries?" + q.Encode(), body
	case "rename":
		return "PATCH", "/v1/fs/entries?" + q.Encode(), body
	case "rm":
		q.Set("item", body["path"].(string))
		return "DELETE", "/v1/fs/entries?" + q.Encode(), nil
	case "restore":
		return "PATCH", "/v1/fs/trash/" + url.PathEscape(body["id"].(string)) + "?" + q.Encode(), map[string]any{"trashed": false}
	case "purge":
		q.Set("item", body["id"].(string))
		return "DELETE", "/v1/fs/trash?" + q.Encode(), nil
	case "files":
		return "POST", "/v1/fs/files?" + q.Encode(), body
	}
	panic("op " + op)
}

func fsop(t *testing.T, h http.Handler, op string, v map[string]any) (int, map[string]any, string) {
	t.Helper()
	m, target, body := fsReq(op, v)
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	return call(t, h, m, target, string(b))
}

func errCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func TestFsRmAndPurgeSeveralItems(t *testing.T) {
	h, _, proj := web(t)
	for _, n := range []string{"a.txt", "b.txt"} {
		os.WriteFile(filepath.Join(proj, n), []byte(n), 0o644)
	}
	q := "dir=" + url.QueryEscape(proj)
	c, m, raw := call(t, h, "DELETE", "/v1/fs/entries?"+q+"&item=a.txt&item=b.txt", "")
	ids, _ := m["done"].([]any)
	if c != 200 || len(ids) != 2 {
		t.Fatal(c, raw)
	}
	if c, _, _ := call(t, h, "DELETE", "/v1/fs/entries?"+q+"&item=gone.txt&item=x.txt", ""); c != 404 {
		t.Fatal("nothing valid must give the uniform error", c)
	}
	os.WriteFile(filepath.Join(proj, "c.txt"), nil, 0o644)
	c, m, raw = call(t, h, "DELETE", "/v1/fs/entries?"+q+"&item=gone.txt&item=c.txt", "")
	done, _ := m["done"].([]any)
	failed, _ := m["failed"].([]any)
	if c != 200 || len(done) != 1 || len(failed) != 1 || failed[0].(map[string]any)["item"] != "gone.txt" {
		t.Fatal("partial report", c, raw)
	}
	if f0, _ := failed[0].(map[string]any)["error"].(map[string]any); f0["code"] != "not_found" {
		t.Fatal(raw)
	}
	ids = append(ids, done[0])
	if c, _, _ := call(t, h, "DELETE", "/v1/fs/entries?"+q, ""); c != 400 {
		t.Fatal("no item", c)
	}
	p := "/v1/fs/trash?" + q + "&item=" + ids[0].(string) + "&item=" + ids[1].(string) + "&item=" + ids[2].(string) + "&item=bad"
	if c, m, raw := call(t, h, "DELETE", p, ""); c != 200 || len(m["done"].([]any)) != 3 || len(m["failed"].([]any)) != 1 {
		t.Fatal(c, raw)
	}
	if _, m, _ := call(t, h, "GET", "/v1/fs/trash?"+q, ""); len(m["items"].([]any)) != 0 {
		t.Fatal(m)
	}
}
