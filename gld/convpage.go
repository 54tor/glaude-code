package main

// Lecture d'une conversation par la fin : un transcript de plusieurs centaines de Mo (des milliers de prompts) ne se
// charge pas d'un bloc. On lit les lignes à rebours, par blocs, jusqu'à avoir assez de messages ; « start » (le
// décalage de la première ligne lue) sert ensuite à demander les messages précédents.

import (
	"bytes"
	"io"
	"os"
)

var convChunk int64 = 4 << 20 // variable pour que les tests puissent la réduire

// convPage renvoie au moins limit messages (sauf en début de fichier) parmi les lignes situées avant l'octet before
// (0 : la fin du fichier), dans l'ordre du fichier. more dit s'il existe des lignes avant start.
func convPage(path string, tools bool, before int64, limit int) (msgs []Msg, start int64, more bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, false, err
	}
	end := fi.Size()
	if before > 0 && before < end {
		end = before
	}
	start = end
	var groups [][]Msg // un groupe par ligne, du plus récent au plus ancien
	n := 0
	var carry []byte // début (incomplet) d'une ligne dont on n'a pas encore atteint le commencement
	pos := end
	for pos > 0 && n < limit {
		size := convChunk
		if size > pos {
			size = pos
		}
		pos -= size
		buf := make([]byte, size, size+int64(len(carry)))
		if _, err := f.ReadAt(buf, pos); err != nil && err != io.EOF {
			return nil, 0, false, err
		}
		data := append(buf, carry...)
		carry = nil
		lo := int64(0) // décalage, dans data, de la première ligne complète
		if pos > 0 {
			i := bytes.IndexByte(data, '\n')
			if i < 0 { // une seule ligne démesurée : on continue de remonter
				carry = data
				continue
			}
			carry = data[:i+1] // suite à compléter avec le bloc précédent (sa fin est ce « \n »)
			lo = int64(i + 1)
		}
		// lignes complètes de data[lo:], de la dernière à la première
		for hi := int64(len(data)); hi > lo && n < limit; {
			j := int64(bytes.LastIndexByte(data[lo:hi-1], '\n')) // hi-1 : le « \n » final de la ligne
			ls := lo
			if j >= 0 {
				ls = lo + j + 1
			}
			line := data[ls:hi]
			start = pos + ls
			if m := withOffset(lineMsgs(line, tools), start); len(m) > 0 {
				groups = append(groups, m)
				n += len(m)
			}
			hi = ls
		}
	}
	if pos == 0 && n < limit {
		start = 0
	}
	for i := len(groups) - 1; i >= 0; i-- {
		msgs = append(msgs, groups[i]...)
	}
	return msgs, start, start > 0, nil
}
