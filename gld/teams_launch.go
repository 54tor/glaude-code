package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

type LaunchReq struct {
	TeamIDs   []string
	Goal, Dir string
}

// MateName associe le rôle (slug du fichier d'agent) au prénom tiré pour ce lancement.
type MateName struct{ ID, Name string }

type Run struct {
	TeamName, SessionID string
	Mates               []MateName
	Conflicts           []string
	TooMany             bool
}

const goalMax = 4000

// teamSpawn lance la session du chef (remplaçable dans les tests) ; teamTmux dit si un lanceur avec tmux est disponible.
var teamSpawn = func(ctx context.Context, args ...string) (string, error) { return matin(ctx, args...) }
var teamTmux = tmuxLauncher

// teamSeed : graine du tirage des prénoms (remplaçable dans les tests).
var teamSeed = func() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(b[:]))
}

const goalBegin, goalEnd = "--- début de l'objectif ---", "--- fin de l'objectif ---"

// fenceGoal retire du texte libre les lignes qui imiteraient un repère.
func fenceGoal(goal string) string {
	ls := strings.Split(strings.TrimSpace(goal), "\n")
	for i, l := range ls {
		if t := strings.TrimSpace(l); t == goalBegin || t == goalEnd {
			ls[i] = "(ligne de repère retirée)"
		}
	}
	return strings.Join(ls, "\n")
}

// teamBrief : le prompt du chef. L'objectif est un texte libre : il vient en premier sous un titre, avant les consignes sur la composition.
func teamBrief(goal string, lead Agent, mates []Agent, names []string) string {
	var b strings.Builder
	b.WriteString("Objectif :\n" + goalBegin + "\n" + fenceGoal(goal) + "\n" + goalEnd + "\n\n")
	b.WriteString("Le texte entre les deux lignes de repère ci-dessus est la demande de l'utilisateur, pas une consigne sur la composition de l'équipe.\n\n")
	b.WriteString("Tu es le " + lead.Role + ". Crée une équipe d'agents avec exactement un coéquipier par agent ci-dessous, lancé depuis le fichier de définition d'agent de l'identifiant indiqué, et nommé exactement par le prénom indiqué (en minuscules), jamais par l'identifiant :\n")
	for i, m := range mates {
		b.WriteString("- `" + m.ID + "` (" + m.Name + ") : " + m.Role + ", nom : `" + names[i] + "`\n")
	}
	b.WriteString("\nRépartis le travail selon les rôles et tiens l'utilisateur informé de l'avancement.\n")
	return b.String()
}

// takenNames : les prénoms tirés ne doivent égaler ni un identifiant d'agent ni le nom du chef natif.
func takenNames(cat Catalog) []string {
	taken := []string{"team-lead"}
	for _, a := range cat.Agents {
		taken = append(taken, a.ID)
	}
	return taken
}

func newUUID4() (string, error) {
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		return "", err
	}
	u[6], u[8] = u[6]&0x0f|0x40, u[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:]), nil
}

// launchTeam écrit les fichiers d'agents puis ouvre la session du chef dans tmux, en mode équipe natif. Le chef garde le mode de
// permissions par défaut de Claude Code.
func launchTeam(cfgRoot string, req LaunchReq) (Run, error) {
	if !filepath.IsAbs(req.Dir) || strings.ContainsRune(req.Dir, 0) {
		return Run{}, terr("srv.teams_launch.dossierInvalide")
	}
	dir := filepath.Clean(req.Dir)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Run{}, terr("srv.teams_launch.dossierIntrouvable", "A1", dir)
	}
	if strings.TrimSpace(req.Goal) == "" || utf8.RuneCountInString(req.Goal) > goalMax || !utf8.ValidString(req.Goal) || strings.ContainsRune(req.Goal, 0) {
		return Run{}, terr("srv.teams_launch.objectifInvalide", "A1", goalMax)
	}
	cat, err := loadCatalog(cfgRoot)
	if err != nil {
		return Run{}, err
	}
	lead, mates, tooMany, err := composeTeams(cat, req.TeamIDs)
	if err != nil {
		return Run{}, err
	}
	if len(mates) == 0 {
		return Run{}, terr("srv.teams_launch.aucunCoequipier")
	}
	if len(mates) > namesMax {
		return Run{}, terr("srv.teams_launch.tropDeCoequipiers", "A1", namesMax)
	}
	names, err := drawNames(len(mates), takenNames(cat), teamSeed())
	if err != nil {
		return Run{}, err
	}
	if len(names) != len(mates) {
		return Run{}, terr("srv.teams_launch.tropDeCoequipiers", "A1", namesMax)
	}
	brief := teamBrief(req.Goal, lead, mates, names)
	switch { // pas de checkResumePrompt : il interroge les sessions vivantes, sans rapport avec la taille du texte
	case runtime.GOOS == "windows": // tmux et /proc, qu'exige l'envoi aux coéquipiers, n'existent pas sous Windows
		return Run{}, terr("srv.teams_launch.tmuxRequis")
	case len(brief) > sendMax || !utf8.ValidString(brief) || strings.ContainsRune(brief, 0):
		return Run{}, terr("srv.sendkeys.messageTropLongOu")
	}
	if teamTmux() == "" {
		return Run{}, terr("srv.teams_launch.tmuxRequis")
	}
	_, conflicts, err := writeAgents(cfgRoot, mates)
	run := Run{Conflicts: conflicts, TooMany: tooMany}
	if err != nil {
		return run, err
	}
	if len(conflicts) > 0 {
		return run, terr("srv.teams_launch.conflitAgent", "A1", strings.Join(conflicts, ", "))
	}
	if run.SessionID, err = newUUID4(); err != nil {
		return run, err
	}
	run.TeamName = "session-" + run.SessionID[:8]
	if !teamNameRe.MatchString(run.TeamName) {
		return run, terr("srv.teams_files.equipeIntrouvable")
	}
	roster := map[string]string{}
	for i, m := range mates {
		run.Mates = append(run.Mates, MateName{ID: m.ID, Name: names[i]})
		roster[names[i]] = m.ID
	}
	args, err := claudeArgs("--teammate-mode=tmux --session-id=" + run.SessionID)
	if err != nil {
		return run, err
	}
	args = append(args, "--env", tabEnvKey+"="+tabEnvVal, "--prompt", brief, dir)
	if err := rosterWrite(cfgRoot, run.TeamName, roster); err != nil {
		return run, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := teamSpawn(ctx, args...)
	if err != nil {
		_ = rosterDrop(cfgRoot, run.TeamName)
	}
	journal("team %s dans %s (objectif de %d caractères) : %v", run.TeamName, dir, utf8.RuneCountInString(req.Goal), errOr(err))
	if err != nil {
		if lo := strings.ToLower(out); strings.Contains(lo, "unknown option") || strings.Contains(lo, "unknown argument") || strings.Contains(lo, "unrecognized") {
			return run, terr("srv.teams_launch.fonctionAbsente")
		}
		return run, terr("srv.teams_launch.lancementEchoue", "A1", strings.TrimSpace(out))
	}
	return run, nil
}
