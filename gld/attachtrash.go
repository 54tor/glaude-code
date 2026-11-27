package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// attachGrace : un fichier déposé dans un brouillon n'est cité par aucun transcript avant l'envoi du message ; indépendant de GLD_GRACE.
	attachGrace    = 24 * time.Hour
	orphanTrashSub = ".trash"
	// ownerKept : fichier restauré à la main, rangé dans l'index pour que la passe de fond ne le remette pas à la corbeille.
	ownerKept = "-"
)

var errOrphanName = terr("srv.attachtrash.nomInvalide")

type OrphanFile struct {
	Name    string  `json:"name"`
	Bytes   int64   `json:"bytes"`
	At      float64 `json:"at"`
	Owner   string  `json:"owner,omitempty"`
	Trashed bool    `json:"trashed"`
}

func orphanTrashDir() string { return filepath.Join(imagesDir(), orphanTrashSub) }

func orphanStampPath(name string) string {
	return filepath.Join(orphanTrashDir(), "."+name+".at")
}

// validOrphanName : un simple nom de fichier déposé, jamais un chemin, un fichier caché ni l'index.
func validOrphanName(name string) bool {
	return name != "" && name == filepath.Base(name) && !strings.HasPrefix(name, ".") && name != ownersFile
}

func hasSessionPrefix(name string) bool {
	const idLength = 36
	return len(name) > idLength && name[idLength] == '-' && idRe.MatchString(name[:idLength])
}

func isUnownedCandidate(name string) bool {
	return validOrphanName(name) && !hasSessionPrefix(name)
}

func trashOrphan(name string) error {
	if !isUnownedCandidate(name) {
		return errOrphanName
	}
	if err := rMkdirAll(cfgDir(), orphanTrashDir(), 0o700); err != nil {
		return err
	}
	// l'horodatage d'abord : sans lui, le fichier déplacé hériterait de son ancien mtime et serait purgé trop tôt
	stamp := []byte(strconv.FormatInt(time.Now().Unix(), 10))
	if err := rWriteAtomic(cfgDir(), orphanStampPath(name), stamp, 0o600); err != nil {
		return err
	}
	if err := rRename(cfgDir(), filepath.Join(imagesDir(), name), filepath.Join(orphanTrashDir(), name)); err != nil {
		rRemove(cfgDir(), orphanStampPath(name))
		return err
	}
	if owners, err := loadOwners(); err == nil {
		if _, indexed := owners[name]; indexed {
			delete(owners, name)
			saveOwners(owners)
		}
	}
	return nil
}

func restoreOrphan(name string) error {
	if !validOrphanName(name) {
		return errOrphanName
	}
	owners, err := loadOwners()
	if err != nil {
		return err
	}
	destination := filepath.Join(imagesDir(), name)
	if _, statErr := os.Lstat(destination); statErr == nil {
		return terr("srv.attachtrash.existeDeja")
	}
	if err := rRename(cfgDir(), filepath.Join(orphanTrashDir(), name), destination); err != nil {
		return err
	}
	rRemove(cfgDir(), orphanStampPath(name))
	owners[name] = ownerKept
	return saveOwners(owners)
}

func purgeOrphan(name string) error {
	if !validOrphanName(name) {
		return errOrphanName
	}
	rRemove(cfgDir(), orphanStampPath(name))
	return rRemove(cfgDir(), filepath.Join(orphanTrashDir(), name))
}

func trashedAt(name string, fallback time.Time) time.Time {
	content, err := readTrusted(orphanStampPath(name))
	if err != nil {
		return fallback
	}
	seconds, parseErr := strconv.ParseInt(strings.TrimSpace(string(content)), 10, 64)
	if parseErr != nil {
		return fallback
	}
	return time.Unix(seconds, 0)
}

func listOrphans() []OrphanFile {
	owners, _ := loadOwners()
	files := []OrphanFile{}
	if entries, err := readDirTrusted(imagesDir()); err == nil {
		for _, entry := range entries {
			info, infoErr := entry.Info()
			if infoErr != nil || !info.Mode().IsRegular() || !isUnownedCandidate(entry.Name()) {
				continue
			}
			owner := owners[entry.Name()]
			if owner == ownerKept {
				owner = ""
			}
			files = append(files, OrphanFile{Name: entry.Name(), Bytes: info.Size(), At: float64(info.ModTime().Unix()), Owner: owner})
		}
	}
	if entries, err := readDirTrusted(orphanTrashDir()); err == nil {
		for _, entry := range entries {
			info, infoErr := entry.Info()
			if infoErr != nil || !info.Mode().IsRegular() || !validOrphanName(entry.Name()) {
				continue
			}
			at := trashedAt(entry.Name(), info.ModTime())
			files = append(files, OrphanFile{Name: entry.Name(), Bytes: info.Size(), At: float64(at.Unix()), Trashed: true})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].At > files[j].At })
	return files
}

// sweepAttachments rattache à leur session les fichiers sans préfixe cités par un transcript, met les autres à la corbeille
// (au-delà de grace()) et purge ceux qui y sont depuis plus de trashKeep. À appeler sous sessionsMu.
func sweepAttachments() (indexed, trashed, purged int, err error) {
	owners, err := loadOwners()
	if err != nil {
		return 0, 0, 0, err
	}
	citations := attachmentCitations()
	entries, _ := readDirTrusted(imagesDir())
	indexChanged := false
	present := map[string]bool{}
	for _, entry := range entries {
		present[entry.Name()] = true
	}
	for name := range owners {
		if !present[name] {
			delete(owners, name)
			indexChanged = true
		}
	}
	for _, entry := range entries {
		info, infoErr := entry.Info()
		name := entry.Name()
		if infoErr != nil || !info.Mode().IsRegular() || !isUnownedCandidate(name) || time.Since(info.ModTime()) < attachGrace {
			continue
		}
		if _, known := owners[name]; known {
			continue
		}
		if sessions := citations[name]; len(sessions) > 0 {
			owners[name] = sessions[0]
			indexChanged = true
			indexed++
			continue
		}
		if trashErr := trashOrphan(name); trashErr != nil {
			err = errors.Join(err, trashErr)
			continue
		}
		trashed++
	}
	if indexChanged {
		err = errors.Join(err, saveOwners(owners))
	}
	if trashEntries, listErr := readDirTrusted(orphanTrashDir()); listErr == nil {
		for _, entry := range trashEntries {
			info, infoErr := entry.Info()
			if infoErr != nil || !info.Mode().IsRegular() || !validOrphanName(entry.Name()) {
				continue
			}
			if time.Since(trashedAt(entry.Name(), info.ModTime())) > trashKeep && purgeOrphan(entry.Name()) == nil {
				purged++
			}
		}
	}
	return indexed, trashed, purged, err
}

// attachSweepLoop : une passe au démarrage, puis à chaque période, jusqu'à l'arrêt du serveur.
func attachSweepLoop(every time.Duration) {
	sweep := func() {
		sessionsMu.Lock()
		defer sessionsMu.Unlock()
		indexed, trashed, purged, err := sweepAttachments()
		journal("attach-sweep : %d rattaché(s), %d à la corbeille, %d purgé(s) : %v", indexed, trashed, purged, errOr(err))
	}
	sweep()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}
