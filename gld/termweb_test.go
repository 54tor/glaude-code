package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Terminal web de bout en bout, avec un faux tmux qui recopie ses frappes dans un fichier : jeton à usage unique et limité
// dans le temps, Origin d'un autre site refusée, un seul client par session, lecture seule tant que la main n'est pas prise.
func TestTerminalWeb(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty indisponible")
	}
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("pas de cat")
	}
	t.Setenv("GLD_WEB_TERMINAL", "1")
	handler, _, _ := web(t)
	frappes := filepath.Join(t.TempDir(), "frappes")
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nexec "+cat+" > '"+frappes+"'\n"), 0o755)
	t.Setenv("PATH", bin)
	oldRun, oldPid, oldPar := tmuxRun, sessionPIDs, parentOf
	t.Cleanup(func() { tmuxRun, sessionPIDs, parentOf = oldRun, oldPid, oldPar })
	sessionPIDs = func() map[string]int { return map[string]int{sid: 300} }
	parentOf = func(pid int) int { return map[int]int{300: 100}[pid] }
	tmuxRun = func(_ string, values ...string) (string, error) { // appelé aussi par le serveur : aucun état partagé
		switch values[0] {
		case "list-panes":
			return "%3 100\n", nil
		case "display-message":
			return "$1 @2 120 40\n", nil
		}
		return "", nil
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	hosts[host] = true

	token := func() string {
		t.Helper()
		lookup := v1ok(t, handler, "POST", "/v1/terminal", `{"id":"`+sid+`"}`, "token")
		return lookup["token"].(string)
	}
	dial := func(tok, origin string) (*websocket.Conn, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, resp, err := websocket.Dial(ctx, "ws://"+host+"/v1/terminal/ws?t="+tok, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
		if err != nil {
			if resp == nil {
				t.Fatal(err)
			}
			return nil, resp.StatusCode
		}
		return conn, 101
	}
	self := "http://" + host

	status(t, handler, "POST", "/v1/terminal", `{"id":"pas-un-id"}`, 400)
	v1err(t, handler, "POST", "/v1/terminal", `{"id":"99999999-2222-3333-4444-555555555555"}`, 409, "pas dans tmux")
	if _, code := dial("inconnu", self); code != 403 {
		t.Fatalf("jeton inconnu : %d", code)
	}
	tok := token()
	if _, code := dial(tok, "http://evil.example"); code != 403 {
		t.Fatalf("autre origine : %d", code)
	}
	if _, code := dial(tok, self); code != 403 {
		t.Fatalf("jeton réutilisé : %d", code)
	}
	tok = token()
	termMu.Lock()
	termTok := termToks[tok]
	termTok.exp = time.Now().Add(-time.Second)
	termToks[tok] = termTok
	termMu.Unlock()
	if _, code := dial(tok, self); code != 403 {
		t.Fatalf("jeton expiré : %d", code)
	}

	localConn, code := dial(token(), self)
	if code != 101 {
		t.Fatalf("ouverture : %d", code)
	}
	defer localConn.CloseNow()
	if _, code := dial(token(), self); code != 409 {
		t.Fatalf("second client : %d", code)
	}
	ctx := context.Background()
	localConn.Write(ctx, websocket.MessageBinary, []byte("lecture\n"))
	localConn.Write(ctx, websocket.MessageText, []byte(`{"write":true}`))
	localConn.Write(ctx, websocket.MessageBinary, []byte("main\n"))
	deadline := time.Now().Add(3 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		if got, _ = os.ReadFile(frappes); strings.Contains(string(got), "main") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if string(got) != "main\n" {
		t.Fatalf("frappes reçues par le terminal : %q", got)
	}
	localConn.Close(websocket.StatusNormalClosure, "")
	// le serveur libère la session en dernier (après ses appels à tmux) : attendre avant que t.Cleanup ne restaure tmuxRun
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		termMu.Lock()
		busy := termBusy[sid]
		termMu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(end) {
			t.Fatal("terminal jamais libéré après fermeture")
		}
	}
}
