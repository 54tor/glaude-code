//go:build !windows

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// writeScript écrit un exécutable sh dans un dossier de test et rend son chemin.
func writeScript(t *testing.T, body string) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "gld")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return scriptPath
}

func TestPathless(t *testing.T) {
	pathErr := &fs.PathError{Op: "lstat", Path: "/home/bob/secret", Err: unix.EACCES}
	got := pathless(pathErr)
	if got.Error() != "lstat: permission denied" || strings.Contains(got.Error(), "/home/bob") {
		t.Fatalf("pathless = %q", got)
	}
	wrapped := errors.Join(errors.New("contexte"), pathErr)
	if strings.Contains(pathless(wrapped).Error(), "/home/bob") {
		t.Fatalf("chemin conservé dans une erreur enveloppée : %q", pathless(wrapped))
	}
	plain := errors.New("pas de chemin")
	if pathless(plain) != plain {
		t.Fatal("une erreur sans chemin doit rester telle quelle")
	}
}

func TestLstatOwnerOS(t *testing.T) {
	directory := t.TempDir()
	filePath := filepath.Join(directory, "f")
	os.WriteFile(filePath, nil, 0o640)
	linkPath := filepath.Join(directory, "l")
	os.Symlink(filePath, linkPath)

	ownerUID, ownerGID, mode, err := lstatOwnerOS(filePath)
	if err != nil || int(ownerUID) != os.Getuid() || int(ownerGID) != os.Getgid() || mode.Perm() != 0o640 || !mode.IsRegular() {
		t.Fatalf("fichier : %d %d %v %v", ownerUID, ownerGID, mode, err)
	}
	_, _, linkMode, err := lstatOwnerOS(linkPath)
	if err != nil || linkMode&os.ModeSymlink == 0 {
		t.Fatalf("le lien doit être décrit lui-même, pas sa cible : %v %v", linkMode, err)
	}
	if _, _, _, err := lstatOwnerOS(filepath.Join(directory, "absent")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("absent : %v", err)
	}
}

// Une ACL étendue (utilisateur nommé + masque), sans quoi le noyau la ramène aux bits de mode et ne la stocke pas.
func extendedACL() []byte {
	entry := func(tag, perm uint16, id uint32) []byte {
		return []byte{byte(tag), byte(tag >> 8), byte(perm), byte(perm >> 8), byte(id), byte(id >> 8), byte(id >> 16), byte(id >> 24)}
	}
	const undefinedID = 0xffffffff
	acl := []byte{2, 0, 0, 0}
	for _, aclEntry := range [][]byte{
		entry(0x01, 6, undefinedID), entry(0x02, 4, 1234), entry(0x04, 4, undefinedID), entry(0x10, 4, undefinedID), entry(0x20, 4, undefinedID),
	} {
		acl = append(acl, aclEntry...)
	}
	return acl
}

func TestHasACLOS(t *testing.T) {
	previousGOOS := runAsGOOS
	t.Cleanup(func() { runAsGOOS = previousGOOS })
	runAsGOOS = "linux"
	directory := t.TempDir()
	plainFile := filepath.Join(directory, "plain")
	os.WriteFile(plainFile, nil, 0o600)
	if hasACL, err := hasACLOS(plainFile); err != nil || hasACL {
		t.Fatalf("fichier sans ACL : %v %v", hasACL, err)
	}
	if _, err := hasACLOS(filepath.Join(directory, "absent")); !errors.Is(err, unix.ENOENT) {
		t.Fatalf("fichier absent : %v", err)
	}

	aclFile := filepath.Join(directory, "acl")
	os.WriteFile(aclFile, nil, 0o600)
	if err := unix.Setxattr(aclFile, "system.posix_acl_access", extendedACL(), 0); err != nil {
		t.Skipf("ACL POSIX non posable ici : %v", err)
	}
	if hasACL, err := hasACLOS(aclFile); err != nil || !hasACL {
		t.Fatalf("ACL d'accès non vue : %v %v", hasACL, err)
	}
	runAsGOOS = "darwin"
	if hasACL, err := hasACLOS(aclFile); err != nil || hasACL {
		t.Fatalf("hors Linux, aucune ACL n'est cherchée : %v %v", hasACL, err)
	}
}

func TestHasACLOSDefaultACL(t *testing.T) {
	previousGOOS := runAsGOOS
	t.Cleanup(func() { runAsGOOS = previousGOOS })
	runAsGOOS = "linux"
	directory := t.TempDir()
	if err := unix.Setxattr(directory, "system.posix_acl_default", extendedACL(), 0); err != nil {
		t.Skipf("ACL par défaut non posable ici : %v", err)
	}
	if hasACL, err := hasACLOS(directory); err != nil || !hasACL {
		t.Fatalf("ACL par défaut non vue : %v %v", hasACL, err)
	}
}

func TestSelfExeOS(t *testing.T) {
	got, err := selfExeOS()
	if err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	want, _ := filepath.EvalSymlinks(executable)
	if got != want || !filepath.IsAbs(got) {
		t.Fatalf("selfExeOS = %q, attendu %q", got, want)
	}
}

func TestBinaryHasCapsOS(t *testing.T) {
	previousGOOS := runAsGOOS
	t.Cleanup(func() { runAsGOOS = previousGOOS })
	runAsGOOS = "linux"
	directory := t.TempDir()
	plainBinary := filepath.Join(directory, "plain")
	os.WriteFile(plainBinary, nil, 0o755)
	if hasCaps, err := binaryHasCapsOS(plainBinary); err != nil || hasCaps {
		t.Fatalf("binaire sans capacité : %v %v", hasCaps, err)
	}
	if _, err := binaryHasCapsOS(filepath.Join(directory, "absent")); !errors.Is(err, unix.ENOENT) {
		t.Fatalf("binaire absent : %v", err)
	}

	cappedBinary := filepath.Join(directory, "capped")
	os.WriteFile(cappedBinary, nil, 0o755)
	// vfs_cap_data v2 : magic_etc, permitted et inheritable (deux mots chacun), ici vides.
	if err := unix.Setxattr(cappedBinary, "security.capability", []byte{0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 0); err != nil {
		t.Skipf("capacités de fichier non posables ici : %v", err)
	}
	if hasCaps, err := binaryHasCapsOS(cappedBinary); err != nil || !hasCaps {
		t.Fatalf("capacités non vues : %v %v", hasCaps, err)
	}
	runAsGOOS = "darwin"
	if hasCaps, err := binaryHasCapsOS(cappedBinary); err != nil || hasCaps {
		t.Fatalf("hors Linux : %v %v", hasCaps, err)
	}
}

func TestCanReadAsHelperExitCodes(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		exitCode     string
		wantAuth     bool
		wantBinWrite bool
		wantErr      bool
	}{
		{"aucun accès", "0", false, false, false},
		{"dossier des comptes accessible", "3", true, false, false},
		{"binaire modifiable", "4", false, true, false},
		{"échec du contrôle", "1", false, false, true},
		{"code inconnu", "2", false, false, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			argumentsFile := filepath.Join(t.TempDir(), "args")
			bin := writeScript(t, `printf '%s|%s|%s|%s' "$PWD" "$1" "$2" "$3" > "`+argumentsFile+`"; exit `+testCase.exitCode)
			auth, binWritable, err := canReadAsHelper("/home/bob/accounts", bin)
			if auth != testCase.wantAuth || binWritable != testCase.wantBinWrite || (err != nil) != testCase.wantErr {
				t.Fatalf("auth=%v binW=%v err=%v", auth, binWritable, err)
			}
			recorded, _ := os.ReadFile(argumentsFile)
			if want := "/|_canread|/home/bob/accounts|" + bin; string(recorded) != want {
				t.Fatalf("appel = %q, attendu %q", recorded, want)
			}
		})
	}
	if _, _, err := canReadAsHelper("/home/bob/accounts", filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("binaire introuvable : erreur attendue")
	}
}

func TestWriteSpecAsTarget(t *testing.T) {
	stdinFile := filepath.Join(t.TempDir(), "stdin")
	argumentsFile := filepath.Join(t.TempDir(), "args")
	self := writeScript(t, `printf '%s|%s' "$PWD" "$*" > "`+argumentsFile+`"; cat > "`+stdinFile+`"; echo "/tmp/gld-123.json"`)
	name, err := writeSpecAsTarget(self, []byte(`{"tab":"x"}`))
	if err != nil || name != "/tmp/gld-123.json" {
		t.Fatalf("nom = %q err = %v", name, err)
	}
	if received, _ := os.ReadFile(stdinFile); string(received) != `{"tab":"x"}` {
		t.Fatalf("spec reçue = %q", received)
	}
	if recorded, _ := os.ReadFile(argumentsFile); string(recorded) != "/|_spec" {
		t.Fatalf("appel = %q", recorded)
	}

	if _, err := writeSpecAsTarget(writeScript(t, "cat > /dev/null"), []byte("{}")); err == nil || err.Error() != "gld _spec" {
		t.Fatalf("sortie vide : %v", err)
	}
	if _, err := writeSpecAsTarget(writeScript(t, "cat > /dev/null; echo partial; exit 1"), []byte("{}")); err == nil {
		t.Fatal("échec du processus ignoré")
	}
	if _, err := writeSpecAsTarget(filepath.Join(t.TempDir(), "absent"), nil); err == nil {
		t.Fatal("binaire absent : erreur attendue")
	}
}

func TestRunSpecCopiesStdinToTempFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	stdinFile := filepath.Join(t.TempDir(), "in")
	os.WriteFile(stdinFile, []byte(`{"tab":"y"}`), 0o600)
	stdout, _ := os.Create(filepath.Join(t.TempDir(), "out"))
	stdin, _ := os.Open(stdinFile)
	previousIn, previousOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdin, stdout
	code := runSpec()
	os.Stdin, os.Stdout = previousIn, previousOut
	stdin.Close()
	stdout.Close()

	printed, _ := os.ReadFile(stdout.Name())
	specPath := strings.TrimSpace(string(printed))
	if code != 0 || filepath.Dir(specPath) != tempDir || !strings.HasPrefix(filepath.Base(specPath), "gld-") || !strings.HasSuffix(specPath, ".json") {
		t.Fatalf("code=%d chemin imprimé=%q", code, specPath)
	}
	if content, _ := os.ReadFile(specPath); string(content) != `{"tab":"y"}` {
		t.Fatalf("contenu = %q", content)
	}
	if info, _ := os.Stat(specPath); info.Mode().Perm() != 0o600 {
		t.Fatalf("droits = %v", info.Mode().Perm())
	}
}

func TestRunSpecFailsWithoutTempDir(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "absent"))
	if code := runSpec(); code != 1 {
		t.Fatalf("code = %d", code)
	}
}
