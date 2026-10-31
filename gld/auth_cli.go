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
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Sans terminal, une seule ligne de stdin, et seulement si GLD_USER_PASSWORD_STDIN=1 (tests, scripts).
func readNewPassword() (string, error) {
	if !stdinIsTerm() {
		if os.Getenv("GLD_USER_PASSWORD_STDIN") != "1" {
			return "", terr("srv.user.terminalRequis")
		}
		return readLine()
	}
	askPassword := func(messageKey string) (string, error) {
		fmt.Fprint(os.Stderr, L(messageKey))
		passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(passwordBytes), err
	}
	firstEntry, err := askPassword("srv.user.motDePasse")
	if err != nil {
		return "", err
	}
	confirmation, err := askPassword("srv.user.motDePasseConfirm")
	if err != nil {
		return "", err
	}
	if firstEntry != confirmation {
		return "", terr("srv.user.motsDePasseDifferents")
	}
	return firstEntry, nil
}

func pwError(err error) error {
	for sentinel, messageKey := range map[error]string{errPasswordShort: "srv.user.mdpCourt", errPasswordLong: "srv.user.mdpLong", errPasswordNUL: "srv.user.mdpNUL", errPasswordCommon: "srv.user.mdpCommun"} {
		if errors.Is(err, sentinel) {
			return terr(messageKey)
		}
	}
	return err
}

func parseRights(names []string) ([]Right, error) {
	rights := make([]Right, 0, len(names))
	for _, name := range names {
		if !validRight(Right(name)) {
			return nil, terr("srv.auth.droitInconnu", "A1", name)
		}
		rights = append(rights, Right(name))
	}
	return rights, nil
}

func cliUser(commandContext context.Context, args []string, output io.Writer) error {
	parsedFlags, err := parseFlags(args, []string{"--admin", "--yes"}, nil, nil)
	if err != nil {
		if err == errHelp {
			return helpText(output, L("cli.user.help"))
		}
		return err
	}
	positional := parsedFlags.pos
	if len(positional) == 0 {
		return helpText(output, L("cli.user.help"))
	}
	expectedArgs := map[string]int{"add": 2, "list": 1, "disable": 2, "enable": 2, "passwd": 2, "reset-mfa": 2, "grant": 4, "revoke": 4}
	expectedCount, isKnownCommand := expectedArgs[positional[0]]
	if !isKnownCommand {
		return terr("srv.user.sousCommandeInconnue", "A1", positional[0])
	}
	if len(positional) < expectedCount || (expectedCount < 4 && len(positional) > expectedCount) {
		return terr("srv.user.arguments", "A1", positional[0])
	}
	var rights []Right
	if expectedCount == 4 {
		if rights, err = parseRights(positional[3:]); err != nil {
			return err
		}
	}
	store, err := openAuthStore()
	if err != nil {
		return err
	}
	defer store.db.Close()
	if positional[0] == "add" {
		password, passwordErr := readNewPassword()
		if passwordErr != nil {
			return passwordErr
		}
		if _, addErr := store.AddUser(positional[1], password, parsedFlags.bools["--admin"]); addErr != nil {
			return pwError(addErr)
		}
		fmt.Fprintln(output, L("srv.user.cree", "A1", normName(positional[1])))
		return nil
	}
	if positional[0] == "list" {
		users, tamperedByID, listErr := store.Users()
		if listErr != nil {
			return listErr
		}
		for _, user := range users {
			var tags []string
			if user.Admin {
				tags = append(tags, L("srv.user.tagAdmin"))
			}
			if user.Disabled {
				tags = append(tags, L("srv.user.tagDesactive"))
			}
			if tamperedByID[user.ID] {
				tags = append(tags, L("srv.user.tagAltere"))
			}
			fmt.Fprintf(output, "%d\t%s\t%s\n", user.ID, user.Name, strings.Join(tags, ", "))
		}
		return nil
	}
	user, err := store.UserByName(positional[1])
	if err != nil {
		return err
	}
	switch positional[0] {
	case "disable", "enable":
		err = store.SetDisabled(user.ID, positional[0] == "disable")
	case "passwd":
		var password string
		if password, err = readNewPassword(); err == nil {
			err = pwError(store.SetPassword(user.ID, password))
		}
	case "reset-mfa":
		err = store.ResetMFA(user.ID)
	case "grant":
		for _, right := range rights {
			if right == RightExec || right == RightConfigWrite || right == RightFsWrite {
				if err = confirmDangerous(output, parsedFlags.bools["--yes"]); err != nil {
					return err
				}
				break
			}
		}
		var homeID string
		if homeID, err = cliHomeID(positional[2]); err == nil {
			err = store.Grant(user.ID, homeID, rights...)
		}
	case "revoke":
		var homeID string
		if homeID, err = cliHomeID(positional[2]); err == nil {
			err = store.Revoke(user.ID, homeID, rights...)
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(output, L("srv.user.fait"))
	return nil
}

var homeIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// cliHomeID : « main », un nom de home ou un identifiant brut (pour révoquer les droits d'un home déjà retiré).
func cliHomeID(arg string) (string, error) {
	if arg == mainHomeID {
		return mainHomeID, nil
	}
	if homeID, found := homeIDFor(arg); found && arg != "" {
		return homeID, nil
	}
	if homeIDRe.MatchString(arg) {
		return arg, nil
	}
	return "", terr("srv.user.homeInconnu", "A1", arg)
}

func confirmDangerous(output io.Writer, yes bool) error {
	fmt.Fprintln(output, L("srv.user.avertExec"))
	if yes {
		return nil
	}
	if !stdinIsTerm() {
		return terr("srv.user.confirmationRequise")
	}
	fmt.Fprint(output, L("srv.user.confirmer"))
	answer, err := readLine()
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "o", "oui", "y", "yes":
		return nil
	}
	return terr("srv.user.annule")
}
