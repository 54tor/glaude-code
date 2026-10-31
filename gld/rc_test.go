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
	dir := filepath.Join(cfgDir(), "sessions")
	os.MkdirAll(dir, 0o755)
	pid := os.Getpid()
	callback := func(name, body string) { os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600) }
	callback("a.json", `{"pid":`+strconv.Itoa(pid)+`,"sessionId":"rc1","bridgeSessionId":"session_x"}`)
	callback("b.json", `{"pid":`+strconv.Itoa(pid)+`,"sessionId":"plain"}`)
	callback("c.json", `{"pid":999999999,"sessionId":"dead","bridgeSessionId":"session_y"}`)
	if got := rcSessions(); !slices.Equal(got, []string{"rc1"}) {
		t.Fatalf("rcSessions = %v", got)
	}
}
