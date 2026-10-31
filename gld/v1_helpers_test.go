package main

import (
	"net/http"
	"strings"
	"testing"
)

// v1ok : appel REST réussi (2xx) dont la réponse contient want.
func v1ok(t *testing.T, handler http.Handler, method, path, body, want string) map[string]any {
	t.Helper()
	code, lookup, raw := call(t, handler, method, path, body)
	if code < 200 || code > 299 || !strings.Contains(raw, want) {
		t.Fatalf("%s %s %s : %d %s", method, path, cut(body, 200), code, cut(raw, 300))
	}
	return lookup
}

// v1err : erreur au format uniforme {"error":{"code","message"}} avec le statut voulu ; le message contient want.
func v1err(t *testing.T, handler http.Handler, method, path, body string, status int, want string) map[string]any {
	t.Helper()
	code, lookup, raw := call(t, handler, method, path, body)
	localLookup, _ := lookup["error"].(map[string]any)
	if code != status || localLookup == nil || localLookup["code"] == "" || !strings.Contains(raw, want) {
		t.Fatalf("%s %s %s : %d au lieu de %d %s", method, path, cut(body, 200), code, status, cut(raw, 300))
	}
	return localLookup
}
