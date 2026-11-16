package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSessionsReturnsAnIndependentSliceToEachCaller(t *testing.T) {
	previousRoot := root
	t.Cleanup(func() { root = previousRoot })
	root = filepath.Join(t.TempDir(), "projects")
	projectDir := filepath.Join(root, "-home-bob-app")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := `{"type":"user","cwd":"/home/bob/app","message":{"role":"user","content":"salut"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, "11111111-1111-1111-1111-111111111111.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	var waitGroup sync.WaitGroup
	start := make(chan struct{})
	for callerIndex := 0; callerIndex < 64; callerIndex++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			for repeat := 0; repeat < 20; repeat++ {
				groups := sessions()
				groups[0] = Group{Project: "/modifié par l'appelant"}
			}
		}()
	}
	close(start)
	waitGroup.Wait()
}
