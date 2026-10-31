package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHideEnvironHelper(t *testing.T) {
	if os.Getenv("GLD_TEST_HIDE_ENVIRON") == "" {
		t.Skip("processus d'aide")
	}
	if err := hideEnviron(); err != nil {
		os.Exit(2)
	}
	os.Stdout.WriteString("prêt\n")
	time.Sleep(10 * time.Second)
}

func TestHideEnviron(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHideEnvironHelper$")
	cmd.Env = append(os.Environ(), "GLD_TEST_HIDE_ENVIRON=1", childTokenEnv+"=secret-enfant")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	buf := make([]byte, 64)
	if count, _ := out.Read(buf); !strings.Contains(string(buf[:count]), "prêt") {
		t.Fatalf("aide non prête : %q", buf[:count])
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/environ")
	if err == nil && strings.Contains(string(data), "secret-enfant") {
		t.Fatal("jeton lisible dans /proc/<pid>/environ")
	}
}
