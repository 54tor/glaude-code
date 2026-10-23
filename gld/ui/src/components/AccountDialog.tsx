import { loc } from "@/i18n"
import { useEffect, useState } from "react"
import { CircleUser } from "lucide-react"
import { api, type Account } from "@/lib/api"
import { compact } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

const TIERS: Record<string, string> = {
  default_claude_max_20x: "Max 20x",
  default_claude_max_5x: "Max 5x",
  default_claude_pro: "Pro",
  default_claude_ai: "Pro",
}
const REASONS: Record<string, string> = { out_of_credits: "crédits épuisés", overage_not_provisioned: "non activé pour ce compte", org_level_disabled: "désactivé par l'organisation" }
const tier = (a: Account) => TIERS[a.tier ?? ""] ?? (a.orgType ?? "").replace("claude_", "") ?? ""

// Délai avant une échéance, ou depuis un relevé.
function delay(sec: number): string {
  const s = Math.abs(sec)
  const t = s < 3600 ? `${Math.max(1, Math.round(s / 60))} min` : s < 86400 ? `${Math.floor(s / 3600)} h ${Math.round((s % 3600) / 60)} min` : `${Math.floor(s / 86400)} j ${Math.round((s % 86400) / 3600)} h`
  return sec >= 0 ? `dans ${t}` : `il y a ${t}`
}

const Row = ({ k, children }: { k: string; children: React.ReactNode }) => (
  <div className="flex gap-3 py-0.5 text-sm">
    <span className="w-44 shrink-0 text-muted-foreground">{k}</span>
    <span className="min-w-0 flex-1 break-words">{children}</span>
  </div>
)

// Bouton de l'en-tête : la limite d'usage la plus proche du plafond, et la fenêtre « Compte » (plan, limites, activité, modèles).
export function AccountButton() {
  const [a, setA] = useState<Account | null>(null)
  const [open, setOpen] = useState(false)
  const [updating, setUpdating] = useState(false)
  useEffect(() => {
    let live = true
    const load = () => api.account().then((r) => live && setA(r), () => {})
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
  const top = a?.limits.filter((l) => l.key === "five_hour" || l.key === "seven_day").sort((x, y) => y.percent - x.percent)[0]
  return (
    <>
      <Button variant="ghost" size="sm" onClick={() => setOpen(true)} title="Compte : plan, limites d'usage, activité" aria-label="Compte">
        <CircleUser />
        {top && (
          <span className={cn("text-xs tabular-nums", top.percent >= 90 ? "text-destructive" : top.percent >= 70 ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground")}>
            {Math.round(top.percent)} %
          </span>
        )}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex max-h-[90vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>Compte</DialogTitle>
            <DialogDescription className="sr-only">Plan, limites d'usage, activité et tokens par modèle</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto">
            {a ? <Body a={a} updating={updating || !!a.refreshing} onRefresh={refresh} /> : <p className="p-6 text-sm text-muted-foreground">Chargement…</p>}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

function Body({ a, updating, onRefresh }: { a: Account; updating: boolean; onRefresh: () => void }) {
  const [now] = useState(() => Date.now() / 1000) // l'heure de l'ouverture : la fenêtre ne bouge pas toute seule
  const days = a.days
  return (
    <div className="space-y-6 px-6 py-5">
      <section>
        <h3 className="mb-1 text-sm font-medium">Compte</h3>
        <Row k="Adresse">{a.email || "inconnue"}</Row>
        {a.name && <Row k="Nom">{a.name}</Row>}
        <Row k="Plan">{tier(a) || "inconnu"}</Row>
        {a.org && <Row k="Organisation">{a.org}</Row>}
        {a.billing && <Row k="Facturation">{a.billing === "stripe_subscription" ? "abonnement" : a.billing}</Row>}
        <Row k="Usage supplémentaire">{a.extraUsage ? "activé" : "désactivé"}{a.extraWhy ? ` · ${REASONS[a.extraWhy] ?? a.extraWhy}` : ""}</Row>
        {a.since && <Row k="Abonné depuis">{new Date(a.since).toLocaleDateString(loc(), { dateStyle: "long" })}</Row>}
      </section>

      <section>
        <h3 className="mb-1 text-sm font-medium">Limites d'usage</h3>
        {a.limits.length === 0 ? (
          <p className="text-sm text-muted-foreground">Aucun relevé pour l'instant : il est demandé à Claude Code (un instant).</p>
        ) : (
          <ul className="space-y-2">
            {a.limits.map((l) => (
              <li key={l.key}>
                <div className="flex items-baseline gap-2 text-sm">
                  <span className="font-medium">{l.label}</span>
                  <span className="tabular-nums">{Math.round(l.percent * 10) / 10} %</span>
                  {l.usedDollars != null && l.limitDollars != null && (
                    <span className="text-muted-foreground">
                      {l.usedDollars.toLocaleString(loc(), { maximumFractionDigits: 2 })} $ sur {l.limitDollars.toLocaleString(loc())} $
                    </span>
                  )}
                  {l.resetsAt && !isNaN(Date.parse(l.resetsAt)) && <span className="ml-auto text-xs text-muted-foreground">se renouvelle {delay(Date.parse(l.resetsAt) / 1000 - now)}</span>}
                </div>
                <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={Math.round(l.percent)} aria-valuemin={0} aria-valuemax={100} aria-label={l.label}>
                  <div className={cn("h-full rounded-full", l.percent >= 90 ? "bg-destructive" : l.percent >= 70 ? "bg-amber-500" : "bg-emerald-500")} style={{ width: `${Math.min(100, l.percent)}%` }} />
                </div>
                {l.lockedWhy && <p className="text-xs text-muted-foreground">verrouillée : {l.lockedWhy}</p>}
              </li>
            ))}
          </ul>
        )}
        <p className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {a.limitsAt ? `Relevé ${delay(a.limitsAt - now)}` : "Aucun relevé"}
          <Button size="xs" variant="outline" onClick={onRefresh} disabled={updating}>
            {updating ? "Mise à jour…" : "Actualiser"}
          </Button>
        </p>
      </section>

      <section>
        <h3 className="mb-1 text-sm font-medium">Activité</h3>
        <Row k="Sessions">{a.totalSessions.toLocaleString(loc())}{a.firstSession ? ` depuis le ${new Date(a.firstSession).toLocaleDateString(loc(), { dateStyle: "long" })}` : ""}</Row>
        <Row k="Messages">{a.totalMessages.toLocaleString(loc())}</Row>
        {days.length > 0 && <DayChart days={a.days} />}
        {a.statsAt && <p className="mt-1 text-xs text-muted-foreground">Statistiques calculées par Claude Code jusqu'au {new Date(a.statsAt).toLocaleDateString(loc(), { dateStyle: "long" })}.</p>}
      </section>

      {a.models.length > 0 && (
        <section>
          <h3 className="mb-1 text-sm font-medium">Tokens par modèle (depuis le début)</h3>
          <div className="overflow-x-auto">
          <table className="w-full min-w-[28rem] text-xs">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-1 font-normal">Modèle</th>
                <th className="font-normal">Entrée</th>
                <th className="font-normal">Sortie</th>
                <th className="font-normal">Cache lu</th>
                <th className="font-normal">Cache écrit</th>
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
  ["messages", "Messages", (d: Account["days"][number]) => d.messages],
  ["tokens", "Tokens", (d: Account["days"][number]) => dayTok(d.tokens)],
  ["tools", "Appels d'outils", (d: Account["days"][number]) => d.tools],
  ["sessions", "Sessions", (d: Account["days"][number]) => d.sessions],
] as const

// Activité des derniers jours : une barre par jour, au choix messages, tokens, outils ou sessions ; le détail du jour au survol.
function DayChart({ days }: { days: Account["days"] }) {
  const [metric, setMetric] = useState<(typeof METRICS)[number][0]>("messages")
  const [, label, value] = METRICS.find((m) => m[0] === metric)!
  const max = Math.max(1, ...days.map(value))
  return (
    <div className="mt-2">
      <div className="mb-1 flex flex-wrap items-center gap-1">
        {METRICS.map(([k, l]) => (
          <Button key={k} size="xs" variant={metric === k ? "secondary" : "ghost"} onClick={() => setMetric(k)} aria-pressed={metric === k}>
            {l}
          </Button>
        ))}
        <span className="ml-auto text-xs text-muted-foreground">
          {days.length} derniers jours actifs · max {compact(max)}
        </span>
      </div>
      <div className="flex h-32 items-end gap-1 border-b" role="img" aria-label={`${label} par jour`}>
        {days.map((d) => {
          const v = value(d)
          const date = new Date(d.date)
          return (
            <div
              key={d.date}
              className="group relative flex h-full min-w-0 flex-1 flex-col justify-end"
              title={`${date.toLocaleDateString(loc(), { weekday: "long", day: "numeric", month: "long" })} : ${d.messages.toLocaleString(loc())} messages, ${d.sessions} sessions, ${d.tools.toLocaleString(loc())} outils${dayTok(d.tokens) ? `, ${compact(dayTok(d.tokens))} tokens` : ""}`}
            >
              <span className="mb-0.5 text-center text-[10px] tabular-nums text-muted-foreground">{v ? compact(v) : ""}</span>
              <div className="w-full rounded-t bg-primary/70 transition-colors group-hover:bg-primary" style={{ height: `${Math.max(v ? 2 : 0, (v / max) * 100)}%` }} />
            </div>
          )
        })}
      </div>
      <div className="flex gap-1 pt-1">
        {days.map((d) => (
          <span key={d.date} className="min-w-0 flex-1 truncate text-center text-[10px] text-muted-foreground">
            {new Date(d.date).getDate()}
          </span>
        ))}
      </div>
    </div>
  )
}
