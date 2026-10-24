package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// wsTest abaisse les délais et ferme tout espace restant, pour qu'aucun test ne laisse de veille derrière lui.
func wsTest(t *testing.T) {
	t.Helper()
	od, ot, op, oi := ideDebounce, ideIdleTTL, idePollInterval, idePing
	ideDebounce, ideIdleTTL, idePollInterval, idePing = 30*time.Millisecond, time.Minute, 40*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() {
		ideWSMu.Lock()
		for d, w := range ideWorkspaces {
			w.mu.Lock()
			w.closeLocked()
			w.mu.Unlock()
			delete(ideWorkspaces, d)
		}
		ideWSMu.Unlock()
		ideDebounce, ideIdleTTL, idePollInterval, idePing = od, ot, op, oi
	})
}

func next(t *testing.T, ch <-chan ideEvent) ideEvent {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("aucun évènement")
	}
	return ideEvent{}
}

func until(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(what)
}

func expectSeq(t *testing.T, ch <-chan ideEvent, proj string) {
	t.Helper()
	p := filepath.Join(proj, "a.txt")
	os.WriteFile(p, []byte("1"), 0o644)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("22")
	f.Close()
	if e := next(t, ch); e != (ideEvent{"a.txt", "create"}) {
		t.Fatal(e)
	}
	if e := next(t, ch); e != (ideEvent{"a.txt", "write"}) {
		t.Fatal(e)
	}
	time.Sleep(3 * ideDebounce)
	select {
	case e := <-ch:
		t.Fatal("en trop :", e)
	default:
	}
	os.Remove(p)
	if e := next(t, ch); e != (ideEvent{"a.txt", "remove"}) {
		t.Fatal(e)
	}
}

func TestWatchEmitsCreateWriteRemove(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	ideDebounce = 150 * time.Millisecond
	ws, err := ideWS(proj)
	if err != nil {
		t.Fatal(err)
	}
	ch, cancel := ws.Subscribe([]string{""})
	defer cancel()
	expectSeq(t, ch, proj)
}

func TestWatchIgnoresUndeclaredDirs(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	mk(t, proj, "src/k", "other/k")
	ws, _ := ideWS(proj)
	ws.Index()
	ch, cancel := ws.Subscribe([]string{"src", "../x", "absent"})
	defer cancel()
	mk(t, proj, "other/x.txt", "y.txt")
	time.Sleep(3 * ideDebounce)
	mk(t, proj, "src/z.txt")
	if e := next(t, ch); e.Path != "src/z.txt" {
		t.Fatal(e)
	}
}

func TestWatchPollFallback(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	old := ideNewWatcher
	ideNewWatcher = func() (*fsnotify.Watcher, error) { return nil, errors.New("inotify épuisé") }
	t.Cleanup(func() { ideNewWatcher = old })
	ws, _ := ideWS(proj)
	ch, cancel := ws.Subscribe([]string{""})
	defer cancel()
	p := filepath.Join(proj, "a.txt")
	os.WriteFile(p, []byte("1"), 0o644)
	if e := next(t, ch); e != (ideEvent{"a.txt", "create"}) {
		t.Fatal(e)
	}
	os.WriteFile(p, []byte("1234"), 0o644)
	if e := next(t, ch); e != (ideEvent{"a.txt", "write"}) {
		t.Fatal(e)
	}
	os.Remove(p)
	if e := next(t, ch); e != (ideEvent{"a.txt", "remove"}) {
		t.Fatal(e)
	}
}

func indexHas(ws *ideWorkspace, p string) bool {
	es, _ := ws.Index()
	for _, e := range es {
		if e.Path == p {
			return true
		}
	}
	return false
}

func TestWatchUpdatesIndex(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	mk(t, proj, "src/a.ts")
	ws, _ := ideWS(proj)
	until(t, "index", func() bool { _, st := ws.Index(); return st == "ready" })
	mk(t, proj, "src/new.ts")
	until(t, "src/new.ts absent", func() bool { return indexHas(ws, "src/new.ts") })
	mk(t, proj, "node_modules/x", "src/.env", "src/sub/deep.ts")
	until(t, "src/sub/deep.ts absent", func() bool { return indexHas(ws, "src/sub/deep.ts") })
	mk(t, proj, "src/sub/later.ts")
	until(t, "src/sub/later.ts absent", func() bool { return indexHas(ws, "src/sub/later.ts") })
	if indexHas(ws, "node_modules/x") || indexHas(ws, "src/.env") {
		t.Fatal(ws.Index())
	}
	os.Remove(filepath.Join(proj, "src/new.ts"))
	until(t, "src/new.ts resté", func() bool { return !indexHas(ws, "src/new.ts") })
}

func TestWatchReleasesWhenIdle(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	ideIdleTTL = 60 * time.Millisecond
	ws, _ := ideWS(proj)
	ws.Index()
	_, cancel := ws.Subscribe([]string{""})
	time.Sleep(2 * ideIdleTTL)
	ideWSMu.Lock()
	kept := ideWorkspaces[proj] == ws
	ideWSMu.Unlock()
	if !kept {
		t.Fatal("libéré malgré un abonné")
	}
	cancel()
	until(t, "pas libéré", func() bool {
		ideWSMu.Lock()
		defer ideWSMu.Unlock()
		return ideWorkspaces[proj] == nil
	})
	select {
	case <-ws.done:
	default:
		t.Fatal("veille pas fermée")
	}
	if ws.watcher != nil {
		t.Fatal("watcher pas fermé")
	}
	if nw, _ := ideWS(proj); nw == ws {
		t.Fatal("espace réutilisé")
	}
}

func TestWatchIndexRetriesAfterPanic(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	mk(t, proj, "a.go")
	old := ideBuildHook
	var once atomic.Bool
	ideBuildHook = func() {
		if !once.Swap(true) {
			panic("casse")
		}
	}
	t.Cleanup(func() { ideBuildHook = old })
	ws, _ := ideWS(proj)
	until(t, "pas partiel", func() bool { _, st := ws.Index(); return st == "partial" })
	until(t, "pas reconstruit", func() bool { return indexHas(ws, "a.go") })
}

func TestWatchUnknownDir(t *testing.T) {
	web(t)
	wsTest(t)
	if _, err := ideWS(t.TempDir()); err == nil {
		t.Fatal("dossier inconnu accepté")
	}
}

func TestFsWatchSSE(t *testing.T) {
	h, _, proj := web(t)
	wsTest(t)
	ideIdleTTL = 50 * time.Millisecond
	before := runtime.NumGoroutine()
	srv := httptest.NewServer(h)
	tr := &http.Transport{}
	ctx, stop := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/fs-watch?dir="+proj+"&paths=", nil)
	req.Host = testHost
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(resp.Status, resp.Header)
	}
	mk(t, proj, "n.txt")
	sc := bufio.NewScanner(resp.Body)
	var got ideEvent
	ping := false
	for sc.Scan() {
		l := sc.Text()
		if l == ": ping" {
			ping = true
		}
		if d, ok := strings.CutPrefix(l, "data: "); ok {
			json.Unmarshal([]byte(d), &got)
			if strings.Contains(d, "content") {
				t.Fatal(d)
			}
			break
		}
	}
	if got.Path != "n.txt" || got.Type != "create" {
		t.Fatal(got)
	}
	for !ping && sc.Scan() {
		ping = sc.Text() == ": ping"
	}
	stop()
	resp.Body.Close()
	srv.Close()
	tr.CloseIdleConnections()
	until(t, "espace pas libéré", func() bool {
		ideWSMu.Lock()
		defer ideWSMu.Unlock()
		return len(ideWorkspaces) == 0
	})
	until(t, "goroutines qui fuient", func() bool { return runtime.NumGoroutine() <= before })
	if c, _, _ := call(t, h, "GET", "/fs-watch?dir=/nope", ""); c != 404 {
		t.Fatal(c)
	}
}

func TestFsWatchRequiresFsRead(t *testing.T) {
	g := newGateRig(t)
	no := g.user(t, "sans", false)
	g.grant(t, no, "", RightMemRead)
	if w := g.do("GET", "/fs-watch", &no, "", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
}
