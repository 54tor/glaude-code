package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ideTrash = ".gld-trash"

// ideBeforeRename est un point d'injection pour les tests de course.
var ideBeforeRename = func() {}

var ideTrashID = regexp.MustCompile(`^[0-9]+$`)

// ideBody décode le JSON ; un corps au-delà de limit devient errIdeTooBig.
func ideBody(responseWriter http.ResponseWriter, request *http.Request, limit int64, destination any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(responseWriter, request.Body, limit)).Decode(destination); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			ideFail(responseWriter, errIdeTooBig)
		} else {
			ideFail(responseWriter, errIdePath)
		}
		return false
	}
	return true
}

// ideParentDir vérifie que le parent existe, est un dossier réel et sans lien sur le trajet.
func ideParentDir(root *os.Root, rel string) error {
	dir := path.Dir(rel)
	if dir == "." {
		return nil
	}
	fileInfo, err := ideLstat(root, dir)
	if err != nil {
		return err
	}
	if !fileInfo.IsDir() {
		return errIdePath
	}
	return nil
}

func ideExists(root *os.Root, rel string) bool {
	_, err := root.Lstat(rel)
	return err == nil
}

// ideCodePath : un chemin que Claude Code ou git exécutent (hooks et MCP de .claude/, .git/config et hooks, filtres de
// .gitattributes). L'écrire revient à exécuter du code sous le compte gld : config_write, comme PUT /v1/skills/{key}.
// La comparaison ignore la casse, les points et espaces finaux, les noms courts 8.3 (GIT~1), les flux NTFS (:$DATA) et les points
// de code qu'HFS+ ignore : sur APFS, HFS+, NTFS ou vfat, ces variantes désignent le même fichier.
func ideCodePath(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if ideSegIs(seg, ".git", ".claude", ".mcp.json", ".gitattributes", ".gitmodules") {
			return true
		}
	}
	return false
}

// ideSegIs : seg désigne l'un des noms, avec la même normalisation que ideCodePath.
func ideSegIs(seg string, names ...string) bool {
	seg, _, _ = strings.Cut(seg, ":")
	seg = strings.Map(func(character rune) rune {
		if character >= 0x200c && character <= 0x200f || character >= 0x202a && character <= 0x202e || character >= 0x206a && character <= 0x206f || character == 0xfeff {
			return -1
		}
		return character
	}, seg)
	seg = strings.ToLower(strings.TrimRight(seg, ". "))
	short, _, tilde := strings.Cut(seg, "~")
	for _, name := range names {
		if strings.EqualFold(seg, name) || tilde && strings.HasPrefix(strings.ReplaceAll(name, ".", ""), short) {
			return true
		}
	}
	return false
}

// ideCodeRestore : le from d'une entrée de corbeille (lu par la passerelle, qui ne voit pas le corps du handler) est un chemin
// exécutable. Entrée illisible : false, le handler échoue lui aussi.
func ideCodeRestore(dir, trashID string) bool {
	if !ideTrashID.MatchString(trashID) || dir == "" || strings.ContainsRune(dir, 0) || !filepath.IsAbs(dir) {
		return false
	}
	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return false
	}
	defer root.Close()
	from, err := ideReadFrom(root, trashID)
	return err == nil && ideCodePath(from)
}

// ideCodeDenied : répond 403 et renvoie true si rel est un chemin exécutable et que l'appelant n'a pas config_write.
func ideCodeDenied(responseWriter http.ResponseWriter, request *http.Request, rels ...string) bool {
	hit := false
	for _, rel := range rels {
		hit = hit || ideCodePath(rel)
	}
	if !hit {
		return false
	}
	gate := gateFrom(request)
	if gate == nil || gate.p == nil {
		return false
	}
	if homeID, found := homeIDFor(homeFrom(request)); found {
		if rights, err := gate.rights(homeID); err == nil && rights[RightConfigWrite] {
			return false
		}
	}
	writeErr(responseWriter, 403, "forbidden", L("srv.auth.droitRefuse"))
	return true
}

func ideInTrash(rel string) bool {
	first, _, _ := strings.Cut(rel, "/")
	return ideSegIs(first, ideTrash)
}

// ideOpen ouvre la racine et valide un chemin non vide ; l'appelant ferme root.
func ideOpen(dir, requestedPath string) (*os.Root, string, error) {
	root, err := ideOpenRoot(dir)
	if err != nil {
		return nil, "", err
	}
	rel, err := ideRel(requestedPath, false)
	if err != nil {
		root.Close()
		return nil, "", err
	}
	return root, rel, nil
}

// ideConflict : 409 « conflict » (le fichier a changé) ou « deleted » (il a disparu) ; etag est celui du disque, vide si supprimé.
func ideConflict(responseWriter http.ResponseWriter, deleted bool, etag string) {
	code, key := "conflict", "srv.ide.conflit"
	if deleted {
		code, key = "deleted", "srv.ide.supprime"
	}
	writeErrExtra(responseWriter, 409, code, L(key), map[string]any{"etag": etag, "deleted": deleted})
}

// ideAtomicWrite : le temporaire reste dans le dossier cible pour que Rename ne change pas de volume.
// verify est rappelé juste avant le Rename pour réduire la fenêtre de mise à jour perdue.
func ideAtomicWrite(root *os.Root, rel string, data []byte, mode fs.FileMode, verify func() error) error {
	tempPath := path.Join(path.Dir(rel), ".gld-tmp-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	file, err := root.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	err = file.Chmod(mode)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil && verify != nil {
		ideBeforeRename()
		err = verify()
	}
	if err == nil {
		err = root.Rename(tempPath, rel)
	}
	if err != nil {
		root.Remove(tempPath)
	}
	return err
}

// ideTrashDir renvoie l'état du dossier corbeille : errIdeNotFound s'il n'existe pas encore.
func ideTrashDir(root *os.Root) error {
	fileInfo, err := ideLstat(root, ideTrash)
	if err != nil {
		return err
	}
	if !fileInfo.IsDir() {
		return errIdePath
	}
	return nil
}

func ideReadFrom(root *os.Root, trashID string) (string, error) {
	fromPath := ideTrash + "/" + trashID + "/from"
	fileInfo, err := ideLstat(root, fromPath)
	if err != nil {
		return "", err
	}
	if !fileInfo.Mode().IsRegular() {
		return "", errIdePath
	}
	file, err := root.Open(fromPath)
	if err != nil {
		return "", errIdeNotFound
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 4096))
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// ideMkParents crée les dossiers manquants un à un pour ne jamais traverser un lien.
func ideMkParents(root *os.Root, rel string) error {
	dir := path.Dir(rel)
	if dir == "." {
		return nil
	}
	parts := strings.Split(dir, "/")
	for partIndex := range parts {
		prefix := strings.Join(parts[:partIndex+1], "/")
		fileInfo, err := root.Lstat(prefix)
		switch {
		case err == nil && fileInfo.Mode()&fs.ModeSymlink != 0:
			return errIdeSymlink
		case err == nil && !fileInfo.IsDir():
			return errIdePath
		case err != nil && errors.Is(err, fs.ErrNotExist):
			if mkdirErr := root.Mkdir(prefix, 0o755); mkdirErr != nil {
				return mkdirErr
			}
		case err != nil:
			return err
		}
	}
	return nil
}

func ideTrashRoot(responseWriter http.ResponseWriter, dir, trashID string) (*os.Root, bool) {
	if !ideTrashID.MatchString(trashID) {
		ideFail(responseWriter, errIdePath)
		return nil, false
	}
	root, err := ideOpenRoot(dir)
	if err != nil {
		ideFail(responseWriter, err)
		return nil, false
	}
	if trashErr := ideTrashDir(root); trashErr != nil {
		root.Close()
		ideFail(responseWriter, trashErr)
		return nil, false
	}
	return root, true
}

// ideTrashMove range rel dans la corbeille du projet et renvoie l'identifiant de l'entrée.
func ideTrashMove(root *os.Root, rel string) (string, error) {
	if trashErr := ideTrashDir(root); errors.Is(trashErr, errIdeNotFound) {
		if mkdirErr := root.Mkdir(ideTrash, 0o755); mkdirErr != nil {
			return "", mkdirErr
		}
	} else if trashErr != nil {
		return "", trashErr
	}
	if gitignoreFile, gitignoreErr := root.OpenFile(ideTrash+"/.gitignore", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); gitignoreErr == nil {
		_, gitignoreWriteErr := gitignoreFile.WriteString("*\n")
		if closeErr := gitignoreFile.Close(); gitignoreWriteErr == nil {
			gitignoreWriteErr = closeErr
		}
		if gitignoreWriteErr != nil {
			return "", gitignoreWriteErr
		}
	}
	trashID := strconv.FormatInt(time.Now().UnixNano(), 10)
	base := ideTrash + "/" + trashID
	if entryMkdirErr := root.Mkdir(base, 0o755); entryMkdirErr != nil {
		return "", entryMkdirErr
	}
	fromFile, err := root.OpenFile(base+"/from", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		_, err = fromFile.WriteString(rel)
		if closeErr := fromFile.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = root.Rename(rel, base+"/item")
		}
	}
	if err != nil {
		root.Remove(base + "/from")
		root.Remove(base)
		return "", err
	}
	return trashID, nil
}

// Le plafond de corps couvre le pire cas d'échappement JSON (\u00XX, 6 octets par octet).
func routesIDEWrite(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/fs/entries/content", func(responseWriter http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		dir := query.Get("dir")
		var body struct {
			Content, Etag string
			Force         bool
		}
		if !ideBody(responseWriter, request, 6*ideMaxFile+64<<10, &body) {
			return
		}
		if len(body.Content) > ideMaxFile {
			ideFail(responseWriter, errIdeTooBig)
			return
		}
		root, rel, err := ideOpen(dir, query.Get("path"))
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer root.Close()
		if ideInTrash(rel) {
			ideFail(responseWriter, errIdePath)
			return
		}
		if ideCodeDenied(responseWriter, request, rel) {
			return
		}
		if parentErr := ideParentDir(root, rel); parentErr != nil {
			ideFail(responseWriter, parentErr)
			return
		}
		mode := fs.FileMode(0o644)
		info, err := ideLstat(root, rel)
		switch {
		case err == nil:
			if !info.Mode().IsRegular() {
				ideFail(responseWriter, errIdePath)
				return
			}
			if diskEtag := ideEtag(info); !body.Force && body.Etag != diskEtag {
				ideConflict(responseWriter, false, diskEtag)
				return
			}
			mode = info.Mode().Perm()
		case errors.Is(err, errIdeNotFound):
			if body.Etag != "" && !body.Force {
				ideConflict(responseWriter, true, "")
				return
			}
		default:
			ideFail(responseWriter, err)
			return
		}
		existed := info != nil
		var currentEtag string
		verify := func() error {
			if body.Force {
				return nil
			}
			currentInfo, lstatErr := root.Lstat(rel)
			switch {
			case lstatErr != nil && !existed && errors.Is(lstatErr, fs.ErrNotExist):
				return nil
			case lstatErr != nil && errors.Is(lstatErr, fs.ErrNotExist):
				currentEtag = ""
			case lstatErr != nil:
				return lstatErr
			case existed && currentInfo.Mode().IsRegular() && ideEtag(currentInfo) == body.Etag:
				return nil
			default:
				currentEtag = ideEtag(currentInfo)
			}
			return errIdeConflict
		}
		if atomicWriteErr := ideAtomicWrite(root, rel, []byte(body.Content), mode, verify); atomicWriteErr != nil {
			if errors.Is(atomicWriteErr, errIdeConflict) {
				ideConflict(responseWriter, existed && currentEtag == "", currentEtag)
				return
			}
			ideFail(responseWriter, atomicWriteErr)
			return
		}
		writtenInfo, err := root.Lstat(rel)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		operation := "save"
		if !existed {
			operation = "create"
		}
		ideAudit(responseWriter, request, operation, dir, rel, int64(len(body.Content)))
		writeJSON(responseWriter, 200, map[string]any{"etag": ideEtag(writtenInfo)})
	})

	mux.HandleFunc("POST /v1/fs/entries", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir := request.URL.Query().Get("dir")
		var body struct{ Path, Kind string }
		if !ideBody(responseWriter, request, 64<<10, &body) {
			return
		}
		if body.Kind != "file" && body.Kind != "dir" {
			ideFail(responseWriter, errIdePath)
			return
		}
		root, rel, err := ideOpen(dir, body.Path)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer root.Close()
		if ideInTrash(rel) {
			ideFail(responseWriter, errIdePath)
			return
		}
		if ideCodeDenied(responseWriter, request, rel) {
			return
		}
		if parentErr := ideParentDir(root, rel); parentErr != nil {
			ideFail(responseWriter, parentErr)
			return
		}
		if ideExists(root, rel) {
			ideFail(responseWriter, errIdeExists)
			return
		}
		if body.Kind == "dir" {
			err = root.Mkdir(rel, 0o755)
		} else {
			var file *os.File
			if file, err = root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err == nil {
				err = file.Close()
			}
		}
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		ideAudit(responseWriter, request, "new", dir, rel, -1)
		writeJSON(responseWriter, 201, map[string]any{"path": rel})
	})

	mux.HandleFunc("PATCH /v1/fs/entries", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir := request.URL.Query().Get("dir")
		var body struct{ From, To string }
		if !ideBody(responseWriter, request, 64<<10, &body) {
			return
		}
		root, from, err := ideOpen(dir, body.From)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer root.Close()
		to, err := ideRel(body.To, false)
		if err != nil || ideInTrash(from) || ideInTrash(to) {
			ideFail(responseWriter, errIdePath)
			return
		}
		if ideCodeDenied(responseWriter, request, from, to) {
			return
		}
		if _, sourceErr := ideLstat(root, from); sourceErr != nil {
			ideFail(responseWriter, sourceErr)
			return
		}
		if parentErr := ideParentDir(root, to); parentErr != nil {
			ideFail(responseWriter, parentErr)
			return
		}
		// Le test d'existence puis le Rename restent séparés : os.Root n'offre pas de renommage sans écrasement.
		if ideExists(root, to) {
			ideFail(responseWriter, errIdeExists)
			return
		}
		if renameErr := root.Rename(from, to); renameErr != nil {
			ideFail(responseWriter, renameErr)
			return
		}
		ideAudit(responseWriter, request, "rename", dir, from+" -> "+to, -1)
		responseWriter.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/fs/entries", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir := request.URL.Query().Get("dir")
		paths := request.URL.Query()["item"]
		if len(paths) == 0 {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		if len(paths) > itemMax {
			writeErr(responseWriter, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
			return
		}
		if ideCodeDenied(responseWriter, request, paths...) {
			return
		}
		root, err := ideOpenRoot(dir)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer root.Close()
		var result bulk
		var rels []string
		for _, requestedPath := range paths {
			rel, itemErr := ideRel(requestedPath, false)
			if itemErr == nil && ideInTrash(rel) {
				itemErr = errIdePath
			}
			if itemErr == nil {
				_, itemErr = ideLstat(root, rel)
			}
			if itemErr == nil {
				var trashID string
				if trashID, itemErr = ideTrashMove(root, rel); itemErr == nil {
					result.ok(trashID)
					rels = append(rels, rel)
					continue
				}
			}
			status, code, message := ideErr(itemErr)
			result.fail(requestedPath, status, code, message)
		}
		if len(rels) > 0 {
			ideAudit(responseWriter, request, "rm", dir, strings.Join(rels, ","), -1)
		}
		result.reply(responseWriter, "")
	})

	mux.HandleFunc("GET /v1/fs/trash", func(responseWriter http.ResponseWriter, request *http.Request) {
		root, err := ideOpenRoot(request.URL.Query().Get("dir"))
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer root.Close()
		items := []map[string]any{}
		err = ideTrashDir(root)
		if err != nil && !errors.Is(err, errIdeNotFound) {
			ideFail(responseWriter, err)
			return
		}
		if err == nil {
			trashDirectory, listErr := root.Open(ideTrash)
			if listErr != nil {
				ideFail(responseWriter, listErr)
				return
			}
			dirEntries, listErr := trashDirectory.ReadDir(-1)
			trashDirectory.Close()
			if listErr != nil {
				ideFail(responseWriter, listErr)
				return
			}
			for _, dirEntry := range dirEntries {
				if !dirEntry.IsDir() || !ideTrashID.MatchString(dirEntry.Name()) {
					continue
				}
				from, readFromErr := ideReadFrom(root, dirEntry.Name())
				if readFromErr != nil {
					continue
				}
				var modifiedTime int64
				if info, infoErr := dirEntry.Info(); infoErr == nil {
					modifiedTime = info.ModTime().Unix()
				}
				items = append(items, map[string]any{"id": dirEntry.Name(), "path": from, "mtime": modifiedTime})
			}
			sort.Slice(items, func(leftIndex, rightIndex int) bool {
				left, right := items[leftIndex]["id"].(string), items[rightIndex]["id"].(string)
				if len(left) != len(right) {
					return len(left) > len(right)
				}
				return left > right
			})
		}
		writeJSON(responseWriter, 200, map[string]any{"items": items})
	})

	mux.HandleFunc("PATCH /v1/fs/trash/{entry}", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir, trashID := request.URL.Query().Get("dir"), request.PathValue("entry")
		var body struct{ Trashed *bool }
		if !ideBody(responseWriter, request, 64<<10, &body) {
			return
		}
		if body.Trashed == nil || *body.Trashed {
			ideFail(responseWriter, errIdePath)
			return
		}
		root, opened := ideTrashRoot(responseWriter, dir, trashID)
		if !opened {
			return
		}
		defer root.Close()
		base := ideTrash + "/" + trashID
		fromRaw, err := ideReadFrom(root, trashID)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		from, err := ideRel(fromRaw, false)
		if err != nil || ideInTrash(from) {
			ideFail(responseWriter, errIdePath)
			return
		}
		if ideCodeDenied(responseWriter, request, from) {
			return
		}
		if _, itemErr := ideLstat(root, base+"/item"); itemErr != nil {
			ideFail(responseWriter, itemErr)
			return
		}
		// Le test d'existence puis le Rename restent séparés : os.Root n'offre pas de renommage sans écrasement.
		if ideExists(root, from) {
			ideFail(responseWriter, errIdeExists)
			return
		}
		if mkdirErr := ideMkParents(root, from); mkdirErr != nil {
			ideFail(responseWriter, mkdirErr)
			return
		}
		if renameErr := root.Rename(base+"/item", from); renameErr != nil {
			ideFail(responseWriter, renameErr)
			return
		}
		root.Remove(base + "/from")
		root.Remove(base)
		ideAudit(responseWriter, request, "restore", dir, from, -1)
		responseWriter.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/fs/trash", func(responseWriter http.ResponseWriter, request *http.Request) {
		dir := request.URL.Query().Get("dir")
		items := request.URL.Query()["item"]
		if len(items) == 0 {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		if len(items) > itemMax {
			writeErr(responseWriter, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
			return
		}
		root, err := ideOpenRoot(dir)
		if err == nil {
			err = ideTrashDir(root)
			if err != nil {
				root.Close()
			}
		}
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		defer root.Close()
		var result bulk
		purged := make([]string, 0, len(items))
		for _, trashID := range items {
			base := ideTrash + "/" + trashID
			var itemErr error
			if !ideTrashID.MatchString(trashID) {
				itemErr = errIdePath
			} else if _, itemErr = ideLstat(root, base); itemErr == nil {
				what, _ := ideReadFrom(root, trashID)
				if what == "" {
					what = base
				}
				if itemErr = root.RemoveAll(base); itemErr == nil {
					purged = append(purged, what)
					result.ok(trashID)
					continue
				}
			}
			status, code, message := ideErr(itemErr)
			result.fail(trashID, status, code, message)
		}
		if len(purged) > 0 {
			ideAudit(responseWriter, request, "purge", dir, strings.Join(purged, ","), -1)
		}
		result.reply(responseWriter, "")
	})
}
