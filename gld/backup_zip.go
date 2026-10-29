package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	backupFormat       = 1
	backupMaxArchive   = 16 << 20
	backupMaxEntries   = 2000
	backupMaxTotal     = 32 << 20
	backupMaxEntry     = 2 << 20
	backupTmpPrefix    = "gld-backup-"
	backupManifestName = "manifest.json"
)

var backupRoots = map[string]bool{backupManifestName: true, "home": true, "plugins.json": true, "mcp.json": true, "projects": true}

type BackupManifest struct {
	Format int      `json:"format"`
	Gld    string   `json:"gld"`
	Date   string   `json:"date"`
	Cats   []string `json:"cats"`
}

// writeBackupZip : files = chemin en « / » → contenu ; le manifeste est ajouté.
func writeBackupZip(files map[string][]byte, m BackupManifest) ([]byte, error) {
	return writeBackupZipExec(files, nil, m)
}

// writeBackupZipExec : les entrées de exec sont écrites en 0755 pour que le bit exécutable (scripts de skills) survive.
func writeBackupZipExec(files map[string][]byte, exec map[string]bool, m BackupManifest) ([]byte, error) {
	mj, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, b []byte) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if exec[name] {
			h.SetMode(0o755)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
	if err := add(backupManifestName, mj); err != nil {
		return nil, err
	}
	for name, b := range files {
		if err := add(name, b); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// extractBackup : l'archive vient d'un tiers, donc chaque entrée est validée avant toute écriture.
// Le dossier retourné est à supprimer par l'appelant ; en cas d'erreur il l'est déjà.
func extractBackup(b []byte) (dir string, m BackupManifest, err error) {
	if len(b) > backupMaxArchive {
		return "", m, terr("srv.backup_zip.tropGrosse")
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return "", m, terr("srv.backup_zip.illisible", "A1", err)
	}
	if len(zr.File) > backupMaxEntries {
		return "", m, terr("srv.backup_zip.tropDEntrees")
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		n := f.Name
		root, _, _ := strings.Cut(n, "/")
		if strings.HasPrefix(n, "/") || strings.Contains(n, `\`) || strings.Contains(n, "\x00") {
			return "", m, terr("srv.backup_zip.nomRefuse", "A1", n)
		}
		for _, seg := range strings.Split(n, "/") {
			if seg == ".." || seg == "" {
				return "", m, terr("srv.backup_zip.nomRefuse", "A1", n)
			}
		}
		if !f.Mode().IsRegular() {
			return "", m, terr("srv.backup_zip.entreeNonRegulier", "A1", n)
		}
		if !backupRoots[root] || (root != "home" && root != "projects" && strings.Contains(n, "/")) {
			return "", m, terr("srv.backup_zip.nomRefuse", "A1", n)
		}
	}
	dir, err = os.MkdirTemp("", backupTmpPrefix)
	if err != nil {
		return "", m, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
			dir = ""
		}
	}()
	var total int64
	seen := false
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, e := f.Open()
		if e != nil {
			return dir, m, terr("srv.backup_zip.illisible", "A1", e)
		}
		data, e := io.ReadAll(io.LimitReader(rc, backupMaxEntry+1))
		rc.Close()
		if e != nil {
			return dir, m, terr("srv.backup_zip.illisible", "A1", e)
		}
		if len(data) > backupMaxEntry {
			return dir, m, terr("srv.backup_zip.entreeTropGrosse", "A1", f.Name)
		}
		if total += int64(len(data)); total > backupMaxTotal {
			return dir, m, terr("srv.backup_zip.tropVolumineuse")
		}
		if f.Name == backupManifestName {
			if e := json.Unmarshal(data, &m); e != nil {
				return dir, m, terr("srv.backup_zip.manifesteInvalide", "A1", e)
			}
			if m.Format > backupFormat {
				return dir, m, terr("srv.backup_zip.formatTropRecent", "A1", m.Format, "A2", backupFormat)
			}
			seen = true
		}
		if e := writeConfigExec(filepath.Join(dir, filepath.FromSlash(f.Name)), data, f.Mode()&0o111 != 0); e != nil {
			return dir, m, e
		}
	}
	if !seen {
		return dir, m, terr("srv.backup_zip.sansManifeste")
	}
	return dir, m, nil
}
