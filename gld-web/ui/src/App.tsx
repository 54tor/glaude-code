import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { toast } from "sonner"
import {
  ChevronRight,
  FolderInput,
  Inbox,
  Layers,
  Monitor,
  Moon,
  MoreHorizontal,
  Search,
  Settings2,
  ShieldCheck,
  SquareTerminal,
  Sun,
  Trash2,
  TriangleAlert,
  X,
} from "lucide-react"
import { api, type Data, type Group, type Item, type Session } from "@/lib/api"
import { buildTree, find, under, type Entry } from "@/lib/tree"
import { size, tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { useLocal } from "@/hooks/useLocal"
import { useTheme } from "@/components/theme-provider"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { ActionsCtx, type Actions } from "@/components/actions"
import { Confirm, type Ask } from "@/components/Confirm"
import { Conversation } from "@/components/Conversation"
import { DirPicker, type Pick } from "@/components/DirPicker"
import { ProjectTree } from "@/components/ProjectTree"
import { SessionList } from "@/components/SessionList"

// Sessions lancées automatiquement (revues de sécurité) : leur titre est leur premier prompt. Elles sont masquées
// de l'arbre, comptées dans la barre latérale, et se suppriment d'un coup avec le bouton dédié.
const AUTO = ["Review this change for security vulnerabilities", "You previously flagged these candidate"]

const THEMES = [
  { v: "light", label: "Clair", Icon: Sun },
  { v: "dark", label: "Sombre", Icon: Moon },
  { v: "system", label: "Système", Icon: Monitor },
] as const

// Réglages rarement modifiés : options passées à claude et thème.
function Settings({ args, setArgs }: { args: string; setArgs: (v: string) => void }) {
  const { theme, setTheme } = useTheme()
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="sm" className="text-muted-foreground">
          <Settings2 /> Réglages
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" side="top" className="w-80 space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="args">Options ajoutées à claude</Label>
          <Input
            id="args"
            value={args}
            onChange={(e) => setArgs(e.target.value)}
            placeholder="ex. --dangerously-skip-permissions"
            className="font-mono text-xs"
            spellCheck={false}
          />
          <p className="text-xs text-muted-foreground">Utilisées à chaque reprise dans un terminal. Vide par défaut.</p>
        </div>
        <div className="space-y-1.5">
          <Label>Thème</Label>
          <div className="grid grid-cols-3 gap-1 rounded-lg bg-muted p-1">
            {THEMES.map(({ v, label, Icon }) => (
              <button
                key={v}
                onClick={() => setTheme(v)}
                aria-pressed={theme === v}
                className={cn(
                  "flex items-center justify-center gap-1.5 rounded-md py-1 text-xs transition-colors",
                  theme === v ? "bg-background font-medium shadow-sm" : "text-muted-foreground hover:text-foreground"
                )}
              >
                <Icon className="size-3.5" /> {label}
              </button>
            ))}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}

export default function App() {
  const [data, setData] = useState<Data | null>(null)
  const [q, setQ] = useState("")
  const [sel, setSel] = useState("") // clé du dossier affiché ; "" = toutes les sessions
  const [args, setArgs] = useLocal("ccArgs", "")
  const [reading, setReading] = useState<Session | null>(null)
  const [picker, setPicker] = useState<Pick | null>(null)
  const [ask, setAsk] = useState<Ask | null>(null)
  const search = useRef<HTMLInputElement>(null)

  const reload = useCallback(async () => setData(await api.data()), [])
  useEffect(() => {
    let live = true
    api.data().then((d) => live && setData(d))
    return () => {
      live = false
    }
  }, [])

  // « / » place le curseur dans la recherche, comme sur la plupart des outils.
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if (e.key === "/" && !/INPUT|TEXTAREA/.test(t.tagName) && !t.isContentEditable) {
        e.preventDefault()
        search.current?.focus()
      }
    }
    window.addEventListener("keydown", k)
    return () => window.removeEventListener("keydown", k)
  }, [])

  // Sessions affichées (hors revues automatiques, selon le filtre) et revues automatiques masquées.
  const view = useMemo(() => {
    const f = q.toLowerCase()
    const entries: Entry[] = []
    const autoItems: Item[] = []
    let akb = 0
    let total = 0
    for (const g of data?.groups ?? []) {
      const vis: Session[] = []
      for (const s of g.sessions) {
        if (AUTO.some((p) => s.title.includes(p))) {
          autoItems.push({ dir: s.dir, id: s.id })
          akb += s.kb
        } else vis.push(s)
      }
      total += vis.length
      const ss = vis.filter((s) => !f || g.project.toLowerCase().includes(f) || s.title.toLowerCase().includes(f))
      if (ss.length) entries.push({ g, ss })
    }
    return { entries, autoItems, akb, total }
  }, [data, q])

  const tree = useMemo(() => buildTree(view.entries, data?.home ?? ""), [view.entries, data?.home])
  // Un dossier qui disparaît (filtre, suppression) ramène à « toutes les sessions ».
  const node = sel ? find(tree, sel) : null
  const byDate = (a: { s: Session }, b: { s: Session }) => b.s.mtime - a.s.mtime
  const items = useMemo(
    () => (node ? node.ss.map((s) => ({ s, g: node.g! })) : view.entries.flatMap(({ g, ss }) => ss.map((s) => ({ s, g })))).sort(byDate),
    [node, view.entries]
  )
  // Sessions des sous-dossiers : affichées à part, sous la liste du dossier sélectionné.
  const subs = useMemo(() => (node ? [...node.kids.values()].flatMap(under).sort(byDate) : []), [node])
  const multi = new Set(items.map((i) => i.g.project)).size > 1

  // Une action : toast de progression puis de résultat, erreurs jamais muettes, liste rechargée.
  const act = async (url: string, body: unknown, busy: string, pick: "first" | "last" = "first") => {
    const id = toast.loading(busy)
    const r = await api.post(url, body)
    const lines = r.out
      .trim()
      .split("\n")
      .filter((l) => l.trim())
    const main = (pick === "last" ? lines[lines.length - 1] : lines[0]) ?? (r.ok ? "Terminé" : "Échec")
    const rest = lines.filter((l) => l !== main).join("\n")
    ;(r.ok ? toast.success : toast.error)(main, {
      id,
      description: rest || undefined,
    })
    await reload()
  }

  const pickDir = (title: string, start: string) =>
    new Promise<string | null>((resolve) =>
      setPicker({
        title,
        start,
        done: (p) => {
          setPicker(null)
          resolve(p)
        },
      })
    )

  const home = data?.home ?? ""
  const actions: Actions = {
    home,
    read: setReading,
    open: (b) => act("/open", { ...b, args }, "Ouverture du terminal…", "last"),
    copy: (s) =>
      navigator.clipboard.writeText(`claude --resume ${s.id}`).then(
        () =>
          toast.success("Commande copiée", {
            description: `claude --resume ${s.id}`,
          }),
        () => toast.error("Copie impossible")
      ),
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
      body: `Environ ${size(view.akb)} : sessions automatiques dont le titre contient « Review this change for security vulnerabilities » ou « You previously flagged these candidate ».\n\nLes sessions vivantes sont épargnées. Suppression définitive.`,
      confirm: `Supprimer ${n} sessions`,
      run: () => act("/rm", { items: view.autoItems }, `Suppression de ${n} sessions…`),
    })
  }

  const g = node?.g ?? null
  const own = g ? node!.ss.map((s) => ({ dir: s.dir, id: s.id })) : []

  return (
    <ActionsCtx.Provider value={actions}>
      <div className="flex h-dvh flex-col md:flex-row">
        {/* Barre latérale : recherche, dossiers de projet, réglages */}
        <aside className="flex max-h-[45dvh] shrink-0 flex-col border-b bg-sidebar md:max-h-none md:w-72 md:border-r md:border-b-0 lg:w-80">
          <div className="space-y-3 p-3">
            <div className="flex items-center gap-2 px-1 pt-1">
              <div className="grid size-7 place-items-center rounded-md bg-primary text-primary-foreground">
                <Layers className="size-4" />
              </div>
              <div className="leading-tight">
                <h1 className="text-sm font-semibold">Sessions Claude</h1>
                <p className="text-xs text-muted-foreground">{data ? `${view.total} sessions` : "Chargement…"}</p>
              </div>
            </div>
            <div className="relative">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                ref={search}
                value={q}
                onChange={(e) => setQ(e.target.value)}
                onKeyDown={(e) => e.key === "Escape" && setQ("")}
                placeholder="Rechercher…"
                className="bg-background pr-8 pl-8"
                aria-label="Rechercher par projet ou titre"
                autoFocus
              />
              {q ? (
                <button
                  className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  onClick={() => setQ("")}
                  aria-label="Effacer la recherche"
                >
                  <X className="size-4" />
                </button>
              ) : (
                <kbd className="pointer-events-none absolute top-1/2 right-2 -translate-y-1/2 rounded border bg-muted px-1.5 font-mono text-[10px] text-muted-foreground">
                  /
                </kbd>
              )}
            </div>
          </div>

          <ScrollArea className="min-h-0 flex-1">
            <nav className="px-2 pb-3" aria-label="Projets">
              <button
                onClick={() => setSel("")}
                className={cn(
                  "mb-1 flex h-8 w-full items-center gap-2 rounded-md px-2 text-sm transition-colors",
                  !node ? "bg-accent font-medium" : "text-foreground/80 hover:bg-muted hover:text-foreground"
                )}
              >
                <Inbox className="size-4 text-muted-foreground" />
                <span className="flex-1 text-left">Toutes les sessions</span>
                <span className="text-xs text-muted-foreground tabular-nums">{view.entries.reduce((a, e) => a + e.ss.length, 0)}</span>
              </button>
              {tree.length > 0 && <p className="mt-3 mb-1 px-2 text-xs font-medium text-muted-foreground">Projets</p>}
              {data === null ? (
                <div className="space-y-2 px-2">
                  <Skeleton className="h-6 w-full" />
                  <Skeleton className="h-6 w-4/5" />
                  <Skeleton className="h-6 w-3/5" />
                </div>
              ) : (
                <ProjectTree nodes={tree} sel={node ? sel : ""} onSel={setSel} />
              )}
            </nav>
          </ScrollArea>

          <div className="space-y-2 border-t p-3">
            {view.autoItems.length > 0 && (
              <div className="flex items-center gap-2 rounded-lg border bg-background px-3 py-2 text-xs">
                <ShieldCheck className="size-4 shrink-0 text-muted-foreground" />
                <span className="flex-1 text-muted-foreground">
                  {view.autoItems.length} revues de sécurité masquées · {size(view.akb)}
                </span>
                <Button variant="ghost" size="xs" className="text-destructive hover:text-destructive" onClick={purgeAuto}>
                  Nettoyer
                </Button>
              </div>
            )}
            <div className="flex items-center gap-2">
              <Settings args={args} setArgs={setArgs} />
              {args && (
                <code className="truncate text-xs text-muted-foreground" title="options ajoutées à claude">
                  {args}
                </code>
              )}
            </div>
          </div>
        </aside>

        {/* Contenu : sessions du dossier sélectionné */}
        <main className="flex min-h-0 min-w-0 flex-1 flex-col">
          <header className="flex flex-wrap items-center gap-3 border-b px-6 py-4">
            <div className="min-w-0 flex-1">
              <h2 className="truncate text-lg font-semibold tracking-tight">{node ? node.name : q ? "Résultats" : "Toutes les sessions"}</h2>
              <p className="truncate text-xs text-muted-foreground">
                {g ? tilde(g.project, home) : q ? `pour « ${q} »` : "Les plus récentes en premier"}
                {data && ` · ${items.length} session${items.length > 1 ? "s" : ""}`}
              </p>
            </div>
            {g && !g.missing && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => actions.open({ dir: node!.ss[0].dir })}
                title="Un onglet de terminal par session des 7 derniers jours"
              >
                <SquareTerminal /> Reprendre les récentes
              </Button>
            )}
            {g && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon-sm" aria-label="Actions du projet">
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-64">
                  <DropdownMenuItem onSelect={() => actions.moveGroup(g, own)}>
                    <FolderInput /> Réassigner les {own.length} sessions…
                  </DropdownMenuItem>
                  <DropdownMenuItem variant="destructive" onSelect={() => actions.remove(own, `les ${own.length} session(s) de ${g.project}`)}>
                    <Trash2 /> Supprimer les {own.length} sessions du projet
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </header>

          {g?.missing && (
            <div className="flex items-center gap-3 border-b bg-amber-500/10 px-6 py-3 text-sm">
              <TriangleAlert className="size-4 shrink-0 text-amber-600 dark:text-amber-400" />
              <p className="flex-1">
                Ce dossier n'existe plus : ses sessions ne peuvent pas être reprises tant qu'elles ne sont pas rattachées à un dossier existant.
              </p>
              <Button size="sm" variant="outline" onClick={() => actions.moveGroup(g, own)}>
                Réassigner…
              </Button>
            </div>
          )}

          <ScrollArea className="min-h-0 flex-1">
            <div className="mx-auto max-w-4xl px-3 py-4 md:px-6">
              {data === null && (
                <div className="space-y-3 px-3">
                  {[0, 1, 2, 3, 4].map((i) => (
                    <Skeleton key={i} className="h-12 w-full" />
                  ))}
                </div>
              )}
              {data && items.length > 0 && <SessionList items={items} showProject={multi} />}
              {data && subs.length > 0 && (
                <details open={items.length === 0} className="group/subs mt-8 border-t pt-4">
                  <summary className="flex cursor-pointer list-none items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground hover:text-foreground">
                    <ChevronRight className="size-4 transition-transform group-open/subs:rotate-90" />
                    Sessions des sous-dossiers
                    <span className="text-xs font-normal tabular-nums">{subs.length}</span>
                  </summary>
                  <div className="mt-2">
                    <SessionList items={subs} showProject />
                  </div>
                </details>
              )}
              {data && items.length === 0 && subs.length === 0 && (
                <div className="flex flex-col items-center gap-2 py-20 text-center">
                  <Search className="size-8 text-muted-foreground/50" />
                  <p className="font-medium">Aucune session</p>
                  <p className="text-sm text-muted-foreground">
                    {q ? "Aucun titre ni projet ne correspond à cette recherche." : "Rien à afficher ici."}
                  </p>
                  {q && (
                    <Button variant="outline" size="sm" className="mt-2" onClick={() => setQ("")}>
                      Effacer la recherche
                    </Button>
                  )}
                </div>
              )}
            </div>
          </ScrollArea>
        </main>
      </div>

      <Conversation session={reading} onClose={() => setReading(null)} />
      <DirPicker pick={picker} home={home} />
      <Confirm ask={ask} onClose={() => setAsk(null)} />
    </ActionsCtx.Provider>
  )
}
