// Journal des modifications (~/.claude/gld-web.log) et corbeille de la zone Mémoire (~/.claude/gld-web-trash/) :
// un fichier de mémoire supprimé, ou remplacé par un enregistrement, y est d'abord copié, et reste restaurable.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var journalMu sync.Mutex

// journal ajoute une ligne horodatée ; une erreur d'écriture n'empêche jamais l'action elle-même.
func journal(format string, a ...any) {
	journalMu.Lock()
	defer journalMu.Unlock()
	f, err := os.OpenFile(filepath.Join(cfgDir(), "gld-web.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	line := strings.ReplaceAll(fmt.Sprintf(format, a...), "\n", " ")
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
}

type Trashed struct {
	ID     string  `json:"id"` // nom du dossier dans la corbeille
	Key    string  `json:"key"`
	Path   string  `json:"path"`
	Reason string  `json:"reason"` // « supprimé » ou « remplacé »
	At     float64 `json:"at"`
	KB     int64   `json:"kb"`
}

func trashDir() string { return filepath.Join(cfgDir(), "gld-web-trash") }

// toTrash copie le contenu actuel d'un fichier de mémoire dans la corbeille, avant sa suppression ou son remplacement.
func toTrash(key, reason string) error {
	text, err := memRead(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil // rien à garder
	}
	if err != nil {
		return err
	}
	base, rel, _ := memResolve(key)
	b := make([]byte, 4)
	rand.Read(b)
	t := Trashed{time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b), key, filepath.Join(base, rel), reason,
		float64(time.Now().UnixNano()) / 1e9, int64(len(text)) / 1024}
	d := filepath.Join(trashDir(), t.ID)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	meta, _ := json.Marshal(t)
	if err := os.WriteFile(filepath.Join(d, "content"), []byte(text), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d, "meta.json"), meta, 0o600)
}

func trashList() []Trashed {
	out := []Trashed{}
	ents, _ := os.ReadDir(trashDir())
	for _, e := range ents {
		var t Trashed
		if b, err := os.ReadFile(filepath.Join(trashDir(), e.Name(), "meta.json")); err == nil && json.Unmarshal(b, &t) == nil && t.ID == e.Name() {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}

func trashItem(id string) (Trashed, string, error) {
	for _, t := range trashList() {
		if t.ID == id {
			b, err := os.ReadFile(filepath.Join(trashDir(), id, "content"))
			return t, string(b), err
		}
	}
	return Trashed{}, "", errors.New("introuvable dans la corbeille")
}

// trashRestore remet le fichier à sa place ; s'il existe déjà, sa version actuelle part à son tour à la corbeille.
func trashRestore(id string) (Trashed, error) {
	t, text, err := trashItem(id)
	if err != nil {
		return t, err
	}
	if err := memSave(t.Key, text); err != nil { // memSave met la version remplacée à la corbeille
		return t, err
	}
	return t, os.RemoveAll(filepath.Join(trashDir(), id))
}

// trashPurge vide la corbeille (ou en retire un seul élément).
func trashPurge(id string) error {
	if id == "" {
		return os.RemoveAll(trashDir())
	}
	if _, _, err := trashItem(id); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(trashDir(), id))
}
