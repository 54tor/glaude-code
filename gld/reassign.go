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

// remapCwd : old devient nw. Ses sous-dossiers ne suivent que si sub (l'ancien dossier a disparu : projet
// renommé ou déplacé) et s'ils ne sont pas déjà dans nw — sinon ce sont de vrais chemins, laissés tels quels.
func remapCwd(v, old, nw string, sub bool) string {
	sep := string(filepath.Separator)
	if v == old {
		return nw
	}
	if sub && strings.HasPrefix(v, old+sep) && v != nw && !strings.HasPrefix(v, nw+sep) {
		return nw + v[len(old):]
	}
	return v
}

// cwdRewrite : un transcript réécrit dans path.tmp, en attente du renommage qui remplace l'original.
type cwdRewrite struct {
	path string
	mod  time.Time
}

// commit remplace l'original par sa réécriture, en gardant la date de modification.
func (c cwdRewrite) commit() error {
	r, rel, err := confine(cfgDir(), c.path)
	if err != nil {
		return err
	}
	defer r.Close()
	if err = r.Rename(rel+".tmp", rel); err == nil {
		forgetInfo(c.path)
		err = r.Chtimes(rel, c.mod, c.mod)
	}
	return err
}

func (c cwdRewrite) abort() { rRemove(cfgDir(), c.path+".tmp") }

// prepareCwd écrit dans path.tmp le transcript au cwd remplacé (voir remapCwd) ; commit le met en place.
func prepareCwd(path, old, nw string, sub bool) (cwdRewrite, error) {
	cwdKey := []byte(`"cwd":`)
	remap := func(m []byte) []byte {
		var v string
		if json.Unmarshal(m[len(cwdKey):], &v) != nil {
			return m
		}
		nv := remapCwd(v, old, nw, sub)
		if nv == v {
			return m
		}
		q, _ := json.Marshal(nv)
		return append(append([]byte{}, cwdKey...), q...)
	}
	in, err := rOpenFile(cfgDir(), path, os.O_RDONLY, 0)
	if err != nil {
		return cwdRewrite{}, err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return cwdRewrite{}, err
	}
	tmp := path + ".tmp"
	if fi, err := os.Lstat(tmp); err == nil && !fi.IsDir() {
		rRemove(cfgDir(), tmp) // un reste d'un essai interrompu, ou un lien posé à sa place : retiré, jamais suivi
	}
	out, err := rOpenFile(cfgDir(), tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return cwdRewrite{}, err
	}
	if err := out.Chmod(st.Mode().Perm()); err != nil {
		out.Close()
		rRemove(cfgDir(), tmp)
		return cwdRewrite{}, err
	}
	w := bufio.NewWriterSize(out, 1<<20)
	r := bufio.NewReaderSize(in, 1<<20)
	for {
		b, rerr := r.ReadBytes('\n')
		if bytes.Contains(b, cwdKey) {
			b = cwdRe.ReplaceAllFunc(b, remap)
		}
		w.Write(b)
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				err = rerr
			}
			break
		}
	}
	if err == nil {
		err = w.Flush()
	}
	if err == nil {
		err = out.Sync() // le transcript réécrit remplace l'original : il doit être sur disque avant le renommage
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		rRemove(cfgDir(), tmp)
		return cwdRewrite{}, err
	}
	return cwdRewrite{path, st.ModTime()}, nil
}

// reassign déplace des sessions vers le dossier de transcripts de `to` (un dossier existant)
// et y réécrit leur cwd. Tout est vérifié avant le premier déplacement.
func reassign(items []item, to string) (int, error) {
	if !filepath.IsAbs(to) {
		return 0, terr("srv.routes_sessions.cheminAbsoluRequis")
	}
	to = filepath.Clean(to)
	if fi, err := os.Stat(to); err != nil || !fi.IsDir() {
		return 0, terr("srv.reassign.leDossierCibleN", "A1", to)
	}
	dst := filepath.Join(root, nonAlnum.ReplaceAllString(to, "-"))
	live, err := liveCheck()
	if err != nil {
		return 0, err
	}
	for _, it := range items {
		if live[it.ID] {
			return 0, terr("srv.reassign.sessionVivanteFermeLa", "A1", it.ID[:8])
		}
		if filepath.Join(root, it.Dir) == dst {
			return 0, terr("srv.reassign.dejaDansCeDossier", "A1", it.ID[:8])
		}
		// Seul le transcript en conflit bloque : un dossier ID déjà là (reste d'un /cd interrompu) est fusionné.
		if n := it.ID + ".jsonl"; lexists(filepath.Join(dst, n)) {
			return 0, terr("srv.reassign.existeDejaDansLa", "A1", n)
		}
	}
	// Toutes les réécritures (transcripts et sous-agents de chaque session) sont écrites avant le premier renommage : un
	// échec en cours de route ne laisse pas une session à moitié réécrite.
	pending := make([][]cwdRewrite, len(items))
	abort := func() {
		for _, l := range pending {
			for _, c := range l {
				c.abort()
			}
		}
	}
	for i, it := range items {
		src := filepath.Join(root, it.Dir)
		jl := filepath.Join(src, it.ID+".jsonl")
		st, err := os.Stat(jl)
		if err != nil {
			continue
		}
		old := info(jl, st).Cwd
		if old == "" || old == to {
			continue
		}
		_, gone := os.Stat(old)
		files := []string{jl}
		filepath.WalkDir(filepath.Join(src, it.ID), func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() && strings.HasSuffix(p, ".jsonl") {
				files = append(files, p)
			}
			return nil
		})
		for _, f := range files {
			c, err := prepareCwd(f, old, to, gone != nil)
			if err != nil {
				abort()
				return 0, err
			}
			pending[i] = append(pending[i], c)
		}
	}
	if err := rMkdirAll(cfgDir(), dst, 0o755); err != nil {
		abort()
		return 0, err
	}
	for i, it := range items {
		for _, c := range pending[i] {
			if err := c.commit(); err != nil {
				abort()
				return i, err
			}
			pending[i] = pending[i][1:]
		}
		src := filepath.Join(root, it.Dir)
		// Le dossier d'abord, le transcript en dernier : c'est lui qui rend la session visible, donc un échec
		// intermédiaire ne laisse jamais une session apparente à moitié déplacée.
		for _, n := range []string{it.ID, it.ID + ".jsonl"} {
			if err := mergeMove(filepath.Join(src, n), filepath.Join(dst, n)); err != nil && !os.IsNotExist(err) {
				abort()
				return i, err
			}
		}
		rRemove(cfgDir(), src) // seulement s'il est devenu vide
	}
	return len(items), nil
}

func lexists(p string) bool { _, err := os.Lstat(p); return err == nil }

// mergeMove déplace from vers to. Si to existe déjà, deux dossiers se fusionnent enfant par enfant ; pour un fichier en
// conflit la version de to l'emporte et celle de from reste en place (rien n'est perdu, le dossier source n'est alors
// pas retiré).
func mergeMove(from, to string) error {
	fi, err := os.Lstat(from)
	if err != nil {
		return err
	}
	ti, err := os.Lstat(to)
	if os.IsNotExist(err) {
		return rRename(cfgDir(), from, to)
	}
	if err != nil || !fi.IsDir() || !ti.IsDir() {
		return err
	}
	ents, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if err := mergeMove(filepath.Join(from, e.Name()), filepath.Join(to, e.Name())); err != nil {
			return err
		}
	}
	rRemove(cfgDir(), from) // seulement s'il est devenu vide
	return nil
}
