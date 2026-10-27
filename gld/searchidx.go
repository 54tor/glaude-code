// Index de recherche plein texte (SQLite FTS5, pur Go) : classement BM25, préfixes, accents ignorés, « phrases ».
// Un message = une ligne ; l'index est mis à jour à la demande (seuls les transcripts modifiés sont relus).
package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	idxFile = func() string { return idxFileFor(root) } // remplacé par les tests
	idxMu   sync.Mutex                                  // un seul synchroniseur à la fois
	idxDB   *sql.DB
	idxNow  = time.Now // remplacé par les tests
)

// idxFileFor : un index par dossier de transcripts. Le gateway et chaque processus enfant (un par home) partagent le même
// cache utilisateur ; un fichier commun laisserait la recherche d'un home lire les extraits d'un autre.
func idxFileFor(root string) string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return filepath.Join(d, "gld-web", "search-"+hex.EncodeToString(sum[:6])+".db")
}

// indexSettle : un transcript déjà indexé et modifié depuis moins longtemps n'est pas relu. Une session vivante écrit en
// continu, et relire un gros transcript à chaque frappe de recherche coûtait plusieurs secondes.
const indexSettle = 30 * time.Second

const idxSchema = `
CREATE TABLE IF NOT EXISTS files(id INTEGER PRIMARY KEY, path TEXT UNIQUE, mod INTEGER, size INTEGER);
CREATE VIRTUAL TABLE IF NOT EXISTS msgs USING fts5(body, dir UNINDEXED, sid UNINDEXED, tokenize='unicode61 remove_diacritics 2');`

// rowid d'un message = fichier × 2^20 + rang : supprimer un fichier = supprimer une plage de rowid.
const perFile = 1 << 20

func idxOpen() (*sql.DB, error) {
	if idxDB != nil {
		return idxDB, nil
	}
	f := idxFile()
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(f)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err == nil {
		_, err = db.Exec(idxSchema)
	}
	if err != nil {
		return nil, err
	}
	idxDB = db
	return db, nil
}

// syncIndex met l'index d'accord avec les transcripts : ajoute les nouveaux, relit les modifiés, oublie les disparus.
func syncIndex() error {
	idxMu.Lock()
	defer idxMu.Unlock()
	db, err := idxOpen()
	if err != nil {
		return err
	}
	type rec struct {
		id        int64
		mod, size int64
	}
	known := map[string]rec{}
	rows, err := db.Query(`SELECT id, path, mod, size FROM files`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p string
		var r rec
		if err := rows.Scan(&r.id, &p, &r.mod, &r.size); err != nil {
			rows.Close()
			return err
		}
		known[p] = r
	}
	rows.Close()

	files, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	seen := map[string]bool{}
	for _, p := range files {
		seen[p] = true
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		r, ok := known[p]
		if ok && r.mod == st.ModTime().UnixNano() && r.size == st.Size() {
			continue
		}
		if ok && idxNow().Sub(st.ModTime()) < indexSettle {
			continue
		}
		if err := indexFile(db, p, st, r.id); err != nil {
			return err
		}
	}
	for p, r := range known {
		if !seen[p] {
			if err := dropFile(db, r.id); err != nil {
				return err
			}
		}
	}
	return nil
}

func dropFile(db *sql.DB, id int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM msgs WHERE rowid >= ? AND rowid < ?`, id*perFile, (id+1)*perFile); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// indexFile (ré)indexe un transcript ; id = 0 pour un fichier inconnu.
func indexFile(db *sql.DB, path string, st os.FileInfo, id int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if id == 0 {
		res, err := tx.Exec(`INSERT INTO files(path, mod, size) VALUES(?, ?, ?)`, path, st.ModTime().UnixNano(), st.Size())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
	} else {
		if _, err := tx.Exec(`DELETE FROM msgs WHERE rowid >= ? AND rowid < ?`, id*perFile, (id+1)*perFile); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE files SET mod = ?, size = ? WHERE id = ?`, st.ModTime().UnixNano(), st.Size(), id); err != nil {
			return err
		}
	}
	ins, err := tx.Prepare(`INSERT INTO msgs(rowid, body, dir, sid) VALUES(?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ins.Close()
	dir, sid := filepath.Base(filepath.Dir(path)), strings.TrimSuffix(filepath.Base(path), ".jsonl")
	n := int64(0)
	var ierr error
	_ = scan(path, func(b []byte) {
		for _, m := range lineMsgs(b, false) {
			if ierr == nil && n < perFile {
				_, ierr = ins.Exec(id*perFile+n, m.T, dir, sid)
				n++
			}
		}
	})
	if ierr != nil {
		return ierr
	}
	return tx.Commit()
}

var ftsTok = regexp.MustCompile(`"([^"]+)"|(\S+)`)

// ftsQuery : la saisie de l'utilisateur devient une requête FTS5 sûre (tout est entre guillemets) ;
// les mots sont combinés par ET, un passage entre guillemets reste une phrase, le dernier mot est un préfixe.
func ftsQuery(q string) string {
	var parts []string
	last := -1
	for _, m := range ftsTok.FindAllStringSubmatch(q, -1) {
		if m[1] != "" {
			parts = append(parts, `"`+m[1]+`"`)
		} else if w := strings.ReplaceAll(m[2], `"`, ""); w != "" {
			parts = append(parts, `"`+w+`"`)
			last = len(parts) - 1
		}
	}
	if last >= 0 && last == len(parts)-1 {
		parts[last] += "*"
	}
	return strings.Join(parts, " ")
}

// searchIndex : sessions classées par pertinence ; N = nombre de messages qui correspondent,
// Snip = extrait du meilleur message, correspondances entourées de \x02…\x03.
func searchIndex(q string) ([]Hit, error) {
	fq := ftsQuery(q)
	if fq == "" {
		return []Hit{}, nil
	}
	if err := syncIndex(); err != nil {
		return nil, err
	}
	db, err := idxOpen()
	if err != nil {
		return nil, err
	}
	// snippet() ne se calcule que pour le meilleur message de chaque session (id suit min(r)) : sur tous les messages trouvés,
	// un mot courant dans un gros transcript coûtait une minute.
	rows, err := db.Query(`
WITH m AS MATERIALIZED (
  SELECT rowid AS id, dir, sid, bm25(msgs) AS r FROM msgs WHERE msgs MATCH ?1
), b AS (
  SELECT dir, sid, count(*) AS n, min(r) AS r, id FROM m GROUP BY sid ORDER BY 4 LIMIT 300
) SELECT dir, sid, n, r, (SELECT snippet(msgs, 0, char(2), char(3), '…', 24) FROM msgs WHERE msgs MATCH ?1 AND rowid = b.id)
FROM b ORDER BY 4`, fq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Hit{}
	for rows.Next() {
		var h Hit
		var r float64
		if err := rows.Scan(&h.Dir, &h.ID, &h.N, &r, &h.Snip); err != nil {
			return nil, err
		}
		h.Snip = strings.Join(strings.Fields(h.Snip), " ")
		out = append(out, h)
	}
	return out, rows.Err()
}

// searchAll : l'index, ou le balayage direct des transcripts si l'index est inutilisable.
func searchAll(q string) []Hit {
	if h, err := searchIndex(q); err == nil {
		return h
	} else {
		journal("index de recherche : %v", err)
	}
	return search(q)
}
