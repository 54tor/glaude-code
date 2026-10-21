import {
  Brain,
  History,
  Clock,
  FileText,
  Inbox,
  Layers,
  Loader2,
  Monitor,
  Moon,
  Pencil,
  Plus,
  Search,
  Settings2,
  ShieldCheck,
  Sun,
  Tag,
  Trash2,
  X,
} from "lucide-react"
import { size } from "@/lib/format"
import type { Route } from "@/lib/route"
import type { Node } from "@/lib/tree"
import { cn } from "@/lib/utils"
import { useTheme } from "@/components/theme-provider"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { ProjectTree } from "./ProjectTree"

const THEMES = [
  { v: "light", label: "Clair", Icon: Sun },
  { v: "dark", label: "Sombre", Icon: Moon },
  { v: "system", label: "Système", Icon: Monitor },
] as const

const KEYS: [string, string][] = [
  ["/", "rechercher"],
  ["j / k", "session suivante / précédente"],
  ["Entrée", "lire"],
  ["t", "reprendre dans un terminal"],
  ["r", "renommer"],
  ["x", "sélectionner"],
  ["e", "exporter en Markdown"],
  ["Suppr", "supprimer"],
  ["Échap", "vider la sélection"],
]

// Réglages rarement modifiés : options passées à claude, thème, raccourcis.
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
        <div className="space-y-1.5">
          <Label>Raccourcis clavier</Label>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
            {KEYS.map(([k, v]) => (
              <div key={k} className="contents">
                <dt>
                  <kbd className="rounded border bg-muted px-1.5 font-mono text-[10px]">{k}</kbd>
                </dt>
                <dd className="text-muted-foreground">{v}</dd>
              </div>
            ))}
          </dl>
        </div>
      </PopoverContent>
    </Popover>
  )
}

function NavItem({ icon: Icon, label, n, active, onClick }: { icon: typeof Inbox; label: string; n?: number; active: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      aria-current={active}
      className={cn(
        "flex h-8 w-full items-center gap-2 rounded-md px-2 text-sm transition-colors",
        active ? "bg-accent font-medium" : "text-foreground/80 hover:bg-muted hover:text-foreground"
      )}
    >
      <Icon className="size-4 shrink-0 text-muted-foreground" />
      <span className="flex-1 truncate text-left">{label}</span>
      {n !== undefined && <span className="text-xs text-muted-foreground tabular-nums">{n}</span>}
    </button>
  )
}

function Heading({ children, action }: { children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="mt-4 mb-1 flex items-center px-2">
      <p className="flex-1 text-xs font-medium text-muted-foreground">{children}</p>
      {action}
    </div>
  )
}

export type SidebarProps = {
  loading: boolean
  total: number
  q: string
  setQ: (v: string) => void
  searchRef: React.RefObject<HTMLInputElement | null>
  searching: boolean
  route: Route
  go: (r: Route) => void
  counts: { recent: number; all: number }
  groups: { name: string; n: number }[]
  newGroup: () => void
  renameGroup: (name: string) => void
  deleteGroup: (name: string) => void
  tree: Node[]
  snippets: number | null // null : bibliothèque non configurée
  auto: { n: number; kb: number; purge: () => void }
  args: string
  setArgs: (v: string) => void
}

export function Sidebar({ searchRef, ...p }: SidebarProps) {
  const r = p.route
  return (
    <aside className="flex max-h-[45dvh] shrink-0 flex-col border-b bg-sidebar md:max-h-none md:w-72 md:border-r md:border-b-0 lg:w-80">
      <div className="space-y-3 p-3">
        <div className="flex items-center gap-2 px-1 pt-1">
          <div className="grid size-7 place-items-center rounded-md bg-primary text-primary-foreground">
            <Layers className="size-4" />
          </div>
          <div className="leading-tight">
            <h1 className="text-sm font-semibold">Sessions Claude</h1>
            <p className="text-xs text-muted-foreground">{p.loading ? "Chargement…" : `${p.total} sessions`}</p>
          </div>
        </div>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={searchRef}
            value={p.q}
            onChange={(e) => p.setQ(e.target.value)}
            onKeyDown={(e) => e.key === "Escape" && p.setQ("")}
            placeholder="Rechercher…"
            className="bg-background pr-8 pl-8"
            aria-label="Rechercher dans les titres, les projets et les conversations"
            autoFocus
          />
          {p.searching ? (
            <Loader2
              className="absolute top-1/2 right-2.5 size-4 -translate-y-1/2 animate-spin text-muted-foreground"
              aria-label="Recherche en cours"
            />
          ) : p.q ? (
            <button
              className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => p.setQ("")}
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
        <nav className="px-2 pb-3" aria-label="Navigation">
          <NavItem
            icon={Clock}
            label="Récentes (7 jours)"
            n={p.counts.recent}
            active={r.kind === "recent"}
            onClick={() => p.go({ kind: "recent" })}
          />
          <NavItem icon={Inbox} label="Toutes les sessions" n={p.counts.all} active={r.kind === "all"} onClick={() => p.go({ kind: "all" })} />
          <NavItem icon={History} label="Sessions sauvegardées" active={r.kind === "tabs"} onClick={() => p.go({ kind: "tabs" })} />
          <NavItem icon={Brain} label="Mémoire" active={r.kind === "memory"} onClick={() => p.go({ kind: "memory", key: "" })} />
          {p.snippets !== null && (
            <NavItem
              icon={FileText}
              label="Snippets"
              n={p.snippets}
              active={r.kind === "snippets"}
              onClick={() => p.go({ kind: "snippets", path: "" })}
            />
          )}

          <Heading
            action={
              <Button variant="ghost" size="icon-xs" onClick={p.newGroup} aria-label="Nouveau groupe" title="Nouveau groupe">
                <Plus />
              </Button>
            }
          >
            Groupes
          </Heading>
          {p.groups.length === 0 && <p className="px-2 py-1 text-xs text-muted-foreground">Clic droit sur une session › Groupe, pour en créer un.</p>}
          {p.groups.map((g) => (
            <ContextMenu key={g.name}>
              <ContextMenuTrigger asChild>
                <div>
                  <NavItem
                    icon={Tag}
                    label={g.name}
                    n={g.n}
                    active={r.kind === "group" && r.name === g.name}
                    onClick={() => p.go({ kind: "group", name: g.name })}
                  />
                </div>
              </ContextMenuTrigger>
              <ContextMenuContent className="w-52">
                <ContextMenuItem onSelect={() => p.renameGroup(g.name)}>
                  <Pencil /> Renommer…
                </ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem variant="destructive" onSelect={() => p.deleteGroup(g.name)}>
                  <Trash2 /> Supprimer le groupe
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
          ))}

          {(p.loading || p.tree.length > 0) && <Heading>Projets</Heading>}
          {p.loading ? (
            <div className="space-y-2 px-2">
              <Skeleton className="h-6 w-full" />
              <Skeleton className="h-6 w-4/5" />
              <Skeleton className="h-6 w-3/5" />
            </div>
          ) : (
            <ProjectTree nodes={p.tree} sel={r.kind === "dir" ? r.key : ""} onSel={(key) => p.go({ kind: "dir", key })} />
          )}
        </nav>
      </ScrollArea>

      <div className="space-y-2 border-t p-3">
        {p.auto.n > 0 && (
          <div className="flex items-center gap-2 rounded-lg border bg-background px-3 py-2 text-xs">
            <ShieldCheck className="size-4 shrink-0 text-muted-foreground" />
            <span className="flex-1 text-muted-foreground">
              {p.auto.n} revues de sécurité masquées · {size(p.auto.kb)}
            </span>
            <Button variant="ghost" size="xs" className="text-destructive hover:text-destructive" onClick={p.auto.purge}>
              Nettoyer
            </Button>
          </div>
        )}
        <div className="flex items-center gap-2">
          <Settings args={p.args} setArgs={p.setArgs} />
          {p.args && (
            <code className="truncate text-xs text-muted-foreground" title="options ajoutées à claude">
              {p.args}
            </code>
          )}
        </div>
      </div>
    </aside>
  )
}
