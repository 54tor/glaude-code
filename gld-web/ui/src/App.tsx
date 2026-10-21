import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { api, type Data, type Group, type Item, type Session } from "@/lib/api"
import { buildTree, type Entry } from "@/lib/tree"
import { useLocal } from "@/hooks/useLocal"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { ActionsCtx, type Actions } from "@/components/actions"
import { Confirm, type Ask } from "@/components/Confirm"
import { Conversation } from "@/components/Conversation"
import { DirPicker, type Pick } from "@/components/DirPicker"
import { NodeView } from "@/components/NodeView"

// Par défaut : les revues de sécurité automatiques (leur titre est leur premier prompt).
const HIDE_DEFAULT = "Review this change for security vulnerabilities|You previously flagged these candidate"

export default function App() {
  const [data, setData] = useState<Data | null>(null)
  const [q, setQ] = useState("")
  const [hide, setHide] = useLocal("ccHide", HIDE_DEFAULT)
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

  // Sessions visibles (ni masquées par le motif, ni écartées par le filtre) et sessions masquées.
  const view = useMemo(() => {
    const pats = hide.toLowerCase().split("|").map((s) => s.trim()).filter(Boolean)
    const f = q.toLowerCase()
    const entries: Entry[] = []
    const hiddenItems: Item[] = []
    let ns = 0
    let hkb = 0
    for (const g of data?.groups ?? []) {
      const vis: Session[] = []
      for (const s of g.sessions) {
        if (pats.some((p) => s.title.toLowerCase().includes(p))) {
          hiddenItems.push({ dir: s.dir, id: s.id })
          hkb += s.kb
        } else vis.push(s)
      }
      const ss = vis.filter((s) => !f || g.project.toLowerCase().includes(f) || s.title.toLowerCase().includes(f))
      if (ss.length) {
        entries.push({ g, ss })
        ns += ss.length
      }
    }
    return { entries, hiddenItems, hkb, ns, np: entries.length }
  }, [data, hide, q])

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
    remove: (items, what) =>
      setAsk({
        title: "Supprimer définitivement ?",
        body: `${what}\n\nSuppression définitive : transcript, sous-agents et fichiers associés.`,
        confirm: "Supprimer",
        run: () => act("/rm", { items }, "Suppression en cours…"),
      }),
  }

  const purge = () => {
    const n = view.hiddenItems.length
    setAsk({
      title: `Supprimer les ${n} sessions masquées ?`,
      body: `Environ ${Math.round(view.hkb / 1024)} Mo, dont le titre contient :\n${hide}\n\nLes sessions vivantes sont épargnées. Suppression définitive.`,
      confirm: `Supprimer ${n} sessions`,
      count: n,
      run: () => act("/rm", { items: view.hiddenItems }, `Suppression de ${n} sessions…`),
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
                <>
                  {view.ns} session(s) dans {view.np} projet(s)
                  {view.hiddenItems.length > 0 && ` · ${view.hiddenItems.length} masquée(s)`}
                </>
              ) : (
                <Skeleton className="mt-1 h-4 w-56" />
              )}
            </p>
          </div>
          {view.hiddenItems.length > 0 && (
            <Button variant="destructive" onClick={purge}>
              Supprimer les {view.hiddenItems.length} masquées
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
          <Input
            value={hide}
            onChange={(e) => setHide(e.target.value)}
            aria-label="Masquer les sessions dont le titre contient"
            placeholder="Masquer : motif|motif…"
            className="font-mono text-xs"
          />
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

      <Conversation session={reading} onClose={() => setReading(null)} />
      <DirPicker pick={picker} home={data?.home ?? ""} />
      <Confirm ask={ask} onClose={() => setAsk(null)} />
    </ActionsCtx.Provider>
  )
}
