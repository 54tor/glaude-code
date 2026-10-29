import { i18n, loc } from "@/i18n"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { CircleUser, Fan } from "lucide-react"
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
const tier = (a: Account) => TIERS[a.tier ?? ""] ?? (a.orgType ?? "").replace("claude_", "") ?? ""

// Délai avant une échéance, ou depuis un relevé.
function delay(sec: number): string {
  const s = Math.abs(sec)
  const t =
    s < 3600
      ? i18n.t("config.account.minutes", { n: Math.max(1, Math.round(s / 60)) })
      : s < 86400
        ? i18n.t("config.account.hoursMin", { h: Math.floor(s / 3600), m: Math.round((s % 3600) / 60) })
        : i18n.t("config.account.daysHours", { d: Math.floor(s / 86400), h: Math.round((s % 86400) / 3600) })
  return sec >= 0 ? i18n.t("config.account.delayIn", { t }) : i18n.t("config.account.delayAgo", { t })
}

// Une limite dont la fenêtre est déjà close : le pourcentage relevé ne dit plus rien.
const stale = (l: Account["limits"][number], now: number) => !!l.resetsAt && !isNaN(Date.parse(l.resetsAt)) && Date.parse(l.resetsAt) / 1000 < now

const Row = ({ k, children }: { k: string; children: React.ReactNode }) => (
  <div className="flex gap-3 py-0.5 text-sm">
    <span className="w-44 shrink-0 text-muted-foreground">{k}</span>
    <span className="min-w-0 flex-1 break-words">{children}</span>
  </div>
)

// Bouton de l'en-tête : la limite d'usage la plus proche du plafond, et la fenêtre « Compte » (plan, limites, activité, modèles).
export function AccountButton() {
  const { t } = useTranslation()
  const [a, setA] = useState<Account | null>(null)
  const [open, setOpen] = useState(false)
  const [updating, setUpdating] = useState(false)
  const [now, setNow] = useState(() => Date.now() / 1000)
  useEffect(() => {
    let live = true
    const load = () =>
      api.account().then(
        (r) => {
          if (!live) return
          setA(r)
          setNow(Date.now() / 1000)
          const n = Date.now() / 1000
          if (r.limits.some((l) => stale(l, n))) void api.account(true).then((x) => live && setA(x), () => {})
        },
        () => {}
      )
    load()
    const t = setInterval(() => document.visibilityState === "visible" && load(), 60_000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [])
  // Actualiser : le serveur lance « claude -p /usage » (local, sans coût) ; le relevé arrive quelques secondes plus tard.
  const refresh = async () => {
    setUpdating(true)
    setA(await api.account(true))
    for (const wait of [5000, 6000, 8000]) {
      await new Promise((r) => setTimeout(r, wait))
      setA(await api.account())
    }
    setUpdating(false)
  }
  const shown = (["five_hour", "seven_day"] as const).map((k) => a?.limits.find((l) => l.key === k)).filter((l) => !!l)
  return (
    <>
      <Tip label={t("config.account.buttonTitle")}>
        <Button variant="ghost" size="sm" onClick={() => setOpen(true)} aria-label={t("config.account.title")}>
          <CircleUser />
          {shown.map((l) =>
            stale(l, now) ? (
              <Fan key={l.key} className="size-3 animate-spin text-muted-foreground" aria-label={t("config.account.updating")} />
            ) : (
              <span key={l.key} title={l.label} className={cn("text-xs tabular-nums", l.percent >= 90 ? "text-destructive" : l.percent >= 70 ? "text-warning" : "text-muted-foreground")}>
                {Math.round(l.percent)} %
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
            {a ? <Body a={a} updating={updating || !!a.refreshing} onRefresh={refresh} /> : <p className="p-6 text-sm text-muted-foreground">{t("common.loading")}</p>}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

function Body({ a, updating, onRefresh }: { a: Account; updating: boolean; onRefresh: () => void }) {
  const { t } = useTranslation()
  const [now] = useState(() => Date.now() / 1000) // l'heure de l'ouverture : la fenêtre ne bouge pas toute seule
  const days = a.days
  return (
    <div className="space-y-6 px-6 py-5">
      <section>
        <h3 className="mb-1 text-sm font-medium">{t("config.account.title")}</h3>
        <Row k={t("config.account.rowAddress")}>{a.email || t("config.account.unknownFem")}</Row>
        {a.name && <Row k={t("config.account.rowName")}>{a.name}</Row>}
        <Row k={t("config.account.rowPlan")}>{tier(a) || t("config.account.unknown")}</Row>
        {a.org && <Row k={t("config.account.rowOrg")}>{a.org}</Row>}
        {a.billing && <Row k={t("config.account.rowBilling")}>{a.billing === "stripe_subscription" ? t("config.account.subscription") : a.billing}</Row>}
        <Row k={t("config.account.rowExtra")}>{a.extraUsage ? t("config.account.on") : t("config.account.off")}{a.extraWhy ? ` · ${REASONS.has(a.extraWhy) ? t(`config.account.reason.${a.extraWhy}`) : a.extraWhy}` : ""}</Row>
        {a.since && <Row k={t("config.account.rowSince")}>{new Date(a.since).toLocaleDateString(loc(), { dateStyle: "long" })}</Row>}
      </section>

      <section>
        <h3 className="mb-1 text-sm font-medium">{t("config.account.limitsTitle")}</h3>
        {a.limits.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("config.account.noLimits")}</p>
        ) : (
          <ul className="space-y-2">
            {a.limits.map((l) => (
              <li key={l.key}>
                <div className="flex items-baseline gap-2 text-sm">
                  <span className="font-medium">{l.label}</span>
                  {!stale(l, now) && <span className="tabular-nums">{Math.round(l.percent * 10) / 10} %</span>}
                  {l.usedDollars != null && l.limitDollars != null && (
                    <span className="text-muted-foreground">
                      {t("config.account.money", { used: l.usedDollars.toLocaleString(loc(), { maximumFractionDigits: 2 }), limit: l.limitDollars.toLocaleString(loc()) })}
                    </span>
                  )}
                  {l.resetsAt && !isNaN(Date.parse(l.resetsAt)) && (stale(l, now) ? <Fan className="ml-auto size-3.5 animate-spin text-muted-foreground" aria-label={t("config.account.updating")} /> : <span className="ml-auto text-xs text-muted-foreground">{t("config.account.resets", { delay: delay(Date.parse(l.resetsAt) / 1000 - now) })}</span>)}
                </div>
                <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={Math.round(l.percent)} aria-valuemin={0} aria-valuemax={100} aria-label={l.label}>
                  <div className={cn("h-full rounded-full", l.percent >= 90 ? "bg-destructive" : l.percent >= 70 ? "bg-warning" : "bg-success")} style={{ width: `${Math.min(100, l.percent)}%` }} />
                </div>
                {l.lockedWhy && <p className="text-xs text-muted-foreground">{t("config.account.locked", { why: l.lockedWhy })}</p>}
              </li>
            ))}
          </ul>
        )}
        <p className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {a.limitsAt ? t("config.account.statement", { delay: delay(a.limitsAt - now) }) : t("config.account.noStatement")}
          <Button size="xs" variant="outline" onClick={onRefresh} disabled={updating}>
            {updating ? t("config.account.updating") : t("config.account.refresh")}
          </Button>
        </p>
      </section>

      <section>
        <h3 className="mb-1 text-sm font-medium">{t("config.account.activity")}</h3>
        <Row k={t("config.account.metric.sessions")}>{a.firstSession ? t("config.account.sessionsSince", { n: a.totalSessions.toLocaleString(loc()), date: new Date(a.firstSession).toLocaleDateString(loc(), { dateStyle: "long" }) }) : a.totalSessions.toLocaleString(loc())}</Row>
        <Row k={t("config.account.metric.messages")}>{a.totalMessages.toLocaleString(loc())}</Row>
        {days.length > 0 && <DayChart days={a.days} />}
        {a.statsAt && <p className="mt-1 text-xs text-muted-foreground">{t("config.account.statsUntil", { date: new Date(a.statsAt).toLocaleDateString(loc(), { dateStyle: "long" }) })}</p>}
      </section>

      {a.models.length > 0 && (
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
              {a.models.map((m) => (
                <tr key={m.model} className="border-t">
                  <td className="py-1 font-mono">{m.model}</td>
                  <td className="tabular-nums">{compact(m.in)}</td>
                  <td className="tabular-nums">{compact(m.out)}</td>
                  <td className="tabular-nums">{compact(m.cacheRead)}</td>
                  <td className="tabular-nums">{compact(m.cacheWrite)}</td>
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

const dayTok = (t?: Record<string, number>) => Object.values(t ?? {}).reduce((n, v) => n + v, 0)
const METRICS = [
  ["messages", (d: Account["days"][number]) => d.messages],
  ["tokens", (d: Account["days"][number]) => dayTok(d.tokens)],
  ["tools", (d: Account["days"][number]) => d.tools],
  ["sessions", (d: Account["days"][number]) => d.sessions],
] as const

// Activité des derniers jours : une barre par jour, au choix messages, tokens, outils ou sessions ; le détail du jour au survol.
function DayChart({ days }: { days: Account["days"] }) {
  const { t } = useTranslation()
  const [metric, setMetric] = useState<(typeof METRICS)[number][0]>("messages")
  const [, value] = METRICS.find((m) => m[0] === metric)!
  const label = t(`config.account.metric.${metric}`)
  const max = Math.max(1, ...days.map(value))
  return (
    <div className="mt-2">
      <div className="mb-1 flex flex-wrap items-center gap-1">
        {METRICS.map(([k]) => (
          <Button key={k} size="xs" variant={metric === k ? "secondary" : "ghost"} onClick={() => setMetric(k)} aria-pressed={metric === k}>
            {t(`config.account.metric.${k}`)}
          </Button>
        ))}
        <span className="ml-auto text-xs text-muted-foreground">
          {t("config.account.lastDays", { n: days.length, max: compact(max) })}
        </span>
      </div>
      <div className="flex h-32 items-end gap-1 border-b" role="img" aria-label={t("config.account.perDay", { label })}>
        {days.map((d) => {
          const v = value(d)
          const date = new Date(d.date)
          return (
            <div
              key={d.date}
              className="group relative flex h-full min-w-0 flex-1 flex-col justify-end"
              title={t(dayTok(d.tokens) ? "config.account.dayTitleTokens" : "config.account.dayTitle", {
                date: date.toLocaleDateString(loc(), { weekday: "long", day: "numeric", month: "long" }),
                messages: d.messages.toLocaleString(loc()),
                sessions: d.sessions,
                tools: d.tools.toLocaleString(loc()),
                tokens: compact(dayTok(d.tokens)),
              })}
            >
              <span className="mb-0.5 text-center text-2xs tabular-nums text-muted-foreground">{v ? compact(v) : ""}</span>
              <div className="w-full rounded-t bg-primary/70 transition-colors group-hover:bg-primary" style={{ height: `${Math.max(v ? 2 : 0, (v / max) * 100)}%` }} />
            </div>
          )
        })}
      </div>
      <div className="flex gap-1 pt-1">
        {days.map((d) => (
          <span key={d.date} className="min-w-0 flex-1 truncate text-center text-2xs text-muted-foreground">
            {new Date(d.date).getDate()}
          </span>
        ))}
      </div>
    </div>
  )
}
