package main

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const ideAuditMax = 300

// ideAudit journalise une écriture réussie dans auth_events (jamais le contenu). Sans garde, c'est un no-op.
// Format : « <op> <base du dossier>:<chemin relatif>[ <octets>] » ; un jeton porteur est écrit avec user_id NULL.
func ideAudit(r *http.Request, op, dir, rel string, n int64) {
	g := gateFrom(r)
	if g == nil || g.p == nil || g.s == nil {
		return
	}
	var uid any
	if !g.p.Token {
		if g.p.User == nil {
			return
		}
		uid = g.p.User.ID
	}
	out := op + " " + filepath.Base(dir) + ":" + rel
	if n >= 0 {
		out += " " + strconv.FormatInt(n, 10)
	}
	g.s.db.Exec(`INSERT INTO auth_events(at,user_id,ip,kind,outcome) VALUES(?,?,?,?,?)`,
		authNow().Unix(), uid, clientIP(r), "ide_write", ideAuditClean(out))
}

// ideAuditClean remplace les caractères de contrôle et plafonne à ideAuditMax octets.
func ideAuditClean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == utf8.RuneError {
			return '?'
		}
		return r
	}, s)
	if len(s) <= ideAuditMax {
		return s
	}
	cut := ideAuditMax - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
