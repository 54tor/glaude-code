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
func idxFileFor(transcriptsRoot string) string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	rootHash := sha256.Sum256([]byte(filepath.Clean(transcriptsRoot)))
	return filepath.Join(cacheDir, "gld-web", "search-"+hex.EncodeToString(rootHash[:indexHashPrefixBytes])+".db")
}

// indexSettle : un transcript déjà indexé et modifié depuis moins longtemps n'est pas relu. Une session vivante écrit en
// continu, et relire un gros transcript à chaque frappe de recherche coûtait plusieurs secondes.
const indexSettle = 30 * time.Second

const idxSchema = `
CREATE TABLE IF NOT EXISTS files(id INTEGER PRIMARY KEY, path TEXT UNIQUE, mod INTEGER, size INTEGER);
CREATE VIRTUAL TABLE IF NOT EXISTS msgs USING fts5(body, dir UNINDEXED, sid UNINDEXED, tokenize='unicode61 remove_diacritics 2');`

// rowid d'un message = fichier × 2^20 + rang : supprimer un fichier = supprimer une plage de rowid.
const perFile = 1 << 20

// indexHashPrefixBytes : octets du condensé du dossier gardés dans le nom du fichier d'index.
const indexHashPrefixBytes = 6

func idxOpen() (*sql.DB, error) {
	if idxDB != nil {
		return idxDB, nil
	}
	indexPath := idxFile()
	if mkdirErr := os.MkdirAll(filepath.Dir(indexPath), 0o700); mkdirErr != nil {
		return nil, mkdirErr
	}
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(indexPath)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err == nil {
		_, err = database.Exec(idxSchema)
	}
	if err != nil {
		return nil, err
	}
	idxDB = database
	return database, nil
}

// syncIndex met l'index d'accord avec les transcripts : ajoute les nouveaux, relit les modifiés, oublie les disparus.
func syncIndex() error {
	idxMu.Lock()
	defer idxMu.Unlock()
	database, err := idxOpen()
	if err != nil {
		return err
	}
	type indexedFile struct {
		id        int64
		mod, size int64
	}
	known := map[string]indexedFile{}
	rows, err := database.Query(`SELECT id, path, mod, size FROM files`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var transcriptPath string
		var knownFile indexedFile
		if scanErr := rows.Scan(&knownFile.id, &transcriptPath, &knownFile.mod, &knownFile.size); scanErr != nil {
			rows.Close()
			return scanErr
		}
		known[transcriptPath] = knownFile
	}
	rows.Close()

	transcriptFiles, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	seen := map[string]bool{}
	for _, transcriptPath := range transcriptFiles {
		seen[transcriptPath] = true
		fileInfo, statErr := os.Stat(transcriptPath)
		if statErr != nil {
			continue
		}
		knownFile, isKnown := known[transcriptPath]
		isUnchanged := knownFile.mod == fileInfo.ModTime().UnixNano() && knownFile.size == fileInfo.Size()
		if isKnown && isUnchanged {
			continue
		}
		if isKnown && idxNow().Sub(fileInfo.ModTime()) < indexSettle {
			continue
		}
		if indexErr := indexFile(database, transcriptPath, fileInfo, knownFile.id); indexErr != nil {
			return indexErr
		}
	}
	for transcriptPath, knownFile := range known {
		if seen[transcriptPath] {
			continue
		}
		if dropErr := dropFile(database, knownFile.id); dropErr != nil {
			return dropErr
		}
	}
	return nil
}

func dropFile(database *sql.DB, fileID int64) error {
	transaction, err := database.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, deleteMessagesErr := transaction.Exec(`DELETE FROM msgs WHERE rowid >= ? AND rowid < ?`, fileID*perFile, (fileID+1)*perFile); deleteMessagesErr != nil {
		return deleteMessagesErr
	}
	if _, deleteFileErr := transaction.Exec(`DELETE FROM files WHERE id = ?`, fileID); deleteFileErr != nil {
		return deleteFileErr
	}
	return transaction.Commit()
}

// indexFile (ré)indexe un transcript ; fileID = 0 pour un fichier inconnu.
func indexFile(database *sql.DB, path string, fileInfo os.FileInfo, fileID int64) error {
	transaction, err := database.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if fileID == 0 {
		insertResult, fileInsertErr := transaction.Exec(`INSERT INTO files(path, mod, size) VALUES(?, ?, ?)`, path, fileInfo.ModTime().UnixNano(), fileInfo.Size())
		if fileInsertErr != nil {
			return fileInsertErr
		}
		fileID, _ = insertResult.LastInsertId()
	} else {
		if _, deleteMessagesErr := transaction.Exec(`DELETE FROM msgs WHERE rowid >= ? AND rowid < ?`, fileID*perFile, (fileID+1)*perFile); deleteMessagesErr != nil {
			return deleteMessagesErr
		}
		if _, updateErr := transaction.Exec(`UPDATE files SET mod = ?, size = ? WHERE id = ?`, fileInfo.ModTime().UnixNano(), fileInfo.Size(), fileID); updateErr != nil {
			return updateErr
		}
	}
	insertStatement, err := transaction.Prepare(`INSERT INTO msgs(rowid, body, dir, sid) VALUES(?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insertStatement.Close()
	dir, sessionID := filepath.Base(filepath.Dir(path)), strings.TrimSuffix(filepath.Base(path), ".jsonl")
	messageRank := int64(0)
	var messageInsertErr error
	_ = scan(path, func(transcriptLine []byte) {
		for _, message := range lineMsgs(transcriptLine, false) {
			if messageInsertErr == nil && messageRank < perFile {
				_, messageInsertErr = insertStatement.Exec(fileID*perFile+messageRank, message.T, dir, sessionID)
				messageRank++
			}
		}
	})
	if messageInsertErr != nil {
		return messageInsertErr
	}
	return transaction.Commit()
}

var ftsTok = regexp.MustCompile(`"([^"]+)"|(\S+)`)

// ftsQuery : la saisie de l'utilisateur devient une requête FTS5 sûre (tout est entre guillemets) ;
// les mots sont combinés par ET, un passage entre guillemets reste une phrase, le dernier mot est un préfixe.
func ftsQuery(userInput string) string {
	var parts []string
	lastWordIndex := -1
	for _, match := range ftsTok.FindAllStringSubmatch(userInput, -1) {
		if match[1] != "" {
			parts = append(parts, `"`+match[1]+`"`)
		} else if word := strings.ReplaceAll(match[2], `"`, ""); word != "" {
			parts = append(parts, `"`+word+`"`)
			lastWordIndex = len(parts) - 1
		}
	}
	if lastWordIndex >= 0 && lastWordIndex == len(parts)-1 {
		parts[lastWordIndex] += "*"
	}
	return strings.Join(parts, " ")
}

// searchIndex : sessions classées par pertinence ; N = nombre de messages qui correspondent,
// Snip = extrait du meilleur message, correspondances entourées de \x02…\x03.
func searchIndex(userInput string) ([]Hit, error) {
	ftsExpression := ftsQuery(userInput)
	if ftsExpression == "" {
		return []Hit{}, nil
	}
	if syncErr := syncIndex(); syncErr != nil {
		return nil, syncErr
	}
	database, err := idxOpen()
	if err != nil {
		return nil, err
	}
	// snippet() ne se calcule que pour le meilleur message de chaque session (id suit min(r)) : sur tous les messages trouvés,
	// un mot courant dans un gros transcript coûtait une minute.
	rows, err := database.Query(`
WITH m AS MATERIALIZED (
  SELECT rowid AS id, dir, sid, bm25(msgs) AS r FROM msgs WHERE msgs MATCH ?1
), b AS (
  SELECT dir, sid, count(*) AS n, min(r) AS r, id FROM m GROUP BY sid ORDER BY 4 LIMIT 300
) SELECT dir, sid, n, r, (SELECT snippet(msgs, 0, char(2), char(3), '…', 24) FROM msgs WHERE msgs MATCH ?1 AND rowid = b.id)
FROM b ORDER BY 4`, ftsExpression)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []Hit{}
	for rows.Next() {
		var hit Hit
		var rank float64
		if scanErr := rows.Scan(&hit.Dir, &hit.ID, &hit.N, &rank, &hit.Snip); scanErr != nil {
			return nil, scanErr
		}
		hit.Snip = strings.Join(strings.Fields(hit.Snip), " ")
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

// searchAll : l'index, ou le balayage direct des transcripts si l'index est inutilisable.
func searchAll(userInput string) []Hit {
	hits, err := searchIndex(userInput)
	if err == nil {
		return hits
	}
	journal("index de recherche : %v", err)
	return search(userInput)
}
