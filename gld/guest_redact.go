package main

// Masquage de l'hôte dans ce qu'un invité reçoit : les chemins du dossier partagé deviennent relatifs, tout autre chemin
// absolu de l'hôte ne garde que son dernier segment, et le nom du compte devient « user ».

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
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
	hostPathRe = regexp.MustCompile(
		"(^|[\\s\"'(=:@,;<>`]|\\\\[nt])" +
			`(/(?:[A-Za-z0-9._@+%~=-]+/)+[A-Za-z0-9._@+%~=-]+)`)
	appPathPrefixes   = []string{"/v1/", "/assets/", "/icons/", "/guest/", "/invite/"}
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
func newPathRedactor(home, projectDir, alias string, extraTerms ...string) *pathRedactor {
	encodedHome := nonAlnum.ReplaceAllString(home, "-")
	if home == "" || home == "/" || encodedHome == "-" {
		return nil
	}
	redactor := &pathRedactor{alias: alias, encodedHome: regexp.MustCompile(regexp.QuoteMeta(encodedHome) + encodedBoundary)}
	if projectDir != "" && projectDir != "/" {
		redactor.projectPrefix = regexp.MustCompile(regexp.QuoteMeta(projectDir) + "/")
		redactor.projectExact = regexp.MustCompile(regexp.QuoteMeta(projectDir) + pathBoundary)
	}
	nameTerms := []string{}
	for _, term := range append([]string{filepath.Base(home)}, extraTerms...) {
		if len(term) >= minAccountLength && !spareAccountNames[strings.ToLower(term)] {
			nameTerms = append(nameTerms, regexp.QuoteMeta(term))
		}
	}
	if len(nameTerms) > 0 {
		sort.Slice(nameTerms, func(first, second int) bool { return len(nameTerms[first]) > len(nameTerms[second]) })
		redactor.account = regexp.MustCompile("(?i)(?:" + strings.Join(nameTerms, "|") + ")")
	}
	return redactor
}

func hideHostPath(match []byte) []byte {
	groups := hostPathRe.FindSubmatch(match)
	if groups == nil {
		return match
	}
	hostPath := string(groups[2])
	for _, appPrefix := range appPathPrefixes {
		if strings.HasPrefix(hostPath, appPrefix) {
			return match
		}
	}
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
	content = hostPathRe.ReplaceAllFunc(content, hideHostPath)
	if redactor.account != nil {
		content = redactor.replaceWords(content)
	}
	return content
}

func isWordByte(character byte) bool {
	return character == '_' || character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

// replaceWords remplace les termes d'identité entiers : un terme collé à une lettre ou un chiffre (bobbyx) reste intact.
func (redactor *pathRedactor) replaceWords(content []byte) []byte {
	var masked []byte
	copiedUntil := 0
	for _, bounds := range redactor.account.FindAllIndex(content, -1) {
		startsWord := bounds[0] == 0 || !isWordByte(content[bounds[0]-1])
		endsWord := bounds[1] == len(content) || !isWordByte(content[bounds[1]])
		if !startsWord || !endsWord {
			continue
		}
		masked = append(append(masked, content[copiedUntil:bounds[0]]...), redactor.alias...)
		copiedUntil = bounds[1]
	}
	if masked == nil {
		return content
	}
	return append(masked, content[copiedUntil:]...)
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
