package main

// Masquage du compte système dans ce qu'un invité reçoit : le dossier personnel de la cible, sous sa forme de chemin
// (/home/nom) et sous sa forme encodée par Claude Code (-home-nom). Seules ces formes sont touchées, jamais le nom nu.

import (
	"net/http"
	"regexp"
)

const (
	redactedHome        = "~"
	redactedEncodedHome = "-home-user"
	pathBoundary        = `([^A-Za-z0-9_.-]|$)`
	encodedBoundary     = `([^A-Za-z0-9]|$)`
)

type pathRedactor struct {
	plainHome   *regexp.Regexp
	encodedHome *regexp.Regexp
}

// newPathRedactor renvoie nil quand il n'y a rien à masquer (home vide ou racine).
func newPathRedactor(home string) *pathRedactor {
	encodedHome := nonAlnum.ReplaceAllString(home, "-")
	if home == "" || home == "/" || encodedHome == "-" {
		return nil
	}
	return &pathRedactor{
		plainHome:   regexp.MustCompile(regexp.QuoteMeta(home) + pathBoundary),
		encodedHome: regexp.MustCompile(regexp.QuoteMeta(encodedHome) + encodedBoundary),
	}
}

func (redactor *pathRedactor) apply(content []byte) []byte {
	if redactor == nil {
		return content
	}
	content = redactor.plainHome.ReplaceAll(content, []byte(redactedHome+"$1"))
	return redactor.encodedHome.ReplaceAll(content, []byte(redactedEncodedHome+"$1"))
}

func (redactor *pathRedactor) applyString(text string) string {
	return string(redactor.apply([]byte(text)))
}

// redactWriter réécrit le corps au passage ; un motif coupé entre deux écritures n'est pas vu (les réponses JSON partent d'un bloc).
type redactWriter struct {
	http.ResponseWriter
	redactor *pathRedactor
}

func (writer *redactWriter) WriteHeader(status int) {
	writer.Header().Del("Content-Length")
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *redactWriter) Write(content []byte) (int, error) {
	if _, err := writer.ResponseWriter.Write(writer.redactor.apply(content)); err != nil {
		return 0, err
	}
	return len(content), nil
}

func (writer *redactWriter) Flush() {
	if flusher, canFlush := writer.ResponseWriter.(http.Flusher); canFlush {
		flusher.Flush()
	}
}

func (writer *redactWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func guestRedactedWriter(responseWriter http.ResponseWriter, redactor *pathRedactor) http.ResponseWriter {
	if redactor == nil {
		return responseWriter
	}
	return &redactWriter{ResponseWriter: responseWriter, redactor: redactor}
}
