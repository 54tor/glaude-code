package main

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	ideAuditMax    = 300
	ideAuditHeader = "X-Gld-Audit"
)

// ideAudit journalise une écriture réussie dans auth_events (jamais le contenu). Sans garde, c'est un no-op.
// Format : « <op> <base du dossier>:<chemin relatif>[ <octets>] » ; un jeton porteur est écrit avec user_id NULL.
// Un enfant n'a pas de base de comptes : il renvoie la ligne à sa passerelle (ideAuditRelay), qui l'écrit.
func ideAudit(w http.ResponseWriter, r *http.Request, op, dir, rel string, n int64) {
	out := op + " " + filepath.Base(dir) + ":" + rel
	if n >= 0 {
		out += " " + strconv.FormatInt(n, 10)
	}
	out = ideAuditClean(out)
	if childToken != "" {
		w.Header().Set(ideAuditHeader, url.QueryEscape(out))
		return
	}
	ideAuditWrite(gateFrom(r), clientIP(r), out)
}

func ideAuditWrite(g *gateInfo, ip, out string) {
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
	g.s.db.Exec(`INSERT INTO auth_events(at,user_id,ip,kind,outcome) VALUES(?,?,?,?,?)`,
		authNow().Unix(), uid, ip, "ide_write", out)
}

type auditIPKey struct{}

// ideAuditRelay (ModifyResponse du proxy d'un enfant) : l'en-tête ne parvient jamais au client ; seule une réponse 2xx
// d'un enfant est journalisée, pour l'utilisateur que la passerelle a authentifié.
func ideAuditRelay(resp *http.Response) error {
	v := resp.Header.Get(ideAuditHeader)
	resp.Header.Del(ideAuditHeader)
	if v == "" || resp.StatusCode/100 != 2 {
		return nil
	}
	out, err := url.QueryUnescape(v)
	if err != nil {
		return nil
	}
	ip, _ := resp.Request.Context().Value(auditIPKey{}).(string)
	ideAuditWrite(gateFrom(resp.Request), ip, ideAuditClean(out))
	return nil
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
