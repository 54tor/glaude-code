import { useCallback, useEffect, useState } from "react"
import { ArrowRight, ChevronRight, TriangleAlert } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { api, type HomesInfo } from "@/lib/api"
import { tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Choose } from "./Choose"
import { useActions } from "./actions"

type Item = { id: string; kind: string; label: string; status: "new" | "diff" | "same"; risk?: string; merge: boolean; from: string; to: string }
type Mode = "merge" | "replace"

const KINDS = ["setting", "perm", "file", "skill"]
const STATUS = { new: "text-emerald-600 dark:text-emerald-400", diff: "text-amber-600 dark:text-amber-400", same: "text-muted-foreground" } as const

const post = (url: string, body: unknown) =>
  fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }).then((r) => r.json() as Promise<{ ok: boolean; out: string; items?: Item[] }>)

// Copie des réglages d'un dossier de configuration Claude vers un autre : chaque élément se compare, se prévisualise et se choisit
// (fusionner ou remplacer). Le plan vient du serveur, qui revérifie chaque choix avant d'écrire et sauvegarde ce qu'il remplace.
export function SyncTab() {
  const { t } = useTranslation()
  const a = useActions()
  const [info, setInfo] = useState<HomesInfo | null>(null)
  const [from, setFrom] = useState("")
  const [to, setTo] = useState("")
  const [items, setItems] = useState<Item[] | null>(null)
  const [picks, setPicks] = useState<Record<string, Mode>>({})
  const [open, setOpen] = useState<Set<string>>(new Set())
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.homes().then((r) => {
      setInfo(r)
      if (r.homes.length > 0) setTo(r.homes[0].name)
    }, () => {})
  }, [])

  const load = useCallback(async (f: string, dest: string) => {
    setItems(null)
    setPicks({})
    setError("")
    if (f === dest) return setError(t("config.sync.pickTwo"))
    const r = await post("/homes-sync-plan", { from: f, to: dest })
    if (r.ok) setItems(r.items ?? [])
    else setError(r.out)
  }, [t])
  useEffect(() => {
    if (info) load(from, to) // eslint-disable-line react-hooks/set-state-in-effect
  }, [info, from, to, load])

  if (!info) return <p className="py-8 text-sm text-muted-foreground">{t("common.loading")}</p>
  if (info.homes.length === 0)
    return <p className="py-6 text-sm text-muted-foreground">{t("config.sync.addSecond")}</p>

  const options: [string, string][] = [["", t("config.sync.main", { dir: tilde(info.main, a.home) })], ...info.homes.map((h): [string, string] => [h.name, t("config.sync.named", { name: h.name, dir: tilde(h.dir, a.home) })])]
  const todo = (items ?? []).filter((i) => i.status !== "same")
  const chosen = Object.keys(picks)
  const setAll = (f: (i: Item) => Mode | null) => setPicks(Object.fromEntries(todo.flatMap((i) => (f(i) ? [[i.id, f(i)!]] : []))))
  const toggle = (i: Item, on: boolean) =>
    setPicks((p) => {
      const n = { ...p }
      if (on) n[i.id] = i.merge ? "merge" : "replace"
      else delete n[i.id]
      return n
    })
  const apply = async () => {
    setBusy(true)
    const r = await post("/homes-sync-apply", { from, to, picks: chosen.map((id) => ({ id, mode: picks[id] })) })
    setBusy(false)
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) load(from, to)
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Choose value={from} onChange={setFrom} options={options} label={t("config.sync.source")} className="min-w-0 max-w-xs" />
        <ArrowRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <Choose value={to} onChange={setTo} options={options} label={t("config.sync.target")} className="min-w-0 max-w-xs" />
      </div>
      <p className="text-xs text-muted-foreground">
        {t("config.sync.noteBefore")} <code>claude mcp add</code>
        {t("config.sync.noteAfter")}
      </p>
      {error && <p className="text-sm text-destructive">{error}</p>}
      {items && (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => setAll((i) => (i.status === "new" || i.merge ? (i.merge ? "merge" : "replace") : null))} title={t("config.sync.mergeAllTitle")}>
              {t("config.sync.mergeAll")}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setAll(() => "replace")} title={t("config.sync.replaceAllTitle")}>
              {t("config.sync.replaceAll")}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setPicks({})} disabled={chosen.length === 0}>
              {t("config.sync.none")}
            </Button>
            <Button size="sm" className="ml-auto" onClick={apply} disabled={chosen.length === 0 || busy}>
              {chosen.length ? t("config.sync.copyItems", { count: chosen.length }) : t("config.sync.copyNone")}
            </Button>
          </div>
          {todo.length === 0 && <p className="text-sm text-muted-foreground">{t("config.sync.nothingToCopy")}</p>}
          {KINDS.map((kind) => {
            const rows = items.filter((i) => i.kind === kind)
            if (rows.length === 0) return null
            return (
              <section key={kind}>
                <h3 className="text-sm font-medium">{t(`config.sync.kind.${kind}`)}</h3>
                <ul className="mt-1 space-y-px">
                  {rows.map((i) => {
                    const label = t(`config.sync.status.${i.status}`)
                    const tone = STATUS[i.status]
                    const isOpen = open.has(i.id)
                    return (
                      <li key={i.id} className="rounded-md px-2 py-1 text-sm hover:bg-muted">
                        <div className="flex flex-wrap items-center gap-2">
                          <Checkbox checked={i.id in picks} disabled={i.status === "same"} onCheckedChange={(v) => toggle(i, v === true)} aria-label={t("config.sync.copyItem", { label: i.label })} />
                          <button
                            className="flex min-w-0 flex-1 items-center gap-1 text-left"
                            onClick={() =>
                              setOpen((o) => {
                                const n = new Set(o)
                                if (!n.delete(i.id)) n.add(i.id)
                                return n
                              })
                            }
                            aria-expanded={isOpen}
                          >
                            <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", isOpen && "rotate-90")} />
                            <code className="truncate text-xs">{i.label}</code>
                          </button>
                          {i.risk === "exec" && (
                            <span className="flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400" title={t("config.sync.riskTitle")}>
                              <TriangleAlert className="size-3.5" /> {t("config.sync.risk")}
                            </span>
                          )}
                          <span className={cn("shrink-0 text-xs", tone)}>{label}</span>
                          {i.id in picks && i.merge && i.status === "diff" && (
                            <span className="flex shrink-0 gap-1" role="radiogroup" aria-label={t("config.sync.modeAria")}>
                              {(["merge", "replace"] as const).map((m) => (
                                <Button key={m} size="xs" variant={picks[i.id] === m ? "secondary" : "ghost"} role="radio" aria-checked={picks[i.id] === m} onClick={() => setPicks((p) => ({ ...p, [i.id]: m }))}>
                                  {m === "merge" ? t("config.shared.merge") : t("config.shared.replace")}
                                </Button>
                              ))}
                            </span>
                          )}
                        </div>
                        {isOpen && (
                          <div className="mt-1 grid gap-2 md:grid-cols-2">
                            {[
                              ["source", i.from],
                              ["target", i.to],
                            ].map(([side, text]) => (
                              <div key={side}>
                                <p className="text-xs text-muted-foreground">{t(`config.sync.${side}`)}</p>
                                <pre className="max-h-56 overflow-auto rounded-md border bg-background p-2 font-mono text-xs whitespace-pre-wrap">{text || t("config.sync.absent")}</pre>
                              </div>
                            ))}
                          </div>
                        )}
                      </li>
                    )
                  })}
                </ul>
              </section>
            )
          })}
        </>
      )}
    </div>
  )
}
