package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"golang.org/x/term"
)

func stdinIsTerm() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// Un lecteur neuf à chaque appel : une invocation ne lit qu'une ligne.
func readLine() (string, error) {
	s, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && s != "") {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// Sans terminal, une seule ligne de stdin, et seulement si GLD_USER_PASSWORD_STDIN=1 (tests, scripts).
func readNewPassword() (string, error) {
	if !stdinIsTerm() {
		if os.Getenv("GLD_USER_PASSWORD_STDIN") != "1" {
			return "", terr("srv.user.terminalRequis")
		}
		return readLine()
	}
	ask := func(id string) (string, error) {
		fmt.Fprint(os.Stderr, L(id))
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	a, err := ask("srv.user.motDePasse")
	if err != nil {
		return "", err
	}
	b, err := ask("srv.user.motDePasseConfirm")
	if err != nil {
		return "", err
	}
	if a != b {
		return "", terr("srv.user.motsDePasseDifferents")
	}
	return a, nil
}

func pwError(err error) error {
	for e, id := range map[error]string{errPasswordShort: "srv.user.mdpCourt", errPasswordLong: "srv.user.mdpLong", errPasswordNUL: "srv.user.mdpNUL", errPasswordCommon: "srv.user.mdpCommun"} {
		if errors.Is(err, e) {
			return terr(id)
		}
	}
	return err
}

func parseRights(names []string) ([]Right, error) {
	out := make([]Right, 0, len(names))
	for _, n := range names {
		if !validRight(Right(n)) {
			return nil, terr("srv.auth.droitInconnu", "A1", n)
		}
		out = append(out, Right(n))
	}
	return out, nil
}

func cliUser(ctx context.Context, args []string, out io.Writer) error {
	f, err := parseFlags(args, []string{"--admin", "--yes"}, nil, nil)
	if err != nil {
		if err == errHelp {
			return helpText(out, L("cli.user.help"))
		}
		return err
	}
	p := f.pos
	if len(p) == 0 {
		return helpText(out, L("cli.user.help"))
	}
	want := map[string]int{"add": 2, "list": 1, "disable": 2, "enable": 2, "passwd": 2, "reset-mfa": 2, "grant": 4, "revoke": 4}
	n, ok := want[p[0]]
	if !ok {
		return terr("srv.user.sousCommandeInconnue", "A1", p[0])
	}
	if len(p) < n || (n < 4 && len(p) > n) {
		return terr("srv.user.arguments", "A1", p[0])
	}
	var rights []Right
	if n == 4 {
		if rights, err = parseRights(p[3:]); err != nil {
			return err
		}
	}
	s, err := openAuthStore()
	if err != nil {
		return err
	}
	defer s.db.Close()
	if p[0] == "add" {
		pw, err := readNewPassword()
		if err != nil {
			return err
		}
		if _, err := s.AddUser(p[1], pw, f.bools["--admin"]); err != nil {
			return pwError(err)
		}
		fmt.Fprintln(out, L("srv.user.cree", "A1", normName(p[1])))
		return nil
	}
	if p[0] == "list" {
		users, bad, err := s.Users()
		if err != nil {
			return err
		}
		for _, u := range users {
			var tags []string
			if u.Admin {
				tags = append(tags, L("srv.user.tagAdmin"))
			}
			if u.Disabled {
				tags = append(tags, L("srv.user.tagDesactive"))
			}
			if bad[u.ID] {
				tags = append(tags, L("srv.user.tagAltere"))
			}
			fmt.Fprintf(out, "%d\t%s\t%s\n", u.ID, u.Name, strings.Join(tags, ", "))
		}
		return nil
	}
	u, err := s.UserByName(p[1])
	if err != nil {
		return err
	}
	switch p[0] {
	case "disable", "enable":
		err = s.SetDisabled(u.ID, p[0] == "disable")
	case "passwd":
		var pw string
		if pw, err = readNewPassword(); err == nil {
			err = pwError(s.SetPassword(u.ID, pw))
		}
	case "reset-mfa":
		err = s.ResetMFA(u.ID)
	case "grant":
		for _, r := range rights {
			if r == RightExec || r == RightConfigWrite || r == RightFsWrite {
				if err = confirmDangerous(out, f.bools["--yes"]); err != nil {
					return err
				}
				break
			}
		}
		var id string
		if id, err = cliHomeID(p[2]); err == nil {
			err = s.Grant(u.ID, id, rights...)
		}
	case "revoke":
		var id string
		if id, err = cliHomeID(p[2]); err == nil {
			err = s.Revoke(u.ID, id, rights...)
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(out, L("srv.user.fait"))
	return nil
}

var homeIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// cliHomeID : « main », un nom de home ou un identifiant brut (pour révoquer les droits d'un home déjà retiré).
func cliHomeID(arg string) (string, error) {
	if arg == mainHomeID {
		return mainHomeID, nil
	}
	if id, ok := homeIDFor(arg); ok && arg != "" {
		return id, nil
	}
	if homeIDRe.MatchString(arg) {
		return arg, nil
	}
	return "", terr("srv.user.homeInconnu", "A1", arg)
}

func confirmDangerous(out io.Writer, yes bool) error {
	fmt.Fprintln(out, L("srv.user.avertExec"))
	if yes {
		return nil
	}
	if !stdinIsTerm() {
		return terr("srv.user.confirmationRequise")
	}
	fmt.Fprint(out, L("srv.user.confirmer"))
	a, err := readLine()
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "o", "oui", "y", "yes":
		return nil
	}
	return terr("srv.user.annule")
}
