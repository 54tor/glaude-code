//go:build !windows

package main

import (
	"os"
	"testing"
)

func TestDescribeDocHidesFilesNotOwnedByRunAsTarget(t *testing.T) {
	path := t.TempDir() + "/r.md"
	os.WriteFile(path, []byte("x"), 0o600)
	runAsAcct = testAccount() // uid 1500 : le fichier appartient à l'utilisateur du test
	defer func() { runAsAcct = nil }()
	if entry := describeDoc(path, ""); !entry.Missing {
		t.Fatalf("fichier d'un autre compte listé comme lisible : %+v", entry)
	}
	runAsAcct = nil
	if entry := describeDoc(path, ""); entry.Missing {
		t.Fatalf("hors GLD_RUN_AS le fichier doit rester lisible : %+v", entry)
	}
}
