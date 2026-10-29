package main

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

func countBackupTmp(t *testing.T) int {
	t.Helper()
	es, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range es {
		if strings.HasPrefix(e.Name(), backupTmpPrefix) {
			n++
		}
	}
	return n
}

func rawZip(t *testing.T, entries ...*zip.FileHeader) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, h := range entries {
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if h.Mode()&os.ModeSymlink != 0 {
			w.Write([]byte("/etc/passwd"))
		} else if h.Name == "manifest.json" {
			w.Write([]byte(`{"format":1}`))
		} else if !h.Mode().IsDir() {
			w.Write([]byte("x"))
		}
	}
	zw.Close()
	return buf.Bytes()
}

func hdr(name string, mode os.FileMode) *zip.FileHeader {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate}
	h.SetMode(mode)
	return h
}

func TestExtractBackupRoundTrip(t *testing.T) {
	files := map[string][]byte{
		"home/settings.json":     []byte(`{"theme":"dark"}`),
		"home/skills/a/SKILL.md": []byte("# a"),
		"projects/p/memory/m.md": []byte("mémoire"),
	}
	b, err := writeBackupZip(files, BackupManifest{Format: backupFormat, Gld: "1.0", Cats: []string{"skills"}})
	if err != nil {
		t.Fatal(err)
	}
	dir, m, err := extractBackup(b)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if m.Format != backupFormat || m.Gld != "1.0" || len(m.Cats) != 1 {
		t.Fatalf("manifeste : %+v", m)
	}
	for name, want := range files {
		got, err := os.ReadFile(dir + "/" + name)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s : %q, %v", name, got, err)
		}
	}
}

func TestExtractBackupRejects(t *testing.T) {
	man := func() *zip.FileHeader { return hdr("manifest.json", 0o644) }
	manJSON := func(f int) []byte {
		b, _ := writeBackupZip(nil, BackupManifest{Format: f})
		return b
	}
	cases := []struct {
		name string
		zip  []byte
		want string
	}{
		{"dotdot", rawZip(t, man(), hdr("../x", 0o644)), ""},
		{"absolu", rawZip(t, man(), hdr("/etc/x", 0o644)), ""},
		{"backslash", rawZip(t, man(), hdr(`home\x`, 0o644)), ""},
		{"symlink", rawZip(t, man(), hdr("home/l", os.ModeSymlink|0o777)), ""},
		{"segment", rawZip(t, man(), hdr("autre/x", 0o644)), ""},
		{"sansManifeste", rawZip(t, hdr("home/x", 0o644)), ""},
		{"format2", manJSON(2), "2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := countBackupTmp(t)
			dir, _, err := extractBackup(c.zip)
			if err == nil {
				os.RemoveAll(dir)
				t.Fatal("refus attendu")
			}
			if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Fatalf("message %q sans %q", err, c.want)
			}
			if n := countBackupTmp(t); n != before {
				t.Fatalf("dossier temporaire restant : %d -> %d", before, n)
			}
		})
	}
}

func TestExtractBackupSize(t *testing.T) {
	b, err := writeBackupZip(map[string][]byte{"home/big": make([]byte, 3<<20)}, BackupManifest{Format: backupFormat})
	if err != nil {
		t.Fatal(err)
	}
	if dir, _, err := extractBackup(b); err == nil {
		os.RemoveAll(dir)
		t.Fatal("entrée de 3 Mo acceptée")
	}
}

func TestExtractBackupOlderFormat(t *testing.T) {
	b, _ := writeBackupZip(map[string][]byte{"home/a": []byte("a")}, BackupManifest{Format: 0})
	dir, _, err := extractBackup(b)
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(dir)
}
