package main

// Retour en arrière dans une session : le transcript est coupé après un message, la reprise repart de là.

import (
	"bytes"
	"encoding/json"
	"os"
)

type rewindLine struct {
	Type    string `json:"type"`
	Message struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func lineEnd(b []byte, at int) int {
	if i := bytes.IndexByte(b[at:], '\n'); i >= 0 {
		return at + i + 1
	}
	return len(b)
}

// onlyToolResults : une ligne « user » qui ne porte que des résultats d'outils (écrite par Claude Code, pas tapée).
func onlyToolResults(c json.RawMessage) bool {
	var parts []struct{ Type string }
	if json.Unmarshal(c, &parts) != nil || len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if p.Type != "tool_result" {
			return false
		}
	}
	return true
}

// rewind coupe le transcript après la ligne qui commence à l'octet off et renvoie le nombre de messages retirés.
// La suite du même message de Claude (une réponse s'écrit sur plusieurs lignes) et les résultats d'outils qui suivent
// restent : un appel d'outil sans résultat ferait refuser la reprise. Les titres posés plus loin sont gardés.
func rewind(path string, off int64) (int, error) {
	b, err := readTrusted(path)
	if err != nil {
		return 0, err
	}
	at := int(off)
	if off < 0 || off >= int64(len(b)) || (at > 0 && b[at-1] != '\n') {
		return 0, terr("srv.http_util.requeteInvalide")
	}
	end := lineEnd(b, at)
	var first rewindLine
	if json.Unmarshal(b[at:end], &first) != nil || (first.Type != "user" && first.Type != "assistant") {
		return 0, terr("srv.http_util.requeteInvalide")
	}
	for end < len(b) {
		next := lineEnd(b, end)
		var l rewindLine
		_ = json.Unmarshal(b[end:next], &l)
		same := first.Type == "assistant" && l.Type == "assistant" && l.Message.ID != "" && l.Message.ID == first.Message.ID
		if !same && (l.Type != "user" || !onlyToolResults(l.Message.Content)) {
			break
		}
		end = next
	}
	out := bytes.Clone(b[:end])
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	n := 0
	for i := end; i < len(b); {
		next := lineEnd(b, i)
		var l rewindLine
		_ = json.Unmarshal(b[i:next], &l)
		switch l.Type {
		case "custom-title":
			out = append(out, bytes.TrimRight(b[i:next], "\n")...)
			out = append(out, '\n')
		case "user", "assistant":
			n++
		}
		i = next
	}
	st, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	return n, rWriteAtomic(cfgDir(), path, out, st.Mode().Perm())
}
