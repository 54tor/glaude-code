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
func fsReq(op string, lookup map[string]any) (method, target string, body map[string]any) {
	values := url.Values{}
	body = map[string]any{}
	for key, value := range lookup {
		body[key] = value
	}
	if dir, ok := body["dir"].(string); ok {
		values.Set("dir", dir)
		delete(body, "dir")
	}
	switch op {
	case "save":
		values.Set("path", body["path"].(string))
		delete(body, "path")
		return "PUT", "/v1/fs/entries/content?" + values.Encode(), body
	case "new":
		return "POST", "/v1/fs/entries?" + values.Encode(), body
	case "rename":
		return "PATCH", "/v1/fs/entries?" + values.Encode(), body
	case "rm":
		values.Set("item", body["path"].(string))
		return "DELETE", "/v1/fs/entries?" + values.Encode(), nil
	case "restore":
		return "PATCH", "/v1/fs/trash/" + url.PathEscape(body["id"].(string)) + "?" + values.Encode(), map[string]any{"trashed": false}
	case "purge":
		values.Set("item", body["id"].(string))
		return "DELETE", "/v1/fs/trash?" + values.Encode(), nil
	case "files":
		return "POST", "/v1/fs/files?" + values.Encode(), body
	}
	panic("op " + op)
}

func fsop(t *testing.T, handler http.Handler, op string, lookup map[string]any) (int, map[string]any, string) {
	t.Helper()
	message, target, body := fsReq(op, lookup)
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	return call(t, handler, message, target, string(data))
}

func errCode(lookup map[string]any) string {
	localLookup, _ := lookup["error"].(map[string]any)
	content, _ := localLookup["code"].(string)
	return content
}

func TestFsRmAndPurgeSeveralItems(t *testing.T) {
	handler, _, proj := web(t)
	for _, name := range []string{"a.txt", "b.txt"} {
		os.WriteFile(filepath.Join(proj, name), []byte(name), 0o644)
	}
	query := "dir=" + url.QueryEscape(proj)
	counter, lookup, raw := call(t, handler, "DELETE", "/v1/fs/entries?"+query+"&item=a.txt&item=b.txt", "")
	ids, _ := lookup["done"].([]any)
	if counter != 200 || len(ids) != 2 {
		t.Fatal(counter, raw)
	}
	if counter, _, _ := call(t, handler, "DELETE", "/v1/fs/entries?"+query+"&item=gone.txt&item=x.txt", ""); counter != 404 {
		t.Fatal("nothing valid must give the uniform error", counter)
	}
	os.WriteFile(filepath.Join(proj, "c.txt"), nil, 0o644)
	counter, lookup, raw = call(t, handler, "DELETE", "/v1/fs/entries?"+query+"&item=gone.txt&item=c.txt", "")
	done, _ := lookup["done"].([]any)
	failed, _ := lookup["failed"].([]any)
	if counter != 200 || len(done) != 1 || len(failed) != 1 || failed[0].(map[string]any)["item"] != "gone.txt" {
		t.Fatal("partial report", counter, raw)
	}
	if f0, _ := failed[0].(map[string]any)["error"].(map[string]any); f0["code"] != "not_found" {
		t.Fatal(raw)
	}
	ids = append(ids, done[0])
	if counter, _, _ := call(t, handler, "DELETE", "/v1/fs/entries?"+query, ""); counter != 400 {
		t.Fatal("no item", counter)
	}
	path := "/v1/fs/trash?" + query + "&item=" + ids[0].(string) + "&item=" + ids[1].(string) + "&item=" + ids[2].(string) + "&item=bad"
	if counter, lookup, raw := call(t, handler, "DELETE", path, ""); counter != 200 || len(lookup["done"].([]any)) != 3 || len(lookup["failed"].([]any)) != 1 {
		t.Fatal(counter, raw)
	}
	if _, lookup, _ := call(t, handler, "GET", "/v1/fs/trash?"+query, ""); len(lookup["items"].([]any)) != 0 {
		t.Fatal(lookup)
	}
}
