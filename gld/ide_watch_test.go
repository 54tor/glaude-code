package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
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
		ideCloseAll()
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
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/v1/fs/watch?dir="+proj+"&paths=", nil)
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
	if c, _, _ := call(t, h, "GET", "/v1/fs/watch?dir=/nope", ""); c != 404 {
		t.Fatal(c)
	}
}

func TestFsWatchRequiresFsRead(t *testing.T) {
	g := newGateRig(t)
	no := g.user(t, "sans", false)
	g.grant(t, no, "", RightMemRead)
	if w := g.do("GET", "/v1/fs/watch", &no, "", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func drain(ch <-chan ideEvent) []ideEvent {
	var out []ideEvent
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestWatchBufferOverflowResyncOnce(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	old := ideSubBuf
	ideSubBuf = 4
	t.Cleanup(func() { ideSubBuf = old })
	ws, _ := ideWS(proj)
	ch, cancel := ws.Subscribe([]string{""})
	defer cancel()
	for i := 0; i < 12; i++ {
		mk(t, proj, fmt.Sprintf("f%02d", i))
		if i == 5 {
			time.Sleep(3 * ideDebounce)
		}
	}
	time.Sleep(3 * ideDebounce)
	n := 0
	for _, e := range drain(ch) {
		if e == ideResync {
			n++
		}
	}
	if n != 1 {
		t.Fatal("resync :", n)
	}
	mk(t, proj, "after")
	if e := next(t, ch); e != (ideEvent{"after", "create"}) {
		t.Fatal(e)
	}
}

func TestWatchEventOverflowResync(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	ideDebounce = 400 * time.Millisecond
	var fw *fsnotify.Watcher
	old := ideNewWatcher
	ideNewWatcher = func() (*fsnotify.Watcher, error) {
		w, err := fsnotify.NewWatcher()
		fw = w
		return w, err
	}
	t.Cleanup(func() { ideNewWatcher = old })
	mk(t, proj, "a.go")
	ws, _ := ideWS(proj)
	until(t, "index", func() bool { _, st := ws.Index(); return st == "ready" })
	ch, cancel := ws.Subscribe([]string{""})
	defer cancel()
	os.WriteFile(filepath.Join(proj, "p.txt"), []byte("1"), 0o644)
	time.Sleep(100 * time.Millisecond)
	fw.Errors <- fsnotify.ErrEventOverflow
	if e := next(t, ch); e != (ideEvent{"p.txt", "create"}) {
		t.Fatal("pending perdu :", e)
	}
	for e := next(t, ch); e != ideResync; e = next(t, ch) {
		if e.Path != "p.txt" {
			t.Fatal(e)
		}
	}
	if _, st := ws.Index(); st != "building" {
		t.Fatal("index pas invalidé :", st)
	}
	until(t, "index pas reconstruit", func() bool { return indexHas(ws, "p.txt") })
}

func TestWatchFallbackResyncAndRebuild(t *testing.T) {
	h, _, proj := web(t)
	wsTest(t)
	mk(t, proj, "a.go")
	ws, _ := ideWS(proj)
	until(t, "index", func() bool { _, st := ws.Index(); return st == "ready" })
	ch, cancel := ws.Subscribe([]string{""})
	defer cancel()
	ws.mu.Lock()
	ws.fallbackLocked()
	ws.mu.Unlock()
	if e := next(t, ch); e != ideResync {
		t.Fatal(e)
	}
	if _, m, _ := call(t, h, "GET", "/v1/fs/index?dir="+proj, ""); m["state"] != "building" {
		t.Fatal(m)
	}
	until(t, "pas reconstruit", func() bool {
		_, m, _ := call(t, h, "GET", "/v1/fs/index?dir="+proj, "")
		return m["state"] == "partial" && len(m["files"].([]any)) == 1
	})
}

func TestWatchTreeBudgetShared(t *testing.T) {
	for _, c := range []struct {
		name   string
		lim    ideLimits
		budget time.Duration
		max    int
	}{{"entrées", ideLimits{5, 12, time.Minute}, time.Minute, 5}, {"échéance", ideLimits{1000, 12, time.Minute}, time.Nanosecond, 0}} {
		t.Run(c.name, func(t *testing.T) {
			_, _, proj := web(t)
			wsTest(t)
			setLimits(t, c.lim)
			ideDebounce = 300 * time.Millisecond
			oldB := ideTreeBudget
			ideTreeBudget = c.budget
			t.Cleanup(func() { ideTreeBudget = oldB })
			ws, _ := ideWS(proj)
			until(t, "index", func() bool { _, st := ws.Index(); return st == "ready" })
			for i := 0; i < 10; i++ {
				d := filepath.Join(proj, fmt.Sprintf("d%02d", i))
				os.MkdirAll(filepath.Join(d, "s"), 0o755)
				os.WriteFile(filepath.Join(d, "s", "f"), []byte("x"), 0o644)
			}
			until(t, "pas partiel", func() bool { _, st := ws.Index(); return st == "partial" })
			if es, _ := ws.Index(); len(es) > c.max {
				t.Fatal(len(es), es)
			}
		})
	}
}

func TestFsWatchCutOnRevoke(t *testing.T) {
	old := authWatchEvery
	t.Cleanup(func() { authWatchEvery = old })
	authWatchEvery = 100 * time.Millisecond
	g := newGateRig(t)
	u := g.user(t, "alice", false)
	g.grant(t, u, "", RightFsRead)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/fs/watch", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(": ok\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	srv := httptest.NewServer(authGate(mux, g.s, withAuthWatch(withHomes(mux))))
	defer srv.Close()
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/v1/fs/watch", nil)
	req.Header.Set("Cookie", (&http.Cookie{Name: sessCookie, Value: u.cookie}).String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(err, resp)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	if l, _ := br.ReadString('\n'); l != ": ok\n" {
		t.Fatal(l)
	}
	start := time.Now()
	g.s.Revoke(u.id, mainHomeID, RightFsRead)
	io.Copy(io.Discard, br)
	if d := time.Since(start); d > 6*time.Second || ctx.Err() != nil {
		t.Fatalf("coupé en %v", d)
	}
}

// Paramètres paths répétés : un dossier dont le nom contient une virgule reste un seul chemin.
func TestFsWatchRepeatedPathsKeepCommas(t *testing.T) {
	h, _, proj := web(t)
	wsTest(t)
	mk(t, proj, "a/.k", "b/.k", "a,b/.k")
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/v1/fs/watch?dir="+proj+"&paths=a%2Cb", nil)
	req.Host = testHost
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	mk(t, proj, "a/x.txt", "b/y.txt")
	time.Sleep(4 * ideDebounce)
	mk(t, proj, "a,b/z.txt")
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var got ideEvent
			json.Unmarshal([]byte(d), &got)
			if got.Path != "a,b/z.txt" || got.Type != "create" {
				t.Fatal("premier évènement inattendu :", got)
			}
			return
		}
	}
	t.Fatal("flux coupé")
}

// Une suppression de masse ne balaie pas l'index pour chaque fichier : 10 000 retraits sur 20 000 entrées restent brefs.
func TestWatchMassRemoveFast(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	w := &ideWorkspace{dir: dir, root: root, subs: map[*ideSub]bool{}, idxDirs: map[string]bool{},
		declared: map[string]int{}, snaps: map[string]map[string]ideSig{}}
	var entries []ideIndexEntry
	set := map[string]bool{}
	for i := 0; i < 20000; i++ {
		p := fmt.Sprintf("d%03d/f%05d", i/100, i)
		entries = append(entries, ideIndexEntry{Path: p})
		set[p] = true
		w.idxDirs[fmt.Sprintf("d%03d", i/100)] = true
		w.idxDirs[fmt.Sprintf("d%03d/sub", i/100)] = true
	}
	w.ix = &ideIdx{state: "ready", entries: entries, set: set}
	var evs []ideEvent
	for i := 0; i < 10000; i++ {
		evs = append(evs, ideEvent{fmt.Sprintf("d%03d/f%05d", i/100, i), "remove"})
	}
	evs = append(evs, ideEvent{"d199", "remove"})
	start := time.Now()
	w.mu.Lock()
	w.applyLocked(evs)
	w.mu.Unlock()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("10 000 retraits en %v", d)
	}
	if len(w.ix.entries) != 20000-10000-100 || w.ix.set["d199/f19950"] || !w.ix.set["d198/f19850"] {
		t.Fatal(len(w.ix.entries))
	}
	if w.idxDirs["d199"] || w.idxDirs["d199/sub"] || !w.idxDirs["d000"] || !w.idxDirs["d198/sub"] {
		t.Fatal("dossiers surveillés")
	}
}

// À l'arrêt du serveur, un flux /v1/fs/watch ouvert se termine dès la fermeture des espaces, sans attendre le délai de Shutdown.
func TestFsWatchEndsOnClose(t *testing.T) {
	h, _, proj := web(t)
	wsTest(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/v1/fs/watch?dir="+proj+"&paths=", nil)
	req.Host = testHost
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	ws, _ := ideWS(proj)
	ch, cancel := ws.Subscribe([]string{""})
	defer cancel()
	ideCloseAll()
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, resp.Body); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("flux toujours ouvert après fermeture de l'espace")
	}
	if _, ok := <-ch; ok {
		t.Fatal("canal d'abonné pas fermé")
	}
}

// Un dossier illisible est seulement sauté : la veille inotify reste active pour le reste.
func TestWatchAddDeniedSkipsDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root lit tout")
	}
	_, _, proj := web(t)
	wsTest(t)
	mk(t, proj, "locked/x")
	os.Chmod(filepath.Join(proj, "locked"), 0)
	t.Cleanup(func() { os.Chmod(filepath.Join(proj, "locked"), 0o755) })
	ws, _ := ideWS(proj)
	ch, cancel := ws.Subscribe([]string{"locked", ""})
	defer cancel()
	ws.mu.Lock()
	poll := ws.poll
	ws.mu.Unlock()
	if poll {
		t.Fatal("passé au sondage pour un dossier illisible")
	}
	mk(t, proj, "n.txt")
	if e := next(t, ch); e != (ideEvent{"n.txt", "create"}) {
		t.Fatal(e)
	}
}

// Les limites d'inotify (ENOSPC, EMFILE) font passer au sondage.
func TestWatchAddLimitFallsBackToPoll(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	old := ideWatchAdd
	ideWatchAdd = func(*fsnotify.Watcher, string) error { return fmt.Errorf("add: %w", syscall.ENOSPC) }
	t.Cleanup(func() { ideWatchAdd = old })
	mk(t, proj, "src/a")
	ws, _ := ideWS(proj)
	_, cancel := ws.Subscribe([]string{"src"})
	defer cancel()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if !ws.poll {
		t.Fatal("ENOSPC sans repli au sondage")
	}
}

func TestWatchDirRecreatedStillWatched(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	sub := filepath.Join(proj, "sub")
	os.Mkdir(sub, 0o755)
	ws, err := ideWS(proj)
	if err != nil {
		t.Fatal(err)
	}
	ch, cancel := ws.Subscribe([]string{"", "sub"})
	defer cancel()
	os.RemoveAll(sub)
	time.Sleep(3 * ideDebounce)
	os.Mkdir(sub, 0o755)
	time.Sleep(3 * ideDebounce)
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("1"), 0o644)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Path == "sub/a.txt" {
				return
			}
		case <-deadline:
			t.Fatal("dossier recréé plus surveillé")
		}
	}
}

func TestWatchDirRecreatedAsSymlinkNotWatched(t *testing.T) {
	_, _, proj := web(t)
	wsTest(t)
	sub := filepath.Join(proj, "sub")
	os.Mkdir(sub, 0o755)
	var adds atomic.Int32
	old := ideWatchAdd
	ideWatchAdd = func(fw *fsnotify.Watcher, p string) error {
		if p == sub {
			adds.Add(1)
		}
		return fw.Add(p)
	}
	t.Cleanup(func() { ideWatchAdd = old })
	ws, err := ideWS(proj)
	if err != nil {
		t.Fatal(err)
	}
	ch, cancel := ws.Subscribe([]string{"", "sub"})
	defer cancel()
	before := adds.Load()
	os.RemoveAll(sub)
	time.Sleep(3 * ideDebounce)
	os.Symlink(t.TempDir(), sub)
	for e := next(t, ch); e.Path != "sub" || e.Type != "create"; e = next(t, ch) {
	}
	time.Sleep(2 * ideDebounce)
	if adds.Load() != before {
		t.Fatal("lien symbolique ajouté à la veille")
	}
}
