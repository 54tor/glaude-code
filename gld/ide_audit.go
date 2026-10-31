package main

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ideAuditMax    = 300
	ideAuditHeader = "X-Gld-Audit"
)

// ideAudit journalise une écriture réussie dans auth_events (jamais le contenu). Sans garde, c'est un no-op.
// Format : « <op> <base du dossier>:<chemin relatif>[ <octets>] » ; un jeton porteur est écrit avec user_id NULL.
// Un enfant n'a pas de base de comptes : il renvoie la ligne à sa passerelle (ideAuditRelay), qui l'écrit.
func ideAudit(responseWriter http.ResponseWriter, request *http.Request, op, dir, rel string, size int64) {
	auditLine := op + " " + filepath.Base(dir) + ":" + rel
	if size >= 0 {
		auditLine += " " + strconv.FormatInt(size, 10)
	}
	auditLine = ideAuditClean(auditLine)
	if childToken != "" {
		responseWriter.Header().Set(ideAuditHeader, url.QueryEscape(auditLine))
		return
	}
	ideAuditWrite(gateFrom(request), clientIP(request), auditLine)
}

func ideAuditWrite(gate *gateInfo, ip, auditLine string) {
	if gate == nil || gate.p == nil || gate.s == nil {
		return
	}
	var userID any
	if !gate.p.Token {
		if gate.p.User == nil {
			return
		}
		userID = gate.p.User.ID
	}
	gate.s.db.Exec(`INSERT INTO auth_events(at,user_id,ip,kind,outcome) VALUES(?,?,?,?,?)`,
		authNow().Unix(), userID, ip, "ide_write", auditLine)
}

type auditIPKey struct{}

// ideAuditRelay (ModifyResponse du proxy d'un enfant) : l'en-tête ne parvient jamais au client ; seule une réponse 2xx
// d'un enfant est journalisée, pour l'utilisateur que la passerelle a authentifié.
func ideAuditRelay(response *http.Response) error {
	encoded := response.Header.Get(ideAuditHeader)
	response.Header.Del(ideAuditHeader)
	isSuccess := response.StatusCode/100 == 2
	if encoded == "" || !isSuccess {
		return nil
	}
	auditLine, err := url.QueryUnescape(encoded)
	if err != nil {
		return nil
	}
	ip, _ := response.Request.Context().Value(auditIPKey{}).(string)
	ideAuditWrite(gateFrom(response.Request), ip, ideAuditClean(auditLine))
	return nil
}

// ideAuditClean remplace les caractères de contrôle, de format et de direction (un U+202E retournerait la ligne
// affichée dans l'admin) ainsi que les séparateurs U+2028/U+2029, puis plafonne à ideAuditMax octets.
func ideAuditClean(auditLine string) string {
	auditLine = strings.Map(func(character rune) rune {
		isControl := character < 0x20 || character == 0x7f || (character >= 0x80 && character < 0xa0)
		isLineSeparator := character == 0x2028 || character == 0x2029
		if isControl || character == utf8.RuneError || isLineSeparator || unicode.Is(unicode.Cf, character) {
			return '?'
		}
		return character
	}, auditLine)
	if len(auditLine) <= ideAuditMax {
		return auditLine
	}
	cutIndex := ideAuditMax - len("…")
	for cutIndex > 0 && !utf8.RuneStart(auditLine[cutIndex]) {
		cutIndex--
	}
	return auditLine[:cutIndex] + "…"
}
