package main

// Tests issus des tests de mutation (gremlins) : chacun fige une limite ou une branche qu'aucun autre test ne vérifiait.

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLimitesHTTP(t *testing.T) {
	h, cfg, _ := web(t)
	pile := strings.Repeat("a", snippetMax)
	ok(t, h, "/snippet-save", `{"key":"u/max.md","text":"`+pile+`"}`, true, "Snippet enregistré")
	t.Setenv("FAKE_DRAFT", "stdin") // le prompt arrive sur stdin, quelle que soit sa taille
	if m := ok(t, h, "/snippet-draft", `{"model":"haiku","instruction":"x","text":"`+pile+`"}`, true, "Mise en forme prête"); m["text"] != "Consigne : x\n" {
		t.Fatalf("prompt reçu : %v", m["text"])
	}
	ok(t, h, "/memory-save", `{"key":"m/-p/max.md","text":"`+strings.Repeat("a", memMax)+`"}`, true, "Enregistré")

	// Le compte rendu de suppression est joint au message.
	put(t, filepath.Join(cfg, "history.jsonl"), `{"sessionId":"`+id1+`"}`+"\n", old)
	v1ok(t, h, "DELETE", "/v1/sessions?item=-p:"+id1, "", "1 session supprimée\\n  history.jsonl   : 1 ligne(s)")

	// Réponse du chat poussée au fil de l'eau.
	put(t, filepath.Join(cfg, "projects", "-p", id2+".jsonl"), `{"cwd":"`+filepath.Join(cfg, "..", "p")+`"}`+"\n", old)
	t.Setenv("FAKE_CHAT", `{"type":"result","subtype":"success"}`)
	r := httptest.NewRequest("POST", "/v1/sessions/"+id2+"/chat?dir=-p", strings.NewReader(`{"prompt":"x"}`))
	r.Host = testHost
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !w.Flushed || !strings.Contains(w.Body.String(), `"done":true`) {
		t.Fatalf("chat non poussé : %v %s", w.Flushed, w.Body)
	}
}

func TestArretSansSignal(t *testing.T) {
	h, _, _ := web(t)
	// Un processus qui ignore l'interruption n'est pas tué pour autant : « Arrêter » demande, ne force pas.
	cmd := exec.Command("/bin/sh", "-c", `trap "" INT; /bin/sleep 30`)
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer cmd.Process.Kill()
	chatMu.Lock()
	chatRun[id1] = cmd
	chatMu.Unlock()
	defer func() { chatMu.Lock(); delete(chatRun, id1); chatMu.Unlock() }()
	time.Sleep(100 * time.Millisecond) // le temps que trap soit posé
	v1ok(t, h, "DELETE", "/v1/sessions/"+id1+"/chat", "", "Arrêt demandé")
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		t.Fatal("processus tué au lieu d'être interrompu")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestMainServeur(t *testing.T) {
	if os.Getenv("GLD_TEST_MAIN") == "1" {
		main()
		return
	}
	home := t.TempDir()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	p := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	l.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainServeur$")
	cmd.Env = append(os.Environ(), "GLD_TEST_MAIN=1", "GLD_WEB_PORT="+p, "HOME="+home, "PATH="+t.TempDir(),
		"CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	var resp *http.Response
	for range 300 {
		if resp, err = http.Get("http://127.0.0.1:" + p + "/v1/state"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("serveur :", err, out.String())
	}
	b := new(bytes.Buffer)
	b.ReadFrom(resp.Body)
	resp.Body.Close()
	if !strings.Contains(b.String(), `"root":"`+filepath.Join(home, ".claude", "projects")+`"`) {
		t.Fatal("root :", b)
	}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:"+p+"/v1/state", nil)
	req.Host = "localhost:" + p
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatal("localhost refusé :", err)
	}
	cmd.Process.Signal(syscall.SIGINT)
	if err := cmd.Wait(); err != nil || !strings.Contains(out.String(), "arrêt.") {
		t.Fatalf("arrêt sur interruption : %v\n%s", err, out.String())
	}
}
