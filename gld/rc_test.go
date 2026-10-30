package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

func TestRcSessions(t *testing.T) {
	old := root
	root = filepath.Join(t.TempDir(), "projects")
	t.Cleanup(func() { root = old })
	d := filepath.Join(cfgDir(), "sessions")
	os.MkdirAll(d, 0o755)
	pid := os.Getpid()
	w := func(n, body string) { os.WriteFile(filepath.Join(d, n), []byte(body), 0o600) }
	w("a.json", `{"pid":`+strconv.Itoa(pid)+`,"sessionId":"rc1","bridgeSessionId":"session_x"}`)
	w("b.json", `{"pid":`+strconv.Itoa(pid)+`,"sessionId":"plain"}`)
	w("c.json", `{"pid":999999999,"sessionId":"dead","bridgeSessionId":"session_y"}`)
	if got := rcSessions(); !slices.Equal(got, []string{"rc1"}) {
		t.Fatalf("rcSessions = %v", got)
	}
}
