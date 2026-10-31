import { i18n, loc } from "@/i18n"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Fan, Gauge } from "lucide-react"
import { api, type Account } from "@/lib/api"
import { compact } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Tip } from "@/components/ui/tooltip"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

const TIERS: Record<string, string> = {
  default_claude_max_20x: "Max 20x",
  default_claude_max_5x: "Max 5x",
  default_claude_pro: "Pro",
  default_claude_ai: "Pro",
}
const REASONS = new Set(["out_of_credits", "overage_not_provisioned", "org_level_disabled"])
const SECONDS_PER_MINUTE = 60
const SECONDS_PER_HOUR = 3600
const SECONDS_PER_DAY = 86400
const MILLISECONDS_PER_SECOND = 1000
const ACCOUNT_REFRESH_INTERVAL_MS = 60_000
const USAGE_POLL_DELAYS_MS = [5000, 6000, 8000]
const CRITICAL_PERCENT = 90
const WARNING_PERCENT = 70
const FULL_PERCENT = 100
const MIN_VISIBLE_BAR_PERCENT = 2

type Limit = Account["limits"][number]
type Day = Account["days"][number]

const nowInSeconds = () => Date.now() / MILLISECONDS_PER_SECOND

function accountTierLabel(account: Account) {
  return TIERS[account.tier ?? ""] ?? (account.orgType ?? "").replace("claude_", "")
}

function formatDuration(absoluteSeconds: number): string {
  if (absoluteSeconds < SECONDS_PER_HOUR) {
    return i18n.t("config.account.minutes", { n: Math.max(1, Math.round(absoluteSeconds / SECONDS_PER_MINUTE)) })
  }
  if (absoluteSeconds < SECONDS_PER_DAY) {
    return i18n.t("config.account.hoursMin", {
      h: Math.floor(absoluteSeconds / SECONDS_PER_HOUR),
      m: Math.round((absoluteSeconds % SECONDS_PER_HOUR) / SECONDS_PER_MINUTE),
    })
  }
  return i18n.t("config.account.daysHours", {
    d: Math.floor(absoluteSeconds / SECONDS_PER_DAY),
    h: Math.round((absoluteSeconds % SECONDS_PER_DAY) / SECONDS_PER_HOUR),
  })
}

// Délai avant une échéance, ou depuis un relevé.
function delay(signedSeconds: number): string {
  const duration = formatDuration(Math.abs(signedSeconds))
  if (signedSeconds >= 0) return i18n.t("config.account.delayIn", { t: duration })
  return i18n.t("config.account.delayAgo", { t: duration })
}

const hasValidResetDate = (limit: Limit): limit is Limit & { resetsAt: string } => !!limit.resetsAt && !isNaN(Date.parse(limit.resetsAt))

// Une limite dont la fenêtre est déjà close : le pourcentage relevé ne dit plus rien.
const stale = (limit: Limit, now: number) => hasValidResetDate(limit) && Date.parse(limit.resetsAt) / MILLISECONDS_PER_SECOND < now

function percentTextTone(percent: number) {
  if (percent >= CRITICAL_PERCENT) return "text-destructive"
  if (percent >= WARNING_PERCENT) return "text-warning"
  return "text-muted-foreground"
}

function percentBarTone(percent: number) {
  if (percent >= CRITICAL_PERCENT) return "bg-destructive"
  if (percent >= WARNING_PERCENT) return "bg-warning"
  return "bg-success"
}

const formatLongDate = (date: string | number) => new Date(date).toLocaleDateString(loc(), { dateStyle: "long" })

const Row = ({ label, children }: { label: string; children: React.ReactNode }) => (
  <div className="flex gap-3 py-0.5 text-sm">
    <span className="w-44 shrink-0 text-muted-foreground">{label}</span>
    <span className="min-w-0 flex-1 break-words">{children}</span>
  </div>
)

// Bouton de l'en-tête : la limite d'usage la plus proche du plafond, et la fenêtre « Compte » (plan, limites, activité, modèles).
export function AccountButton() {
  const { t } = useTranslation()
  const [account, setAccount] = useState<Account | null>(null)
  const [open, setOpen] = useState(false)
  const [updating, setUpdating] = useState(false)
  const [now, setNow] = useState(nowInSeconds)
  useEffect(() => {
    let isMounted = true
    const loadAccount = () =>
      api.account().then(
        (loadedAccount) => {
          if (!isMounted) return
          setAccount(loadedAccount)
          setNow(nowInSeconds())
          const loadedAt = nowInSeconds()
          const hasStaleLimit = loadedAccount.limits.some((limit) => stale(limit, loadedAt))
          if (!hasStaleLimit) return
          void api.account(true).then(
            (refreshedAccount) => {
              if (isMounted) setAccount(refreshedAccount)
            },
            () => {},
          )
        },
        () => {}
      )
    loadAccount()
    const refreshTimer = setInterval(() => {
      if (document.visibilityState === "visible") loadAccount()
    }, ACCOUNT_REFRESH_INTERVAL_MS)
    return () => {
      isMounted = false
      clearInterval(refreshTimer)
    }
  }, [])
  // Actualiser : le serveur lance « claude -p /usage » (local, sans coût) ; le relevé arrive quelques secondes plus tard.
  const refresh = async () => {
    setUpdating(true)
    setAccount(await api.account(true))
    for (const waitMs of USAGE_POLL_DELAYS_MS) {
      await new Promise((resolve) => setTimeout(resolve, waitMs))
      setAccount(await api.account())
    }
    setUpdating(false)
  }
  const shownLimits = (["five_hour", "seven_day"] as const)
    .map((limitKey) => account?.limits.find((limit) => limit.key === limitKey))
    .filter((limit) => !!limit)
  return (
    <>
      <Tip label={t("config.account.buttonTitle")}>
        <Button variant="ghost" size="sm" onClick={() => setOpen(true)} aria-label={t("config.account.title")}>
          <Gauge />
          {shownLimits.map((limit) =>
            stale(limit, now) ? (
              <Fan key={limit.key} className="size-3 animate-spin text-muted-foreground" aria-label={t("config.account.updating")} />
            ) : (
              <span key={limit.key} title={limit.label} className={cn("text-xs tabular-nums", percentTextTone(limit.percent))}>
                {Math.round(limit.percent)} %
              </span>
            )
          )}
        </Button>
      </Tip>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex max-h-[90vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>{t("config.account.title")}</DialogTitle>
            <DialogDescription className="sr-only">{t("config.account.srDesc")}</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto">
            {account ? <Body account={account} updating={updating || !!account.refreshing} onRefresh={refresh} /> : <p className="p-6 text-sm text-muted-foreground">{t("common.loading")}</p>}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

function Body({ account, updating, onRefresh }: { account: Account; updating: boolean; onRefresh: () => void }) {
  const { t } = useTranslation()
  const [now] = useState(nowInSeconds) // l'heure de l'ouverture : la fenêtre ne bouge pas toute seule
  const days = account.days
  const billingLabel = account.billing === "stripe_subscription" ? t("config.account.subscription") : account.billing
  const extraUsageLabel = account.extraUsage ? t("config.account.on") : t("config.account.off")
  let extraUsageReason = ""
  if (account.extraWhy) {
    const reasonLabel = REASONS.has(account.extraWhy) ? t(`config.account.reason.${account.extraWhy}`) : account.extraWhy
    extraUsageReason = ` · ${reasonLabel}`
  }
  const totalSessionsLabel = account.totalSessions.toLocaleString(loc())
  const sessionsSummary = account.firstSession
    ? t("config.account.sessionsSince", { n: totalSessionsLabel, date: formatLongDate(account.firstSession) })
    : totalSessionsLabel
  return (
    <div className="space-y-6 px-6 py-5">
      <section>
        <h3 className="mb-1 text-sm font-medium">{t("config.account.title")}</h3>
        <Row label={t("config.account.rowAddress")}>{account.email || t("config.account.unknownFem")}</Row>
        {account.name && <Row label={t("config.account.rowName")}>{account.name}</Row>}
        <Row label={t("config.account.rowPlan")}>{accountTierLabel(account) || t("config.account.unknown")}</Row>
        {account.org && <Row label={t("config.account.rowOrg")}>{account.org}</Row>}
        {account.billing && <Row label={t("config.account.rowBilling")}>{billingLabel}</Row>}
        <Row label={t("config.account.rowExtra")}>{extraUsageLabel}{extraUsageReason}</Row>
        {account.since && <Row label={t("config.account.rowSince")}>{formatLongDate(account.since)}</Row>}
      </section>

      <section>
        <h3 className="mb-1 text-sm font-medium">{t("config.account.limitsTitle")}</h3>
        {account.limits.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("config.account.noLimits")}</p>
        ) : (
          <ul className="space-y-2">
            {account.limits.map((limit) => (
              <LimitItem key={limit.key} limit={limit} now={now} />
            ))}
          </ul>
        )}
        <p className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {account.limitsAt ? t("config.account.statement", { delay: delay(account.limitsAt - now) }) : t("config.account.noStatement")}
          <Button size="xs" variant="outline" onClick={onRefresh} disabled={updating}>
            {updating ? t("config.account.updating") : t("config.account.refresh")}
          </Button>
        </p>
      </section>

      <section>
        <h3 className="mb-1 text-sm font-medium">{t("config.account.activity")}</h3>
        <Row label={t("config.account.metric.sessions")}>{sessionsSummary}</Row>
        <Row label={t("config.account.metric.messages")}>{account.totalMessages.toLocaleString(loc())}</Row>
        {days.length > 0 && <DayChart days={account.days} />}
        {account.statsAt && <p className="mt-1 text-xs text-muted-foreground">{t("config.account.statsUntil", { date: formatLongDate(account.statsAt) })}</p>}
      </section>

      {account.models.length > 0 && (
        <section>
          <h3 className="mb-1 text-sm font-medium">{t("config.account.tokensTitle")}</h3>
          <div className="overflow-x-auto">
          <table className="w-full min-w-[28rem] text-xs">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-1 font-normal">{t("config.account.colModel")}</th>
                <th className="font-normal">{t("config.account.colIn")}</th>
                <th className="font-normal">{t("config.account.colOut")}</th>
                <th className="font-normal">{t("config.account.colCacheRead")}</th>
                <th className="font-normal">{t("config.account.colCacheWrite")}</th>
              </tr>
            </thead>
            <tbody>
              {account.models.map((modelUsage) => (
                <tr key={modelUsage.model} className="border-t">
                  <td className="py-1 font-mono">{modelUsage.model}</td>
                  <td className="tabular-nums">{compact(modelUsage.in)}</td>
                  <td className="tabular-nums">{compact(modelUsage.out)}</td>
                  <td className="tabular-nums">{compact(modelUsage.cacheRead)}</td>
                  <td className="tabular-nums">{compact(modelUsage.cacheWrite)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          </div>
        </section>
      )}
    </div>
  )
}

function LimitItem({ limit, now }: { limit: Limit; now: number }) {
  const { t } = useTranslation()
  const isStale = stale(limit, now)
  const { usedDollars, limitDollars } = limit
  let resetIndicator: React.ReactNode = null
  if (hasValidResetDate(limit)) {
    resetIndicator = isStale ? (
      <Fan className="ml-auto size-3.5 animate-spin text-muted-foreground" aria-label={t("config.account.updating")} />
    ) : (
      <span className="ml-auto text-xs text-muted-foreground">{t("config.account.resets", { delay: delay(Date.parse(limit.resetsAt) / MILLISECONDS_PER_SECOND - now) })}</span>
    )
  }
  return (
    <li>
      <div className="flex items-baseline gap-2 text-sm">
        <span className="font-medium">{limit.label}</span>
        {!isStale && <span className="tabular-nums">{Math.round(limit.percent * 10) / 10} %</span>}
        {usedDollars != null && limitDollars != null && (
          <span className="text-muted-foreground">
            {t("config.account.money", { used: usedDollars.toLocaleString(loc(), { maximumFractionDigits: 2 }), limit: limitDollars.toLocaleString(loc()) })}
          </span>
        )}
        {resetIndicator}
      </div>
      <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={Math.round(limit.percent)} aria-valuemin={0} aria-valuemax={FULL_PERCENT} aria-label={limit.label}>
        <div className={cn("h-full rounded-full", percentBarTone(limit.percent))} style={{ width: `${Math.min(FULL_PERCENT, limit.percent)}%` }} />
      </div>
      {limit.lockedWhy && <p className="text-xs text-muted-foreground">{t("config.account.locked", { why: limit.lockedWhy })}</p>}
    </li>
  )
}

const dayTokens = (tokensByModel?: Record<string, number>) => Object.values(tokensByModel ?? {}).reduce((total, tokenCount) => total + tokenCount, 0)
const METRICS = [
  ["messages", (day: Day) => day.messages],
  ["tokens", (day: Day) => dayTokens(day.tokens)],
  ["tools", (day: Day) => day.tools],
  ["sessions", (day: Day) => day.sessions],
] as const

// Activité des derniers jours : une barre par jour, au choix messages, tokens, outils ou sessions ; le détail du jour au survol.
function DayChart({ days }: { days: Account["days"] }) {
  const { t } = useTranslation()
  const [metric, setMetric] = useState<(typeof METRICS)[number][0]>("messages")
  const [, metricValue] = METRICS.find(([metricKey]) => metricKey === metric)!
  const label = t(`config.account.metric.${metric}`)
  const max = Math.max(1, ...days.map(metricValue))
  return (
    <div className="mt-2">
      <div className="mb-1 flex flex-wrap items-center gap-1">
        {METRICS.map(([metricKey]) => (
          <Button key={metricKey} size="xs" variant={metric === metricKey ? "secondary" : "ghost"} onClick={() => setMetric(metricKey)} aria-pressed={metric === metricKey}>
            {t(`config.account.metric.${metricKey}`)}
          </Button>
        ))}
        <span className="ml-auto text-xs text-muted-foreground">
          {t("config.account.lastDays", { n: days.length, max: compact(max) })}
        </span>
      </div>
      <div className="flex h-32 items-end gap-1 border-b" role="img" aria-label={t("config.account.perDay", { label })}>
        {days.map((day) => {
          const dayValue = metricValue(day)
          const date = new Date(day.date)
          const tokenTotal = dayTokens(day.tokens)
          const minimumBarPercent = dayValue ? MIN_VISIBLE_BAR_PERCENT : 0
          const barHeightPercent = Math.max(minimumBarPercent, (dayValue / max) * FULL_PERCENT)
          return (
            <div
              key={day.date}
              className="group relative flex h-full min-w-0 flex-1 flex-col justify-end"
              title={t(tokenTotal ? "config.account.dayTitleTokens" : "config.account.dayTitle", {
                date: date.toLocaleDateString(loc(), { weekday: "long", day: "numeric", month: "long" }),
                messages: day.messages.toLocaleString(loc()),
                sessions: day.sessions,
                tools: day.tools.toLocaleString(loc()),
                tokens: compact(tokenTotal),
              })}
            >
              <span className="mb-0.5 text-center text-2xs tabular-nums text-muted-foreground">{dayValue ? compact(dayValue) : ""}</span>
              <div className="w-full rounded-t bg-primary/70 transition-colors group-hover:bg-primary" style={{ height: `${barHeightPercent}%` }} />
            </div>
          )
        })}
      </div>
      <div className="flex gap-1 pt-1">
        {days.map((day) => (
          <span key={day.date} className="min-w-0 flex-1 truncate text-center text-2xs text-muted-foreground">
            {new Date(day.date).getDate()}
          </span>
        ))}
      </div>
    </div>
  )
}
