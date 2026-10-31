package main

// Réassignation d'une session à un autre projet : le cwd des transcripts est réécrit.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var cwdRe = regexp.MustCompile(`"cwd":("(?:[^"\\]|\\.)*")`)

// remapCwd : oldCwd devient newCwd. Ses sous-dossiers ne suivent que si followSubdirs (l'ancien dossier a disparu : projet
// renommé ou déplacé) et s'ils ne sont pas déjà dans newCwd — sinon ce sont de vrais chemins, laissés tels quels.
func remapCwd(cwd, oldCwd, newCwd string, followSubdirs bool) string {
	separator := string(filepath.Separator)
	if cwd == oldCwd {
		return newCwd
	}
	isUnderOld := strings.HasPrefix(cwd, oldCwd+separator)
	isAlreadyUnderNew := cwd == newCwd || strings.HasPrefix(cwd, newCwd+separator)
	if followSubdirs && isUnderOld && !isAlreadyUnderNew {
		return newCwd + cwd[len(oldCwd):]
	}
	return cwd
}

// cwdRewrite : un transcript réécrit dans path.tmp, en attente du renommage qui remplace l'original.
type cwdRewrite struct {
	path string
	mod  time.Time
}

// commit remplace l'original par sa réécriture, en gardant la date de modification.
func (rewrite cwdRewrite) commit() error {
	configRoot, rel, err := confine(cfgDir(), rewrite.path)
	if err != nil {
		return err
	}
	defer configRoot.Close()
	if err = configRoot.Rename(rel+".tmp", rel); err == nil {
		forgetInfo(rewrite.path)
		err = configRoot.Chtimes(rel, rewrite.mod, rewrite.mod)
	}
	return err
}

func (rewrite cwdRewrite) abort() { rRemove(cfgDir(), rewrite.path+".tmp") }

// prepareCwd écrit dans path.tmp le transcript au cwd remplacé (voir remapCwd) ; commit le met en place.
func prepareCwd(path, oldCwd, newCwd string, followSubdirs bool) (cwdRewrite, error) {
	cwdKey := []byte(`"cwd":`)
	remap := func(match []byte) []byte {
		var cwd string
		if json.Unmarshal(match[len(cwdKey):], &cwd) != nil {
			return match
		}
		remapped := remapCwd(cwd, oldCwd, newCwd, followSubdirs)
		if remapped == cwd {
			return match
		}
		quoted, _ := json.Marshal(remapped)
		return append(append([]byte{}, cwdKey...), quoted...)
	}
	input, err := rOpenFile(cfgDir(), path, os.O_RDONLY, 0)
	if err != nil {
		return cwdRewrite{}, err
	}
	defer input.Close()
	inputInfo, err := input.Stat()
	if err != nil {
		return cwdRewrite{}, err
	}
	tempPath := path + ".tmp"
	if tempInfo, lstatErr := os.Lstat(tempPath); lstatErr == nil && !tempInfo.IsDir() {
		rRemove(cfgDir(), tempPath) // un reste d'un essai interrompu, ou un lien posé à sa place : retiré, jamais suivi
	}
	output, err := rOpenFile(cfgDir(), tempPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return cwdRewrite{}, err
	}
	if chmodErr := output.Chmod(inputInfo.Mode().Perm()); chmodErr != nil {
		output.Close()
		rRemove(cfgDir(), tempPath)
		return cwdRewrite{}, chmodErr
	}
	writer := bufio.NewWriterSize(output, jsonLineBufferSize)
	reader := bufio.NewReaderSize(input, jsonLineBufferSize)
	for {
		rawLine, readErr := reader.ReadBytes('\n')
		if bytes.Contains(rawLine, cwdKey) {
			rawLine = cwdRe.ReplaceAllFunc(rawLine, remap)
		}
		writer.Write(rawLine)
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				err = readErr
			}
			break
		}
	}
	if err == nil {
		err = writer.Flush()
	}
	if err == nil {
		err = output.Sync() // le transcript réécrit remplace l'original : il doit être sur disque avant le renommage
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		rRemove(cfgDir(), tempPath)
		return cwdRewrite{}, err
	}
	return cwdRewrite{path, inputInfo.ModTime()}, nil
}

// reassign déplace des sessions vers le dossier de transcripts de `targetDir` (un dossier existant)
// et y réécrit leur cwd. Tout est vérifié avant le premier déplacement.
func reassign(items []item, targetDir string) (int, error) {
	if !filepath.IsAbs(targetDir) {
		return 0, terr("srv.routes_sessions.cheminAbsoluRequis")
	}
	targetDir = filepath.Clean(targetDir)
	if targetInfo, statErr := os.Stat(targetDir); statErr != nil || !targetInfo.IsDir() {
		return 0, terr("srv.reassign.leDossierCibleN", "A1", targetDir)
	}
	destinationDir := filepath.Join(root, nonAlnum.ReplaceAllString(targetDir, "-"))
	live, liveErr := liveCheck()
	if liveErr != nil {
		return 0, liveErr
	}
	for _, sessionItem := range items {
		if live[sessionItem.ID] {
			return 0, terr("srv.reassign.sessionVivanteFermeLa", "A1", sessionItem.ID[:8])
		}
		if filepath.Join(root, sessionItem.Dir) == destinationDir {
			return 0, terr("srv.reassign.dejaDansCeDossier", "A1", sessionItem.ID[:8])
		}
		// Seul le transcript en conflit bloque : un dossier ID déjà là (reste d'un /cd interrompu) est fusionné.
		if transcriptName := sessionItem.ID + ".jsonl"; lexists(filepath.Join(destinationDir, transcriptName)) {
			return 0, terr("srv.reassign.existeDejaDansLa", "A1", transcriptName)
		}
	}
	// Toutes les réécritures (transcripts et sous-agents de chaque session) sont écrites avant le premier renommage : un
	// échec en cours de route ne laisse pas une session à moitié réécrite.
	pending := make([][]cwdRewrite, len(items))
	abort := func() {
		for _, rewrites := range pending {
			for _, rewrite := range rewrites {
				rewrite.abort()
			}
		}
	}
	for itemIndex, sessionItem := range items {
		sourceDir := filepath.Join(root, sessionItem.Dir)
		transcriptPath := filepath.Join(sourceDir, sessionItem.ID+".jsonl")
		transcriptInfo, transcriptStatErr := os.Stat(transcriptPath)
		if transcriptStatErr != nil {
			continue
		}
		oldCwd := info(transcriptPath, transcriptInfo).Cwd
		if oldCwd == "" || oldCwd == targetDir {
			continue
		}
		_, oldCwdErr := os.Stat(oldCwd)
		files := []string{transcriptPath}
		filepath.WalkDir(filepath.Join(sourceDir, sessionItem.ID), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr == nil && !entry.IsDir() && strings.HasSuffix(path, ".jsonl") {
				files = append(files, path)
			}
			return nil
		})
		for _, file := range files {
			rewrite, prepareErr := prepareCwd(file, oldCwd, targetDir, oldCwdErr != nil)
			if prepareErr != nil {
				abort()
				return 0, prepareErr
			}
			pending[itemIndex] = append(pending[itemIndex], rewrite)
		}
	}
	if mkdirErr := rMkdirAll(cfgDir(), destinationDir, 0o755); mkdirErr != nil {
		abort()
		return 0, mkdirErr
	}
	for itemIndex, sessionItem := range items {
		for _, rewrite := range pending[itemIndex] {
			if commitErr := rewrite.commit(); commitErr != nil {
				abort()
				return itemIndex, commitErr
			}
			pending[itemIndex] = pending[itemIndex][1:]
		}
		sourceDir := filepath.Join(root, sessionItem.Dir)
		// Le dossier d'abord, le transcript en dernier : c'est lui qui rend la session visible, donc un échec
		// intermédiaire ne laisse jamais une session apparente à moitié déplacée.
		for _, name := range []string{sessionItem.ID, sessionItem.ID + ".jsonl"} {
			if moveErr := mergeMove(filepath.Join(sourceDir, name), filepath.Join(destinationDir, name)); moveErr != nil && !os.IsNotExist(moveErr) {
				abort()
				return itemIndex, moveErr
			}
		}
		rRemove(cfgDir(), sourceDir) // seulement s'il est devenu vide
	}
	return len(items), nil
}

func lexists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// mergeMove déplace from vers to. Si to existe déjà, deux dossiers se fusionnent enfant par enfant ; pour un fichier en
// conflit la version de to l'emporte et celle de from reste en place (rien n'est perdu, le dossier source n'est alors
// pas retiré).
func mergeMove(from, to string) error {
	fromInfo, err := os.Lstat(from)
	if err != nil {
		return err
	}
	toInfo, err := os.Lstat(to)
	if os.IsNotExist(err) {
		return rRename(cfgDir(), from, to)
	}
	if err != nil || !fromInfo.IsDir() || !toInfo.IsDir() {
		return err
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if moveErr := mergeMove(filepath.Join(from, entry.Name()), filepath.Join(to, entry.Name())); moveErr != nil {
			return moveErr
		}
	}
	rRemove(cfgDir(), from) // seulement s'il est devenu vide
	return nil
}
