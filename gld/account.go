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

func limitLabel(limitKey string) string {
	switch limitKey {
	case "five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet":
		return L("srv.account.limit." + limitKey)
	}
	return L("srv.account.otherLimit", "A1", limitKey)
}

// claudeJSON : le fichier .claude.json de la configuration utilisée (dossier de configuration personnalisé, sinon ~).
func claudeJSON() string {
	if configPath := filepath.Join(cfgDir(), ".claude.json"); fileExists(configPath) {
		return configPath
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".claude.json")
}

func fileExists(path string) bool {
	fileInfo, err := os.Stat(path)
	return err == nil && fileInfo.Mode().IsRegular()
}

const (
	millisecondsPerSecond = 1000
	activityDaysShown     = 14
	unknownLimitRank      = 99
)

func accountInfo() Account {
	account := Account{Limits: []UsageLimit{}, Days: []DayStat{}, Models: []ModelStat{}}
	if claudeConfigContent, err := readTrusted(claudeJSON()); err == nil {
		var claudeConfig struct {
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
		if json.Unmarshal(claudeConfigContent, &claudeConfig) == nil {
			oauthAccount := claudeConfig.OAuth
			account.Email = oauthAccount.Email
			account.Name = oauthAccount.Name
			account.Org = oauthAccount.Org
			account.OrgType = oauthAccount.OrgType
			account.Tier = oauthAccount.Tier
			account.Billing = oauthAccount.Billing
			account.ExtraUsage = oauthAccount.Extra
			account.Since = oauthAccount.Since
			account.ExtraWhy = claudeConfig.ExtraWhy
			account.LimitsAt = claudeConfig.Usage.At / millisecondsPerSecond
			limitKeys := make([]string, 0, len(claudeConfig.Usage.Util))
			for limitKey := range claudeConfig.Usage.Util {
				limitKeys = append(limitKeys, limitKey)
			}
			sort.Strings(limitKeys)
			for _, limitKey := range limitKeys {
				var rawLimit struct {
					Util     *float64 `json:"utilization"`
					ResetsAt string   `json:"resets_at"`
					Limit    *float64 `json:"limit_dollars"`
					Used     *float64 `json:"used_dollars"`
					Locked   *string  `json:"locked_reason"`
				}
				if json.Unmarshal(claudeConfig.Usage.Util[limitKey], &rawLimit) != nil || rawLimit.Util == nil {
					continue // limite absente (null) ou d'une forme inconnue
				}
				usageLimit := UsageLimit{
					Key:      limitKey,
					Label:    limitLabel(limitKey),
					Percent:  *rawLimit.Util,
					ResetsAt: rawLimit.ResetsAt,
					Limit:    rawLimit.Limit,
					Used:     rawLimit.Used,
				}
				if rawLimit.Locked != nil {
					usageLimit.LockedWhy = *rawLimit.Locked
				}
				account.Limits = append(account.Limits, usageLimit)
			}
			sort.SliceStable(account.Limits, func(leftIndex, rightIndex int) bool {
				return limitRank(account.Limits[leftIndex].Key) < limitRank(account.Limits[rightIndex].Key)
			})
		}
	}
	if statsContent, err := readTrusted(filepath.Join(cfgDir(), "stats-cache.json")); err == nil {
		var statsCache struct {
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
		if json.Unmarshal(statsContent, &statsCache) == nil {
			account.TotalSess = statsCache.Total
			account.TotalMsgs = statsCache.Messages
			account.FirstSession = statsCache.First
			account.StatsAt = statsCache.Last
			tokensByDate := map[string]map[string]int{}
			for _, dayTokens := range statsCache.Tokens {
				tokensByDate[dayTokens.Date] = dayTokens.ByMdl
			}
			firstShownDay := max(len(statsCache.Daily)-activityDaysShown, 0)
			for _, dailyActivity := range statsCache.Daily[firstShownDay:] {
				account.Days = append(account.Days, DayStat{
					dailyActivity.Date,
					dailyActivity.Messages,
					dailyActivity.Sessions,
					dailyActivity.Tools,
					tokensByDate[dailyActivity.Date],
				})
			}
		}
		var modelStats struct {
			ModelUsage map[string]struct {
				In     int `json:"inputTokens"`
				Out    int `json:"outputTokens"`
				CRead  int `json:"cacheReadInputTokens"`
				CWrite int `json:"cacheCreationInputTokens"`
			} `json:"modelUsage"`
		}
		if json.Unmarshal(statsContent, &modelStats) == nil {
			for modelName, modelUsage := range modelStats.ModelUsage {
				account.Models = append(account.Models, ModelStat{modelName, modelUsage.In, modelUsage.Out, modelUsage.CRead, modelUsage.CWrite})
			}
			sort.Slice(account.Models, func(leftIndex, rightIndex int) bool {
				return account.Models[leftIndex].Out > account.Models[rightIndex].Out
			})
		}
	}
	return account
}

func limitRank(limitKey string) int {
	for rank, knownKey := range []string{"five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet"} {
		if knownKey == limitKey {
			return rank
		}
	}
	return unknownLimitRank
}

// Le relevé des limites d'usage ne se met à jour que lorsqu'une session de Claude Code le demande. `claude -p "/usage"` s'exécute
// en local (aucun tour, aucun coût, aucune session enregistrée) et rafraîchit ce relevé : on le lance de temps en temps, quand
// le relevé a plus de usageMaxAge, au plus une fois par usageEvery, sans jamais faire attendre la réponse de /account.
const (
	usageMaxAge         = 4 * time.Minute
	usageEvery          = time.Minute
	usageCommandTimeout = 40 * time.Second
)

var (
	usageMu    sync.Mutex
	usageLast  time.Time
	usageBusy  bool
	usageWG    sync.WaitGroup   // les rafraîchissements en vol : les tests attendent leur fin (le journal dépend de l'environnement)
	usageStart = func() error { // remplaçable dans les tests
		commandContext, cancel := context.WithTimeout(context.Background(), usageCommandTimeout)
		defer cancel()
		cmd := withPath(exec.CommandContext(commandContext, "claude", "-p", "/usage", "--no-session-persistence", "--output-format", "json"))
		cmd.Dir = workDir()
		return cmd.Run() // stdin nul : rien à lire
	}
)

// refreshUsage lance la mise à jour en arrière-plan si le relevé est trop vieux (ou si force) ; vrai si elle part.
func refreshUsage(limitsFetchedAt float64, force bool) bool {
	usageMu.Lock()
	defer usageMu.Unlock()
	if usageBusy || time.Since(usageLast) < usageEvery {
		return false
	}
	limitsAreRecent := limitsFetchedAt > 0 && time.Since(time.Unix(int64(limitsFetchedAt), 0)) < usageMaxAge
	if !force && limitsAreRecent {
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
