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

func lineEnd(content []byte, start int) int {
	if newlineIndex := bytes.IndexByte(content[start:], '\n'); newlineIndex >= 0 {
		return start + newlineIndex + 1
	}
	return len(content)
}

// onlyToolResults : une ligne « user » qui ne porte que des résultats d'outils (écrite par Claude Code, pas tapée).
func onlyToolResults(content json.RawMessage) bool {
	var parts []struct{ Type string }
	if json.Unmarshal(content, &parts) != nil || len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if part.Type != "tool_result" {
			return false
		}
	}
	return true
}

// rewind coupe le transcript après la ligne qui commence à l'octet offset et renvoie le nombre de messages retirés.
// La suite du même message de Claude (une réponse s'écrit sur plusieurs lignes) et les résultats d'outils qui suivent
// restent : un appel d'outil sans résultat ferait refuser la reprise. Les titres posés plus loin sont gardés.
func rewind(path string, offset int64) (int, error) {
	transcriptContent, err := readTrusted(path)
	if err != nil {
		return 0, err
	}
	lineStart := int(offset)
	isOutOfRange := offset < 0 || offset >= int64(len(transcriptContent))
	if isOutOfRange || (lineStart > 0 && transcriptContent[lineStart-1] != '\n') {
		return 0, terr("srv.http_util.requeteInvalide")
	}
	end := lineEnd(transcriptContent, lineStart)
	var first rewindLine
	if json.Unmarshal(transcriptContent[lineStart:end], &first) != nil || (first.Type != "user" && first.Type != "assistant") {
		return 0, terr("srv.http_util.requeteInvalide")
	}
	for end < len(transcriptContent) {
		lineStop := lineEnd(transcriptContent, end)
		var entry rewindLine
		_ = json.Unmarshal(transcriptContent[end:lineStop], &entry)
		isSameMessage := first.Type == "assistant" && entry.Type == "assistant" && entry.Message.ID != "" && entry.Message.ID == first.Message.ID
		isToolResultLine := entry.Type == "user" && onlyToolResults(entry.Message.Content)
		if !isSameMessage && !isToolResultLine {
			break
		}
		end = lineStop
	}
	kept := bytes.Clone(transcriptContent[:end])
	if len(kept) > 0 && kept[len(kept)-1] != '\n' {
		kept = append(kept, '\n')
	}
	removedCount := 0
	for position := end; position < len(transcriptContent); {
		lineStop := lineEnd(transcriptContent, position)
		var entry rewindLine
		_ = json.Unmarshal(transcriptContent[position:lineStop], &entry)
		switch entry.Type {
		case "custom-title":
			kept = append(kept, bytes.TrimRight(transcriptContent[position:lineStop], "\n")...)
			kept = append(kept, '\n')
		case "user", "assistant":
			removedCount++
		}
		position = lineStop
	}
	fileInfo, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	return removedCount, rWriteAtomic(cfgDir(), path, kept, fileInfo.Mode().Perm())
}
