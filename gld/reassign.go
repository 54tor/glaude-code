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

// rewriteCwd remplace le cwd (voir remapCwd) dans un transcript, en gardant la date de modification.
func rewriteCwd(path, old, nw string, sub bool) error {
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
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := path + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode())
	if err != nil {
		return err
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
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, path); err == nil {
		forgetInfo(path)
		err = os.Chtimes(path, st.ModTime(), st.ModTime())
	}
	return err
}

// reassign déplace des sessions vers le dossier de transcripts de `to` (un dossier existant)
// et y réécrit leur cwd. Tout est vérifié avant le premier déplacement.
func reassign(items []item, to string) (int, error) {
	if !filepath.IsAbs(to) {
		return 0, errors.New("chemin absolu requis")
	}
	to = filepath.Clean(to)
	if fi, err := os.Stat(to); err != nil || !fi.IsDir() {
		return 0, errors.New("le dossier cible n'existe pas : " + to)
	}
	dst := filepath.Join(root, nonAlnum.ReplaceAllString(to, "-"))
	live, err := liveCheck()
	if err != nil {
		return 0, err
	}
	for _, it := range items {
		if live[it.ID] {
			return 0, errors.New("session vivante, ferme-la d'abord : " + it.ID[:8])
		}
		if filepath.Join(root, it.Dir) == dst {
			return 0, errors.New("déjà dans ce dossier : " + it.ID[:8])
		}
		for _, n := range []string{it.ID + ".jsonl", it.ID} {
			if _, err := os.Lstat(filepath.Join(dst, n)); err == nil {
				return 0, errors.New("existe déjà dans la cible : " + n)
			}
		}
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	for _, it := range items {
		src := filepath.Join(root, it.Dir)
		jl := filepath.Join(src, it.ID+".jsonl")
		if st, err := os.Stat(jl); err == nil {
			if old := info(jl, st).Cwd; old != "" && old != to {
				_, gone := os.Stat(old)
				files := []string{jl}
				filepath.WalkDir(filepath.Join(src, it.ID), func(p string, e os.DirEntry, err error) error {
					if err == nil && !e.IsDir() && strings.HasSuffix(p, ".jsonl") {
						files = append(files, p)
					}
					return nil
				})
				for _, f := range files {
					if err := rewriteCwd(f, old, to, gone != nil); err != nil {
						return 0, err
					}
				}
			}
		}
		for _, n := range []string{it.ID + ".jsonl", it.ID} {
			if err := os.Rename(filepath.Join(src, n), filepath.Join(dst, n)); err != nil && !os.IsNotExist(err) {
				return 0, err
			}
		}
		os.Remove(src) // seulement s'il est devenu vide
	}
	return len(items), nil
}
