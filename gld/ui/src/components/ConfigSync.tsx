import { useCallback, useEffect, useState } from "react"
import { ArrowRight, ChevronRight, TriangleAlert } from "lucide-react"
import { toast } from "sonner"
import { api, type HomesInfo } from "@/lib/api"
import { tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Choose } from "./Choose"
import { useActions } from "./actions"

type Item = { id: string; kind: string; label: string; status: "new" | "diff" | "same"; risk?: string; merge: boolean; from: string; to: string }
type Mode = "merge" | "replace"

const KINDS: [string, string][] = [
  ["setting", "Réglages (settings.json)"],
  ["perm", "Règles de permission"],
  ["file", "CLAUDE.md, règles et commandes"],
  ["skill", "Skills"],
]
const STATUS = { new: ["nouveau", "text-emerald-600 dark:text-emerald-400"], diff: ["différent", "text-amber-600 dark:text-amber-400"], same: ["identique", "text-muted-foreground"] } as const

const post = (url: string, body: unknown) =>
  fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }).then((r) => r.json() as Promise<{ ok: boolean; out: string; items?: Item[] }>)

// Copie des réglages d'un dossier de configuration Claude vers un autre : chaque élément se compare, se prévisualise et se choisit
// (fusionner ou remplacer). Le plan vient du serveur, qui revérifie chaque choix avant d'écrire et sauvegarde ce qu'il remplace.
export function SyncTab() {
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

  const load = useCallback(async (f: string, t: string) => {
    setItems(null)
    setPicks({})
    setError("")
    if (f === t) return setError("Choisis deux dossiers différents.")
    const r = await post("/homes-sync-plan", { from: f, to: t })
    if (r.ok) setItems(r.items ?? [])
    else setError(r.out)
  }, [])
  useEffect(() => {
    if (info) load(from, to) // eslint-disable-line react-hooks/set-state-in-effect
  }, [info, from, to, load])

  if (!info) return <p className="py-8 text-sm text-muted-foreground">Chargement…</p>
  if (info.homes.length === 0)
    return <p className="py-6 text-sm text-muted-foreground">Ajoute d'abord un deuxième dossier de configuration (bouton en haut de la barre latérale) pour synchroniser deux dossiers.</p>

  const options: [string, string][] = [["", `Principal : ${tilde(info.main, a.home)}`], ...info.homes.map((h): [string, string] => [h.name, `${h.name} : ${tilde(h.dir, a.home)}`])]
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
        <Choose value={from} onChange={setFrom} options={options} label="Source" className="min-w-0 max-w-xs" />
        <ArrowRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <Choose value={to} onChange={setTo} options={options} label="Cible" className="min-w-0 max-w-xs" />
      </div>
      <p className="text-xs text-muted-foreground">
        Copie de la source vers la cible. Les secrets (env, jetons, helpers d'authentification) ne sont jamais proposés ; les fichiers remplacés sont sauvegardés dans la cible. Les serveurs MCP sont hors de cette copie : ils vivent dans
        .claude.json, réécrit en continu par les sessions ouvertes (utilise <code>claude mcp add</code>).
      </p>
      {error && <p className="text-sm text-destructive">{error}</p>}
      {items && (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => setAll((i) => (i.status === "new" || i.merge ? (i.merge ? "merge" : "replace") : null))} title="Ajoute ce qui manque ; la cible garde ses valeurs en cas de désaccord">
              Tout fusionner
            </Button>
            <Button size="sm" variant="outline" onClick={() => setAll(() => "replace")} title="La source écrase la cible pour tout ce qui diffère">
              Tout remplacer
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setPicks({})} disabled={chosen.length === 0}>
              Aucun
            </Button>
            <Button size="sm" className="ml-auto" onClick={apply} disabled={chosen.length === 0 || busy}>
              Copier {chosen.length || ""} élément{chosen.length > 1 ? "s" : ""}
            </Button>
          </div>
          {todo.length === 0 && <p className="text-sm text-muted-foreground">Rien à copier : la cible a déjà tout.</p>}
          {KINDS.map(([kind, title]) => {
            const rows = items.filter((i) => i.kind === kind)
            if (rows.length === 0) return null
            return (
              <section key={kind}>
                <h3 className="text-sm font-medium">{title}</h3>
                <ul className="mt-1 space-y-px">
                  {rows.map((i) => {
                    const [label, tone] = STATUS[i.status]
                    const isOpen = open.has(i.id)
                    return (
                      <li key={i.id} className="rounded-md px-2 py-1 text-sm hover:bg-muted">
                        <div className="flex flex-wrap items-center gap-2">
                          <Checkbox checked={i.id in picks} disabled={i.status === "same"} onCheckedChange={(v) => toggle(i, v === true)} aria-label={`Copier ${i.label}`} />
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
                            <span className="flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400" title="Contient des commandes qui s'exécuteront dans la cible">
                              <TriangleAlert className="size-3.5" /> exécute des commandes
                            </span>
                          )}
                          <span className={cn("shrink-0 text-xs", tone)}>{label}</span>
                          {i.id in picks && i.merge && i.status === "diff" && (
                            <span className="flex shrink-0 gap-1" role="radiogroup" aria-label="Mode">
                              {(["merge", "replace"] as const).map((m) => (
                                <Button key={m} size="xs" variant={picks[i.id] === m ? "secondary" : "ghost"} role="radio" aria-checked={picks[i.id] === m} onClick={() => setPicks((p) => ({ ...p, [i.id]: m }))}>
                                  {m === "merge" ? "Fusionner" : "Remplacer"}
                                </Button>
                              ))}
                            </span>
                          )}
                        </div>
                        {isOpen && (
                          <div className="mt-1 grid gap-2 md:grid-cols-2">
                            {[
                              ["Source", i.from],
                              ["Cible", i.to],
                            ].map(([t, text]) => (
                              <div key={t}>
                                <p className="text-xs text-muted-foreground">{t}</p>
                                <pre className="max-h-56 overflow-auto rounded-md border bg-background p-2 font-mono text-xs whitespace-pre-wrap">{text || "(absent)"}</pre>
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
