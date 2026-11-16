package main

// Ménage des dossiers de projets éphémères : Claude Code crée un dossier sous projects/ pour chaque répertoire de travail,
// et des outils (résumeurs, tests) lancent des sessions dans /tmp, ce qui laisse des milliers de dossiers sans transcript
// (juste un sous-dossier memory vide). Chacun coûtait un parcours du disque au décodage de son nom.

import (
	"path/filepath"
	"strings"
	"time"
)

const (
	ephemeralProjectPrefix = "-tmp-"
	ephemeralProjectMinAge = time.Hour
	emptyProjectsEvery     = 6 * time.Hour
)

// hasNoFile : vrai si l'arbre ne contient que des dossiers (aucun fichier ni lien).
func hasNoFile(dirPath string) bool {
	entries, err := readDirTrusted(dirPath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() || !hasNoFile(filepath.Join(dirPath, entry.Name())) {
			return false
		}
	}
	return true
}

// removeEmptyTree supprime les dossiers de bas en haut ; os.Root.Remove refuse un dossier qui contient quelque chose.
func removeEmptyTree(dirPath string) {
	entries, _ := readDirTrusted(dirPath)
	for _, entry := range entries {
		removeEmptyTree(filepath.Join(dirPath, entry.Name()))
	}
	_ = rRemove(cfgDir(), dirPath)
}

// sweepEmptyProjectDirs retire les dossiers « -tmp-… » de projects/ sans aucun fichier et inactifs depuis ephemeralProjectMinAge ;
// renvoie leur nombre.
func sweepEmptyProjectDirs(now time.Time) int {
	entries, err := readDirTrusted(root)
	if err != nil {
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ephemeralProjectPrefix) {
			continue
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil || now.Sub(entryInfo.ModTime()) < ephemeralProjectMinAge {
			continue
		}
		projectPath := filepath.Join(root, entry.Name())
		if hasNoFile(projectPath) {
			removeEmptyTree(projectPath)
			removed++
		}
	}
	return removed
}

func emptyProjectsLoop(every time.Duration) {
	sweepEmptyProjectDirs(time.Now())
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-homeCtx.Done():
			return
		case <-ticker.C:
			sweepEmptyProjectDirs(time.Now())
		}
	}
}
