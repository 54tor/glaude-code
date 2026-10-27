package main

// Informations générales du compte, lues dans les fichiers locaux de Claude Code : ~/.claude.json (compte, plan, dernier relevé
// des limites d'usage) et ~/.claude/stats-cache.json (activité). Aucun jeton ni identifiant interne n'est renvoyé.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type UsageLimit struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Percent   float64  `json:"percent"`
	ResetsAt  string   `json:"resetsAt,omitempty"`
	Limit     *float64 `json:"limitDollars,omitempty"`
	Used      *float64 `json:"usedDollars,omitempty"`
	LockedWhy string   `json:"lockedWhy,omitempty"`
}

type DayStat struct {
	Date     string         `json:"date"`
	Messages int            `json:"messages"`
	Sessions int            `json:"sessions"`
	Tools    int            `json:"tools"`
	Tokens   map[string]int `json:"tokens,omitempty"`
}

type Account struct {
	Email        string       `json:"email"`
	Name         string       `json:"name,omitempty"`
	Org          string       `json:"org,omitempty"`
	OrgType      string       `json:"orgType,omitempty"`
	Tier         string       `json:"tier,omitempty"`
	Billing      string       `json:"billing,omitempty"`
	ExtraUsage   bool         `json:"extraUsage"`
	ExtraWhy     string       `json:"extraWhy,omitempty"`
	Since        string       `json:"since,omitempty"`
	Limits       []UsageLimit `json:"limits"`
	LimitsAt     float64      `json:"limitsAt,omitempty"`   // relevé des limites : secondes epoch
	Refreshing   bool         `json:"refreshing,omitempty"` // une mise à jour du relevé vient de partir
	TotalSess    int          `json:"totalSessions"`
	TotalMsgs    int          `json:"totalMessages"`
	FirstSession string       `json:"firstSession,omitempty"`
	Days         []DayStat    `json:"days"` // 14 derniers jours d'activité
	StatsAt      string       `json:"statsAt,omitempty"`
	Models       []ModelStat  `json:"models"`
}

type ModelStat struct {
	Model      string `json:"model"`
	In         int    `json:"in"`
	Out        int    `json:"out"`
	CacheRead  int    `json:"cacheRead"`
	CacheWrite int    `json:"cacheWrite"`
}

func limitLabel(k string) string {
	switch k {
	case "five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet":
		return L("srv.account.limit." + k)
	}
	return L("srv.account.otherLimit", "A1", k)
}

// claudeJSON : le fichier .claude.json de la configuration utilisée (dossier de configuration personnalisé, sinon ~).
func claudeJSON() string {
	if p := filepath.Join(cfgDir(), ".claude.json"); fileExists(p) {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude.json")
}

func fileExists(p string) bool { fi, err := os.Stat(p); return err == nil && fi.Mode().IsRegular() }

func accountInfo() Account {
	a := Account{Limits: []UsageLimit{}, Days: []DayStat{}, Models: []ModelStat{}}
	if b, err := readTrusted(claudeJSON()); err == nil {
		var d struct {
			OAuth struct {
				Email   string `json:"emailAddress"`
				Name    string `json:"displayName"`
				Org     string `json:"organizationName"`
				OrgType string `json:"organizationType"`
				Tier    string `json:"organizationRateLimitTier"`
				Billing string `json:"billingType"`
				Extra   bool   `json:"hasExtraUsageEnabled"`
				Since   string `json:"subscriptionCreatedAt"`
			} `json:"oauthAccount"`
			ExtraWhy string `json:"cachedExtraUsageDisabledReason"`
			Usage    struct {
				At   float64                    `json:"fetchedAtMs"`
				Util map[string]json.RawMessage `json:"utilization"`
			} `json:"cachedUsageUtilization"`
		}
		if json.Unmarshal(b, &d) == nil {
			o := d.OAuth
			a.Email, a.Name, a.Org, a.OrgType, a.Tier, a.Billing, a.ExtraUsage, a.Since = o.Email, o.Name, o.Org, o.OrgType, o.Tier, o.Billing, o.Extra, o.Since
			a.ExtraWhy = d.ExtraWhy
			a.LimitsAt = d.Usage.At / 1000
			keys := make([]string, 0, len(d.Usage.Util))
			for k := range d.Usage.Util {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				var l struct {
					Util     *float64 `json:"utilization"`
					ResetsAt string   `json:"resets_at"`
					Limit    *float64 `json:"limit_dollars"`
					Used     *float64 `json:"used_dollars"`
					Locked   *string  `json:"locked_reason"`
				}
				if json.Unmarshal(d.Usage.Util[k], &l) != nil || l.Util == nil {
					continue // limite absente (null) ou d'une forme inconnue
				}
				label := limitLabel(k)
				u := UsageLimit{Key: k, Label: label, Percent: *l.Util, ResetsAt: l.ResetsAt, Limit: l.Limit, Used: l.Used}
				if l.Locked != nil {
					u.LockedWhy = *l.Locked
				}
				a.Limits = append(a.Limits, u)
			}
			sort.SliceStable(a.Limits, func(i, j int) bool { return limitRank(a.Limits[i].Key) < limitRank(a.Limits[j].Key) })
		}
	}
	if b, err := readTrusted(filepath.Join(cfgDir(), "stats-cache.json")); err == nil {
		var s struct {
			Last  string `json:"lastComputedDate"`
			Daily []struct {
				Date     string `json:"date"`
				Messages int    `json:"messageCount"`
				Sessions int    `json:"sessionCount"`
				Tools    int    `json:"toolCallCount"`
			} `json:"dailyActivity"`
			Tokens []struct {
				Date  string         `json:"date"`
				ByMdl map[string]int `json:"tokensByModel"`
			} `json:"dailyModelTokens"`
			Total    int    `json:"totalSessions"`
			Messages int    `json:"totalMessages"`
			First    string `json:"firstSessionDate"`
		}
		if json.Unmarshal(b, &s) == nil {
			a.TotalSess, a.TotalMsgs, a.FirstSession, a.StatsAt = s.Total, s.Messages, s.First, s.Last
			tok := map[string]map[string]int{}
			for _, t := range s.Tokens {
				tok[t.Date] = t.ByMdl
			}
			from := len(s.Daily) - 14
			if from < 0 {
				from = 0
			}
			for _, d := range s.Daily[from:] {
				a.Days = append(a.Days, DayStat{d.Date, d.Messages, d.Sessions, d.Tools, tok[d.Date]})
			}
		}
		var m struct {
			ModelUsage map[string]struct {
				In     int `json:"inputTokens"`
				Out    int `json:"outputTokens"`
				CRead  int `json:"cacheReadInputTokens"`
				CWrite int `json:"cacheCreationInputTokens"`
			} `json:"modelUsage"`
		}
		if json.Unmarshal(b, &m) == nil {
			for k, v := range m.ModelUsage {
				a.Models = append(a.Models, ModelStat{k, v.In, v.Out, v.CRead, v.CWrite})
			}
			sort.Slice(a.Models, func(i, j int) bool { return a.Models[i].Out > a.Models[j].Out })
		}
	}
	return a
}

func limitRank(k string) int {
	for i, x := range []string{"five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet"} {
		if x == k {
			return i
		}
	}
	return 99
}

// Le relevé des limites d'usage ne se met à jour que lorsqu'une session de Claude Code le demande. `claude -p "/usage"` s'exécute
// en local (aucun tour, aucun coût, aucune session enregistrée) et rafraîchit ce relevé : on le lance de temps en temps, quand
// le relevé a plus de usageMaxAge, au plus une fois par usageEvery, sans jamais faire attendre la réponse de /account.
const (
	usageMaxAge = 4 * time.Minute
	usageEvery  = time.Minute
)

var (
	usageMu    sync.Mutex
	usageLast  time.Time
	usageBusy  bool
	usageWG    sync.WaitGroup   // les rafraîchissements en vol : les tests attendent leur fin (le journal dépend de l'environnement)
	usageStart = func() error { // remplaçable dans les tests
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		cmd := withPath(exec.CommandContext(ctx, "claude", "-p", "/usage", "--no-session-persistence", "--output-format", "json"))
		cmd.Dir = workDir()
		return cmd.Run() // stdin nul : rien à lire
	}
)

// refreshUsage lance la mise à jour en arrière-plan si le relevé est trop vieux (ou si force) ; vrai si elle part.
func refreshUsage(at float64, force bool) bool {
	usageMu.Lock()
	defer usageMu.Unlock()
	if usageBusy || time.Since(usageLast) < usageEvery {
		return false
	}
	if !force && at > 0 && time.Since(time.Unix(int64(at), 0)) < usageMaxAge {
		return false
	}
	usageBusy, usageLast = true, time.Now()
	start := usageStart // lu ici, sous le verrou : la goroutine n'y touche plus
	usageWG.Add(1)
	go func() {
		defer usageWG.Done()
		err := start()
		journal("usage : relevé des limites rafraîchi : %v", errOr(err))
		usageMu.Lock()
		usageBusy = false
		usageMu.Unlock()
	}()
	return true
}
