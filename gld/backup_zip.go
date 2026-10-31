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
	executableBits     = 0o111
	executableFileMode = 0o755
)

var backupRoots = map[string]bool{backupManifestName: true, "home": true, "plugins.json": true, "mcp.json": true, "projects": true}

type BackupManifest struct {
	Format int      `json:"format"`
	Gld    string   `json:"gld"`
	Date   string   `json:"date"`
	Cats   []string `json:"cats"`
}

// writeBackupZip : files = chemin en « / » → contenu ; le manifeste est ajouté.
func writeBackupZip(files map[string][]byte, manifest BackupManifest) ([]byte, error) {
	return writeBackupZipExec(files, nil, manifest)
}

// writeBackupZipExec : les entrées de executable sont écrites en 0755 pour que le bit exécutable (scripts de skills) survive.
func writeBackupZipExec(files map[string][]byte, executable map[string]bool, manifest BackupManifest) ([]byte, error) {
	manifestJSON, marshalErr := json.Marshal(manifest)
	if marshalErr != nil {
		return nil, marshalErr
	}
	var archiveBuffer bytes.Buffer
	zipWriter := zip.NewWriter(&archiveBuffer)
	addEntry := func(name string, content []byte) error {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if executable[name] {
			header.SetMode(executableFileMode)
		}
		entryWriter, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = entryWriter.Write(content)
		return err
	}
	if err := addEntry(backupManifestName, manifestJSON); err != nil {
		return nil, err
	}
	for name, content := range files {
		if err := addEntry(name, content); err != nil {
			return nil, err
		}
	}
	if err := zipWriter.Close(); err != nil {
		return nil, err
	}
	return archiveBuffer.Bytes(), nil
}

// extractBackup : l'archive vient d'un tiers, donc chaque entrée est validée avant toute écriture.
// Le dossier retourné est à supprimer par l'appelant ; en cas d'erreur il l'est déjà.
func extractBackup(archive []byte) (dir string, manifest BackupManifest, err error) {
	if len(archive) > backupMaxArchive {
		return "", manifest, terr("srv.backup_zip.tropGrosse")
	}
	zipReader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", manifest, terr("srv.backup_zip.illisible", "A1", err)
	}
	if len(zipReader.File) > backupMaxEntries {
		return "", manifest, terr("srv.backup_zip.tropDEntrees")
	}
	for _, entry := range zipReader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name := entry.Name
		root, _, _ := strings.Cut(name, "/")
		hasForbiddenCharacter := strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "\x00")
		if hasForbiddenCharacter {
			return "", manifest, terr("srv.backup_zip.nomRefuse", "A1", name)
		}
		for _, segment := range strings.Split(name, "/") {
			if segment == ".." || segment == "" {
				return "", manifest, terr("srv.backup_zip.nomRefuse", "A1", name)
			}
		}
		if !entry.Mode().IsRegular() {
			return "", manifest, terr("srv.backup_zip.entreeNonRegulier", "A1", name)
		}
		isTreeRoot := root == "home" || root == "projects"
		nestedOutsideTree := !isTreeRoot && strings.Contains(name, "/")
		if !backupRoots[root] || nestedOutsideTree {
			return "", manifest, terr("srv.backup_zip.nomRefuse", "A1", name)
		}
	}
	dir, err = os.MkdirTemp("", backupTmpPrefix)
	if err != nil {
		return "", manifest, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
			dir = ""
		}
	}()
	var totalSize int64
	manifestSeen := false
	for _, entry := range zipReader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		entryReader, readErr := entry.Open()
		if readErr != nil {
			return dir, manifest, terr("srv.backup_zip.illisible", "A1", readErr)
		}
		data, readErr := io.ReadAll(io.LimitReader(entryReader, backupMaxEntry+1))
		entryReader.Close()
		if readErr != nil {
			return dir, manifest, terr("srv.backup_zip.illisible", "A1", readErr)
		}
		if len(data) > backupMaxEntry {
			return dir, manifest, terr("srv.backup_zip.entreeTropGrosse", "A1", entry.Name)
		}
		totalSize += int64(len(data))
		if totalSize > backupMaxTotal {
			return dir, manifest, terr("srv.backup_zip.tropVolumineuse")
		}
		if entry.Name == backupManifestName {
			if decodeErr := json.Unmarshal(data, &manifest); decodeErr != nil {
				return dir, manifest, terr("srv.backup_zip.manifesteInvalide", "A1", decodeErr)
			}
			if manifest.Format > backupFormat {
				return dir, manifest, terr("srv.backup_zip.formatTropRecent", "A1", manifest.Format, "A2", backupFormat)
			}
			manifestSeen = true
		}
		if writeErr := writeConfigExec(filepath.Join(dir, filepath.FromSlash(entry.Name)), data, entry.Mode()&executableBits != 0); writeErr != nil {
			return dir, manifest, writeErr
		}
	}
	if !manifestSeen {
		return dir, manifest, terr("srv.backup_zip.sansManifeste")
	}
	return dir, manifest, nil
}
