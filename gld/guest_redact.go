package main

// Masquage de l'hôte dans ce qu'un invité reçoit : les chemins du dossier partagé deviennent relatifs, tout autre chemin
// absolu de l'hôte ne garde que son dernier segment, et le nom du compte devient « user ».

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	aliasPrefix        = "guest-"
	aliasHexLength     = 4
	hiddenPathPrefix   = "…/"
	pathBoundary       = `([^A-Za-z0-9_.-]|$)`
	encodedBoundary    = `([^A-Za-z0-9]|$)`
	wordBoundaryBefore = `(^|[^A-Za-z0-9_])`
	wordBoundaryAfter  = `([^A-Za-z0-9_]|$)`
	minAccountLength   = 3
)

var (
	hostRootPathRe = regexp.MustCompile(
		"(^|[\\s\"'(=:@,;<>`]|\\\\[nt])" +
			`(/(?:home|Users|tmp|var|usr|etc|opt|mnt|srv|root|run|proc|sys|media|private)(?:/[A-Za-z0-9._@+%~=-]+)+)`)
	spareAccountNames = map[string]bool{"user": true, "root": true}
)

// guestAlias : nom de remplacement stable par invitation, sans rapport avec le vrai compte.
func guestAlias(invitationID string) string {
	digest := sha256.Sum256([]byte("alias:" + invitationID))
	return aliasPrefix + hex.EncodeToString(digest[:])[:aliasHexLength]
}

type pathRedactor struct {
	alias         string
	projectPrefix *regexp.Regexp
	projectExact  *regexp.Regexp
	encodedHome   *regexp.Regexp
	account       *regexp.Regexp
}

// newPathRedactor renvoie nil quand il n'y a rien à masquer. projectDir vide : aucun chemin n'est ramené au projet.
func newPathRedactor(home, projectDir, alias string) *pathRedactor {
	encodedHome := nonAlnum.ReplaceAllString(home, "-")
	if home == "" || home == "/" || encodedHome == "-" {
		return nil
	}
	redactor := &pathRedactor{alias: alias, encodedHome: regexp.MustCompile(regexp.QuoteMeta(encodedHome) + encodedBoundary)}
	if projectDir != "" && projectDir != "/" {
		redactor.projectPrefix = regexp.MustCompile(regexp.QuoteMeta(projectDir) + "/")
		redactor.projectExact = regexp.MustCompile(regexp.QuoteMeta(projectDir) + pathBoundary)
	}
	accountName := filepath.Base(home)
	if len(accountName) >= minAccountLength && !spareAccountNames[accountName] {
		redactor.account = regexp.MustCompile(wordBoundaryBefore + regexp.QuoteMeta(accountName) + wordBoundaryAfter)
	}
	return redactor
}

func hideHostPath(match []byte) []byte {
	groups := hostRootPathRe.FindSubmatch(match)
	if groups == nil {
		return match
	}
	hostPath := string(groups[2])
	lastSeparator := strings.LastIndexByte(hostPath, '/')
	return append(append([]byte{}, groups[1]...), []byte(hiddenPathPrefix+hostPath[lastSeparator+1:])...)
}

func (redactor *pathRedactor) apply(content []byte) []byte {
	if redactor == nil {
		return content
	}
	if redactor.projectPrefix != nil {
		content = redactor.projectPrefix.ReplaceAll(content, nil)
		content = redactor.projectExact.ReplaceAll(content, []byte(".$1"))
	}
	content = redactor.encodedHome.ReplaceAll(content, []byte("-home-"+redactor.alias+"$1"))
	content = hostRootPathRe.ReplaceAllFunc(content, hideHostPath)
	if redactor.account != nil {
		replacement := []byte("${1}" + redactor.alias + "${2}")
		content = redactor.account.ReplaceAll(redactor.account.ReplaceAll(content, replacement), replacement)
	}
	return content
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
