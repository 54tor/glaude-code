package main

// Lecture d'une conversation par la fin : un transcript de plusieurs centaines de Mo (des milliers de prompts) ne se
// charge pas d'un bloc. On lit les lignes à rebours, par blocs, jusqu'à avoir assez de messages ; « start » (le
// décalage de la première ligne lue) sert ensuite à demander les messages précédents.

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"unicode/utf8"
)

var convChunk int64 = 4 << 20 // variable pour que les tests puissent la réduire

// convPage renvoie au moins limit messages (sauf en début de fichier) parmi les lignes situées avant l'octet before
// (0 : la fin du fichier), dans l'ordre du fichier. more dit s'il existe des lignes avant start.
func convPage(path string, tools bool, before int64, limit int) (msgs []Msg, start int64, more bool, err error) {
	return convPageMode(path, tools, false, before, limit)
}

// convPageMode : voir lineMsgsMode pour anchors.
func convPageMode(path string, tools, anchors bool, before int64, limit int) (msgs []Msg, start int64, more bool, err error) {
	transcriptFile, err := openTrusted(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer transcriptFile.Close()
	fileInfo, err := transcriptFile.Stat()
	if err != nil {
		return nil, 0, false, err
	}
	end := fileInfo.Size()
	if before > 0 && before < end {
		end = before
	}
	start = end
	var groups [][]Msg // un groupe par ligne, du plus récent au plus ancien
	collected := 0
	var carry []byte // début (incomplet) d'une ligne dont on n'a pas encore atteint le commencement
	chunkStart := end
	for chunkStart > 0 && collected < limit {
		size := min(convChunk, chunkStart)
		chunkStart -= size
		chunk := make([]byte, size, size+int64(len(carry)))
		_, readErr := transcriptFile.ReadAt(chunk, chunkStart)
		if readErr != nil && readErr != io.EOF {
			return nil, 0, false, readErr
		}
		data := append(chunk, carry...)
		carry = nil
		firstLineOffset := int64(0) // décalage, dans data, de la première ligne complète
		if chunkStart > 0 {
			newlineIndex := bytes.IndexByte(data, '\n')
			if newlineIndex < 0 { // une seule ligne démesurée : on continue de remonter
				carry = data
				continue
			}
			carry = data[:newlineIndex+1] // suite à compléter avec le bloc précédent (sa fin est ce « \n »)
			firstLineOffset = int64(newlineIndex + 1)
		}
		// lignes complètes de data[firstLineOffset:], de la dernière à la première
		for lineEnd := int64(len(data)); lineEnd > firstLineOffset && collected < limit; {
			previousNewline := int64(bytes.LastIndexByte(data[firstLineOffset:lineEnd-1], '\n')) // lineEnd-1 : le « \n » final de la ligne
			lineStart := firstLineOffset
			if previousNewline >= 0 {
				lineStart = firstLineOffset + previousNewline + 1
			}
			completeLine := data[lineStart:lineEnd]
			start = chunkStart + lineStart
			if lineMessages := withOffset(lineMsgsMode(completeLine, tools, anchors), start); len(lineMessages) > 0 {
				groups = append(groups, lineMessages)
				collected += len(lineMessages)
			}
			lineEnd = lineStart
		}
	}
	if chunkStart == 0 && collected < limit {
		start = 0
	}
	msgs = []Msg{} // jamais nil : une session de seules commandes donnerait "msgs":null, pris par l'interface pour « pas chargé »
	for groupIndex := len(groups) - 1; groupIndex >= 0; groupIndex-- {
		msgs = append(msgs, groups[groupIndex]...)
	}
	return msgs, start, start > 0, nil
}

// msgCap : texte d'un message rendu d'un coup. Au-delà (un message de plusieurs dizaines de Mo), le navigateur se fige
// à le mettre en page : la page n'en donne que le début et la suite vient à la demande.
var msgCap = 64 << 10

// capText rend au plus msgCap octets de text à partir de from, coupés sur un début de caractère, et l'octet où reprendre
// (0 : fin du texte).
func capText(text string, from int) (string, int) {
	if from < 0 || from >= len(text) {
		return "", 0
	}
	end := from + msgCap
	if end >= len(text) {
		return text[from:], 0
	}
	for end > from+1 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[from:end], end
}

// capMsgs coupe les textes trop longs d'une page de messages.
func capMsgs(messages []Msg) {
	for messageIndex := range messages {
		if textLength := len(messages[messageIndex].T); textLength > msgCap {
			messages[messageIndex].T, messages[messageIndex].Cut = capText(messages[messageIndex].T, 0)
			messages[messageIndex].Len = textLength
		}
	}
}

// convText rend la suite (à partir de l'octet from) du message de rang messageIndex de la ligne qui commence à l'octet lineOffset.
func convText(path string, lineOffset int64, messageIndex int, tools bool, from int) (string, int, error) {
	line, err := lineAt(path, lineOffset)
	if err != nil {
		return "", 0, err
	}
	messages := lineMsgsMode(line, tools, !tools) // la page des outils masqués porte des ancres : mêmes rangs
	if messageIndex < 0 || messageIndex >= len(messages) {
		return "", 0, terr("srv.convpage.messageIntrouvable")
	}
	text, next := capText(messages[messageIndex].T, from)
	return text, next, nil
}

// convTools rend les messages d'outil des lignes dont le décalage est dans [from, until) (until 0 : jusqu'à la fin), pour déplier
// une ancre. Au plus convToolsMax messages.
const convToolsMax = 1000

func convTools(path string, from, until int64) ([]Msg, error) {
	transcriptFile, err := openTrusted(path)
	if err != nil {
		return nil, err
	}
	defer transcriptFile.Close()
	if _, seekErr := transcriptFile.Seek(from, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	reader := bufio.NewReaderSize(transcriptFile, jsonLineBufferSize)
	toolMessages := []Msg{}
	for lineOffset := from; (until == 0 || lineOffset < until) && len(toolMessages) < convToolsMax; {
		rawLine, readErr := reader.ReadBytes('\n')
		if len(rawLine) > 0 {
			for _, message := range withOffset(lineMsgs(rawLine, true), lineOffset) {
				if message.R == "tool" {
					toolMessages = append(toolMessages, message)
				}
			}
			lineOffset += int64(len(rawLine))
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, readErr
		}
	}
	capMsgs(toolMessages)
	return toolMessages, nil
}
