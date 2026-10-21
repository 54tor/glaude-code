import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { api, type Data, type Group, type Item, type Session } from "@/lib/api"
import { buildTree, type Entry } from "@/lib/tree"
import { useLocal } from "@/hooks/useLocal"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ActionsCtx, type Actions } from "@/components/actions"
import { Confirm, type Ask } from "@/components/Confirm"
import { Conversation } from "@/components/Conversation"
import { DirPicker, type Pick } from "@/components/DirPicker"
import { NodeView } from "@/components/NodeView"

// Sessions lancées automatiquement (revues de sécurité) : leur titre est leur premier prompt. Elles sont masquées
// de l'arbre, comptées en tête de page, et se suppriment d'un coup avec le bouton dédié.
const AUTO = ["Review this change for security vulnerabilities", "You previously flagged these candidate"]

export default function App() {
  const [data, setData] = useState<Data | null>(null)
  const [q, setQ] = useState("")
  const [args, setArgs] = useLocal("ccArgs", "")
  const [opened, setOpened] = useState(() => new Set(["~"]))
  const [reading, setReading] = useState<Session | null>(null)
  const [picker, setPicker] = useState<Pick | null>(null)
  const [ask, setAsk] = useState<Ask | null>(null)

  const reload = useCallback(async () => setData(await api.data()), [])
  useEffect(() => {
    let live = true
    api.data().then((d) => live && setData(d))
    return () => {
      live = false
    }
  }, [])

  // Sessions affichées (hors revues automatiques, selon le filtre) et revues automatiques masquées.
  const view = useMemo(() => {
    const f = q.toLowerCase()
    const entries: Entry[] = []
    const autoItems: Item[] = []
    let ns = 0
    let akb = 0
    for (const g of data?.groups ?? []) {
      const vis: Session[] = []
      for (const s of g.sessions) {
        if (AUTO.some((p) => s.title.includes(p))) {
          autoItems.push({ dir: s.dir, id: s.id })
          akb += s.kb
        } else vis.push(s)
      }
      const ss = vis.filter((s) => !f || g.project.toLowerCase().includes(f) || s.title.toLowerCase().includes(f))
      if (ss.length) {
        entries.push({ g, ss })
        ns += ss.length
      }
    }
    return { entries, autoItems, akb, ns, np: entries.length }
  }, [data, q])

  const tree = useMemo(() => buildTree(view.entries, data?.home ?? ""), [view.entries, data?.home])

  // Une action : toast de progression puis de résultat, erreurs jamais muettes, liste rechargée.
  const act = async (url: string, body: unknown, busy: string, pick: "first" | "last" = "first") => {
    const id = toast.loading(busy)
    const r = await api.post(url, body)
    const lines = r.out.trim().split("\n").filter((l) => l.trim())
    const main = (pick === "last" ? lines[lines.length - 1] : lines[0]) ?? (r.ok ? "Terminé" : "Échec")
    const rest = lines.filter((l) => l !== main).join("\n")
    ;(r.ok ? toast.success : toast.error)(main, { id, description: rest || undefined })
    await reload()
  }

  const pickDir = (title: string, start: string) =>
    new Promise<string | null>((resolve) =>
      setPicker({ title, start, done: (p) => { setPicker(null); resolve(p) } }),
    )

  const actions: Actions = {
    filtering: q !== "",
    opened,
    toggle: (k) =>
      setOpened((o) => {
        const n = new Set(o)
        if (n.has(k)) n.delete(k)
        else n.add(k)
        return n
      }),
    read: setReading,
    open: (b) => act("/open", { ...b, args }, "Ouverture du terminal…", "last"),
    move: async (s: Session) => {
      const to = await pickDir(`Déplacer « ${s.title} » vers…`, s.cwd)
      if (to && to !== s.cwd) act("/reassign", { items: [{ dir: s.dir, id: s.id }], to }, "Déplacement…")
    },
    moveGroup: async (g: Group, items: Item[]) => {
      const to = await pickDir(`Réassigner ${items.length} session(s) vers…`, g.project)
      if (to) act("/reassign", { items, to }, "Réassignation…")
    },
    remove: (items, what, after) =>
      setAsk({
        title: "Supprimer définitivement ?",
        body: `${what}\n\nSuppression définitive : transcript, sous-agents et fichiers associés.`,
        confirm: "Supprimer",
        run: () => {
          after?.()
          act("/rm", { items }, "Suppression en cours…")
        },
      }),
  }

  const purgeAuto = () => {
    const n = view.autoItems.length
    setAsk({
      title: `Supprimer les ${n} revues de sécurité ?`,
      body: `Environ ${Math.round(view.akb / 1024)} Mo : sessions automatiques dont le titre contient « Review this change for security vulnerabilities » ou « You previously flagged these candidate ».\n\nLes sessions vivantes sont épargnées. Suppression définitive.`,
      confirm: `Supprimer ${n} sessions`,
      run: () => act("/rm", { items: view.autoItems }, `Suppression de ${n} sessions…`),
    })
  }

  return (
    <ActionsCtx.Provider value={actions}>
      <main className="mx-auto max-w-4xl px-4 py-8">
        <header className="mb-6 flex items-end justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">Sessions Claude</h1>
            <p className="text-sm text-muted-foreground">
              {data ? (
                `${view.ns} session(s) dans ${view.np} projet(s)${view.autoItems.length ? ` · ${view.autoItems.length} revue(s) de sécurité masquée(s)` : ""}`
              ) : (
                <Skeleton className="mt-1 h-4 w-56" />
              )}
            </p>
          </div>
          {view.autoItems.length > 0 && (
            <Button variant="destructive" onClick={purgeAuto}>
              Supprimer les {view.autoItems.length} revues de sécurité
            </Button>
          )}
        </header>

        <div className="mb-4 space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="args" className="text-xs text-muted-foreground">
              Options ajoutées à <code>claude</code> lors d'une réouverture (vide par défaut)
            </Label>
            <Input id="args" value={args} onChange={(e) => setArgs(e.target.value)} placeholder="ex. --dangerously-skip-permissions" className="font-mono text-xs" />
          </div>
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filtrer par projet ou titre…" autoFocus />
        </div>

        <div className="space-y-3">
          {data === null && (
            <>
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
            </>
          )}
          {tree.map((n) => (
            <NodeView key={n.key} n={n} />
          ))}
          {data && tree.length === 0 && <p className="text-sm text-muted-foreground">Aucune session à afficher.</p>}
        </div>
      </main>

      <Conversation
        session={reading}
        onClose={() => setReading(null)}
        onDelete={(s) => actions.remove([{ dir: s.dir, id: s.id }], `« ${s.title} »`, () => setReading(null))}
      />
      <DirPicker pick={picker} home={data?.home ?? ""} />
      <Confirm ask={ask} onClose={() => setAsk(null)} />
    </ActionsCtx.Provider>
  )
}
