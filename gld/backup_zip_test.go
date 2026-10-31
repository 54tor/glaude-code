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
	count := 0
	for _, value := range es {
		if strings.HasPrefix(value.Name(), backupTmpPrefix) {
			count++
		}
	}
	return count
}

func rawZip(t *testing.T, entries ...*zip.FileHeader) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, fileHeader := range entries {
		writer, err := zw.CreateHeader(fileHeader)
		if err != nil {
			t.Fatal(err)
		}
		if fileHeader.Mode()&os.ModeSymlink != 0 {
			writer.Write([]byte("/etc/passwd"))
		} else if fileHeader.Name == "manifest.json" {
			writer.Write([]byte(`{"format":1}`))
		} else if !fileHeader.Mode().IsDir() {
			writer.Write([]byte("x"))
		}
	}
	zw.Close()
	return buf.Bytes()
}

func hdr(name string, mode os.FileMode) *zip.FileHeader {
	fileHeader := &zip.FileHeader{Name: name, Method: zip.Deflate}
	fileHeader.SetMode(mode)
	return fileHeader
}

func TestExtractBackupRoundTrip(t *testing.T) {
	files := map[string][]byte{
		"home/settings.json":     []byte(`{"theme":"dark"}`),
		"home/skills/a/SKILL.md": []byte("# a"),
		"projects/p/memory/m.md": []byte("mémoire"),
	}
	data, err := writeBackupZip(files, BackupManifest{Format: backupFormat, Gld: "1.0", Cats: []string{"skills"}})
	if err != nil {
		t.Fatal(err)
	}
	dir, backupManifest, err := extractBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if backupManifest.Format != backupFormat || backupManifest.Gld != "1.0" || len(backupManifest.Cats) != 1 {
		t.Fatalf("manifeste : %+v", backupManifest)
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
	manJSON := func(flags int) []byte {
		data, _ := writeBackupZip(nil, BackupManifest{Format: flags})
		return data
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
	for _, record := range cases {
		t.Run(record.name, func(t *testing.T) {
			before := countBackupTmp(t)
			dir, _, err := extractBackup(record.zip)
			if err == nil {
				os.RemoveAll(dir)
				t.Fatal("refus attendu")
			}
			if record.want != "" && !strings.Contains(err.Error(), record.want) {
				t.Fatalf("message %q sans %q", err, record.want)
			}
			if count := countBackupTmp(t); count != before {
				t.Fatalf("dossier temporaire restant : %d -> %d", before, count)
			}
		})
	}
}

func TestExtractBackupSize(t *testing.T) {
	data, err := writeBackupZip(map[string][]byte{"home/big": make([]byte, 3<<20)}, BackupManifest{Format: backupFormat})
	if err != nil {
		t.Fatal(err)
	}
	if dir, _, err := extractBackup(data); err == nil {
		os.RemoveAll(dir)
		t.Fatal("entrée de 3 Mo acceptée")
	}
}

func TestExtractBackupOlderFormat(t *testing.T) {
	data, _ := writeBackupZip(map[string][]byte{"home/a": []byte("a")}, BackupManifest{Format: 0})
	dir, _, err := extractBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(dir)
}
