package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
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

// ideBody décode le JSON ; un corps au-delà de max devient errIdeTooBig.
func ideBody(w http.ResponseWriter, r *http.Request, max int64, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, max)).Decode(v); err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			ideFail(w, errIdeTooBig)
		} else {
			ideFail(w, errIdePath)
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
	fi, err := ideLstat(root, dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
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
		seg, _, _ = strings.Cut(seg, ":")
		seg = strings.Map(func(r rune) rune {
			if r >= 0x200c && r <= 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x206a && r <= 0x206f || r == 0xfeff {
				return -1
			}
			return r
		}, seg)
		seg = strings.ToLower(strings.TrimRight(seg, ". "))
		short, _, tilde := strings.Cut(seg, "~")
		for _, n := range []string{".git", ".claude", ".mcp.json", ".gitattributes", ".gitmodules"} {
			if strings.EqualFold(seg, n) || tilde && strings.HasPrefix(strings.ReplaceAll(n, ".", ""), short) {
				return true
			}
		}
	}
	return false
}

// ideCodeDenied : répond 403 et renvoie true si rel est un chemin exécutable et que l'appelant n'a pas config_write.
func ideCodeDenied(w http.ResponseWriter, r *http.Request, rels ...string) bool {
	hit := false
	for _, rel := range rels {
		hit = hit || ideCodePath(rel)
	}
	if !hit {
		return false
	}
	g := gateFrom(r)
	if g == nil || g.p == nil {
		return false
	}
	if id, ok := homeIDFor(homeFrom(r)); ok {
		if m, err := g.rights(id); err == nil && m[RightConfigWrite] {
			return false
		}
	}
	writeErr(w, 403, "forbidden", L("srv.auth.droitRefuse"))
	return true
}

func ideInTrash(rel string) bool { return rel == ideTrash || strings.HasPrefix(rel, ideTrash+"/") }

// ideOpen ouvre la racine et valide un chemin non vide ; l'appelant ferme root.
func ideOpen(dir, p string) (*os.Root, string, error) {
	root, err := ideOpenRoot(dir)
	if err != nil {
		return nil, "", err
	}
	rel, err := ideRel(p, false)
	if err != nil {
		root.Close()
		return nil, "", err
	}
	return root, rel, nil
}

// ideConflict : 409 « conflict » (le fichier a changé) ou « deleted » (il a disparu) ; etag est celui du disque, vide si supprimé.
func ideConflict(w http.ResponseWriter, deleted bool, etag string) {
	code, key := "conflict", "srv.ide.conflit"
	if deleted {
		code, key = "deleted", "srv.ide.supprime"
	}
	writeErrExtra(w, 409, code, L(key), map[string]any{"etag": etag, "deleted": deleted})
}

// ideAtomicWrite : le temporaire reste dans le dossier cible pour que Rename ne change pas de volume.
// verify est rappelé juste avant le Rename pour réduire la fenêtre de mise à jour perdue.
func ideAtomicWrite(root *os.Root, rel string, data []byte, mode fs.FileMode, verify func() error) error {
	tmp := path.Join(path.Dir(rel), ".gld-tmp-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	err = f.Chmod(mode)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && verify != nil {
		ideBeforeRename()
		err = verify()
	}
	if err == nil {
		err = root.Rename(tmp, rel)
	}
	if err != nil {
		root.Remove(tmp)
	}
	return err
}

// ideTrashDir renvoie l'état du dossier corbeille : errIdeNotFound s'il n'existe pas encore.
func ideTrashDir(root *os.Root) error {
	fi, err := ideLstat(root, ideTrash)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return errIdePath
	}
	return nil
}

func ideReadFrom(root *os.Root, id string) (string, error) {
	p := ideTrash + "/" + id + "/from"
	fi, err := ideLstat(root, p)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", errIdePath
	}
	f, err := root.Open(p)
	if err != nil {
		return "", errIdeNotFound
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ideMkParents crée les dossiers manquants un à un pour ne jamais traverser un lien.
func ideMkParents(root *os.Root, rel string) error {
	dir := path.Dir(rel)
	if dir == "." {
		return nil
	}
	parts := strings.Split(dir, "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		fi, err := root.Lstat(p)
		switch {
		case err == nil && fi.Mode()&fs.ModeSymlink != 0:
			return errIdeSymlink
		case err == nil && !fi.IsDir():
			return errIdePath
		case err != nil && errors.Is(err, fs.ErrNotExist):
			if err := root.Mkdir(p, 0o755); err != nil {
				return err
			}
		case err != nil:
			return err
		}
	}
	return nil
}

func ideTrashRoot(w http.ResponseWriter, dir, id string) (*os.Root, bool) {
	if !ideTrashID.MatchString(id) {
		ideFail(w, errIdePath)
		return nil, false
	}
	root, err := ideOpenRoot(dir)
	if err != nil {
		ideFail(w, err)
		return nil, false
	}
	if err := ideTrashDir(root); err != nil {
		root.Close()
		ideFail(w, err)
		return nil, false
	}
	return root, true
}

// ideTrashMove range rel dans la corbeille du projet et renvoie l'identifiant de l'entrée.
func ideTrashMove(root *os.Root, rel string) (string, error) {
	if err := ideTrashDir(root); errors.Is(err, errIdeNotFound) {
		if err := root.Mkdir(ideTrash, 0o755); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	if f, err := root.OpenFile(ideTrash+"/.gitignore", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err == nil {
		_, werr := f.WriteString("*\n")
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return "", werr
		}
	}
	id := strconv.FormatInt(time.Now().UnixNano(), 10)
	base := ideTrash + "/" + id
	if err := root.Mkdir(base, 0o755); err != nil {
		return "", err
	}
	f, err := root.OpenFile(base+"/from", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		_, err = f.WriteString(rel)
		if cerr := f.Close(); err == nil {
			err = cerr
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
	return id, nil
}

// Le plafond de corps couvre le pire cas d'échappement JSON (\u00XX, 6 octets par octet).
func routesIDEWrite(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/fs/entries/content", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		dir := q.Get("dir")
		var req struct {
			Content, Etag string
			Force         bool
		}
		if !ideBody(w, r, 6*ideMaxFile+64<<10, &req) {
			return
		}
		if len(req.Content) > ideMaxFile {
			ideFail(w, errIdeTooBig)
			return
		}
		root, rel, err := ideOpen(dir, q.Get("path"))
		if err != nil {
			ideFail(w, err)
			return
		}
		defer root.Close()
		if ideInTrash(rel) {
			ideFail(w, errIdePath)
			return
		}
		if ideCodeDenied(w, r, rel) {
			return
		}
		if err := ideParentDir(root, rel); err != nil {
			ideFail(w, err)
			return
		}
		mode := fs.FileMode(0o644)
		fi, err := ideLstat(root, rel)
		switch {
		case err == nil:
			if !fi.Mode().IsRegular() {
				ideFail(w, errIdePath)
				return
			}
			if cur := ideEtag(fi); !req.Force && req.Etag != cur {
				ideConflict(w, false, cur)
				return
			}
			mode = fi.Mode().Perm()
		case errors.Is(err, errIdeNotFound):
			if req.Etag != "" && !req.Force {
				ideConflict(w, true, "")
				return
			}
		default:
			ideFail(w, err)
			return
		}
		existed := fi != nil
		var cur string
		verify := func() error {
			if req.Force {
				return nil
			}
			nfi, err := root.Lstat(rel)
			switch {
			case err != nil && !existed && errors.Is(err, fs.ErrNotExist):
				return nil
			case err != nil && errors.Is(err, fs.ErrNotExist):
				cur = ""
			case err != nil:
				return err
			case existed && nfi.Mode().IsRegular() && ideEtag(nfi) == req.Etag:
				return nil
			default:
				cur = ideEtag(nfi)
			}
			return errIdeConflict
		}
		if err := ideAtomicWrite(root, rel, []byte(req.Content), mode, verify); err != nil {
			if errors.Is(err, errIdeConflict) {
				ideConflict(w, existed && cur == "", cur)
				return
			}
			ideFail(w, err)
			return
		}
		nfi, err := root.Lstat(rel)
		if err != nil {
			ideFail(w, err)
			return
		}
		op := "save"
		if !existed {
			op = "create"
		}
		ideAudit(w, r, op, dir, rel, int64(len(req.Content)))
		writeJSON(w, 200, map[string]any{"etag": ideEtag(nfi)})
	})

	mux.HandleFunc("POST /v1/fs/entries", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		var req struct{ Path, Kind string }
		if !ideBody(w, r, 64<<10, &req) {
			return
		}
		if req.Kind != "file" && req.Kind != "dir" {
			ideFail(w, errIdePath)
			return
		}
		root, rel, err := ideOpen(dir, req.Path)
		if err != nil {
			ideFail(w, err)
			return
		}
		defer root.Close()
		if ideInTrash(rel) {
			ideFail(w, errIdePath)
			return
		}
		if ideCodeDenied(w, r, rel) {
			return
		}
		if err := ideParentDir(root, rel); err != nil {
			ideFail(w, err)
			return
		}
		if ideExists(root, rel) {
			ideFail(w, errIdeExists)
			return
		}
		if req.Kind == "dir" {
			err = root.Mkdir(rel, 0o755)
		} else {
			var f *os.File
			if f, err = root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err == nil {
				err = f.Close()
			}
		}
		if err != nil {
			ideFail(w, err)
			return
		}
		ideAudit(w, r, "new", dir, rel, -1)
		writeJSON(w, 201, map[string]any{"path": rel})
	})

	mux.HandleFunc("PATCH /v1/fs/entries", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		var req struct{ From, To string }
		if !ideBody(w, r, 64<<10, &req) {
			return
		}
		root, from, err := ideOpen(dir, req.From)
		if err != nil {
			ideFail(w, err)
			return
		}
		defer root.Close()
		to, err := ideRel(req.To, false)
		if err != nil || ideInTrash(from) || ideInTrash(to) {
			ideFail(w, errIdePath)
			return
		}
		if ideCodeDenied(w, r, from, to) {
			return
		}
		if _, err := ideLstat(root, from); err != nil {
			ideFail(w, err)
			return
		}
		if err := ideParentDir(root, to); err != nil {
			ideFail(w, err)
			return
		}
		// Le test d'existence puis le Rename restent séparés : os.Root n'offre pas de renommage sans écrasement.
		if ideExists(root, to) {
			ideFail(w, errIdeExists)
			return
		}
		if err := root.Rename(from, to); err != nil {
			ideFail(w, err)
			return
		}
		ideAudit(w, r, "rename", dir, from+" -> "+to, -1)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/fs/entries", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		paths := r.URL.Query()["item"]
		if len(paths) == 0 {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		if len(paths) > itemMax {
			writeErr(w, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
			return
		}
		root, err := ideOpenRoot(dir)
		if err != nil {
			ideFail(w, err)
			return
		}
		defer root.Close()
		var b bulk
		var rels []string
		for _, p := range paths {
			rel, err := ideRel(p, false)
			if err == nil && ideInTrash(rel) {
				err = errIdePath
			}
			if err == nil {
				_, err = ideLstat(root, rel)
			}
			if err == nil {
				var id string
				if id, err = ideTrashMove(root, rel); err == nil {
					b.ok(id)
					rels = append(rels, rel)
					continue
				}
			}
			st, code, msg := ideErr(err)
			b.fail(p, st, code, msg)
		}
		if len(rels) > 0 {
			ideAudit(w, r, "rm", dir, strings.Join(rels, ","), -1)
		}
		b.reply(w, "")
	})

	mux.HandleFunc("GET /v1/fs/trash", func(w http.ResponseWriter, r *http.Request) {
		root, err := ideOpenRoot(r.URL.Query().Get("dir"))
		if err != nil {
			ideFail(w, err)
			return
		}
		defer root.Close()
		items := []map[string]any{}
		err = ideTrashDir(root)
		if err != nil && !errors.Is(err, errIdeNotFound) {
			ideFail(w, err)
			return
		}
		if err == nil {
			d, err := root.Open(ideTrash)
			if err != nil {
				ideFail(w, err)
				return
			}
			des, err := d.ReadDir(-1)
			d.Close()
			if err != nil {
				ideFail(w, err)
				return
			}
			for _, de := range des {
				if !de.IsDir() || !ideTrashID.MatchString(de.Name()) {
					continue
				}
				from, err := ideReadFrom(root, de.Name())
				if err != nil {
					continue
				}
				var mt int64
				if info, err := de.Info(); err == nil {
					mt = info.ModTime().Unix()
				}
				items = append(items, map[string]any{"id": de.Name(), "path": from, "mtime": mt})
			}
			sort.Slice(items, func(i, j int) bool {
				a, b := items[i]["id"].(string), items[j]["id"].(string)
				if len(a) != len(b) {
					return len(a) > len(b)
				}
				return a > b
			})
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})

	mux.HandleFunc("PATCH /v1/fs/trash/{entry}", func(w http.ResponseWriter, r *http.Request) {
		dir, id := r.URL.Query().Get("dir"), r.PathValue("entry")
		var req struct{ Trashed *bool }
		if !ideBody(w, r, 64<<10, &req) {
			return
		}
		if req.Trashed == nil || *req.Trashed {
			ideFail(w, errIdePath)
			return
		}
		root, ok := ideTrashRoot(w, dir, id)
		if !ok {
			return
		}
		defer root.Close()
		base := ideTrash + "/" + id
		fromRaw, err := ideReadFrom(root, id)
		if err != nil {
			ideFail(w, err)
			return
		}
		from, err := ideRel(fromRaw, false)
		if err != nil || ideInTrash(from) {
			ideFail(w, errIdePath)
			return
		}
		if _, err := ideLstat(root, base+"/item"); err != nil {
			ideFail(w, err)
			return
		}
		// Le test d'existence puis le Rename restent séparés : os.Root n'offre pas de renommage sans écrasement.
		if ideExists(root, from) {
			ideFail(w, errIdeExists)
			return
		}
		if err := ideMkParents(root, from); err != nil {
			ideFail(w, err)
			return
		}
		if err := root.Rename(base+"/item", from); err != nil {
			ideFail(w, err)
			return
		}
		root.Remove(base + "/from")
		root.Remove(base)
		ideAudit(w, r, "restore", dir, from, -1)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/fs/trash", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		items := r.URL.Query()["item"]
		if len(items) == 0 {
			writeErr(w, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		if len(items) > itemMax {
			writeErr(w, http.StatusBadRequest, "too_many_items", L("srv.http_util.tropDElements"))
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
			ideFail(w, err)
			return
		}
		defer root.Close()
		var b bulk
		whats := make([]string, 0, len(items))
		for _, id := range items {
			base := ideTrash + "/" + id
			var err error
			if !ideTrashID.MatchString(id) {
				err = errIdePath
			} else if _, err = ideLstat(root, base); err == nil {
				what, _ := ideReadFrom(root, id)
				if what == "" {
					what = base
				}
				if err = root.RemoveAll(base); err == nil {
					whats = append(whats, what)
					b.ok(id)
					continue
				}
			}
			st, code, msg := ideErr(err)
			b.fail(id, st, code, msg)
		}
		if len(whats) > 0 {
			ideAudit(w, r, "purge", dir, strings.Join(whats, ","), -1)
		}
		b.reply(w, "")
	})
}
