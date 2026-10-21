import { AlarmClock, Star, Brain, SlidersHorizontal, Zap, Flame, History, Clock, FileText, Inbox, Loader2, Search, ShieldCheck, Tag, X } from "lucide-react"
import { size } from "@/lib/format"
import type { Choice } from "@/lib/models"
import type { Route } from "@/lib/route"
import type { Node } from "@/lib/tree"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { ProjectTree } from "./ProjectTree"
import { CM, GroupItems, SessionItems } from "./SessionMenu"
import type { Session, VGroup } from "@/lib/api"
import { useActions, type Row } from "./actions"
import { Settings, type Fonts } from "./SettingsDialog"


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

// Sessions actives depuis moins d'une heure : leur cache est chaud, les reprendre coûte peu.
// En orange, celles dont le cache expire dans moins de 10 minutes.
function Hot({ rows, now, ttl }: { rows: Row[]; now: number; ttl: number }) {
  const a = useActions()
  return (
    <>
      <Heading>
        <span title="Le cache des prompts dure une heure : reprendre une de ces sessions maintenant est plus rapide et moins coûteux">
          Cache chaud
        </span>
      </Heading>
      {rows.slice(0, 5).map(({ s }) => {
        const left = Math.max(1, Math.ceil((ttl - (now - s.mtime)) / 60))
        const warn = left <= 10
        return (
          <button
            key={s.id}
            onClick={() => a.read(s)}
            title={`${s.title}\nCache encore environ ${left} min`}
            className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-sm text-foreground/80 transition-colors hover:bg-muted hover:text-foreground"
          >
            {warn ? (
              <AlarmClock className="size-4 shrink-0 text-amber-500" aria-label="cache bientôt expiré" />
            ) : (
              <Flame className="size-4 shrink-0 text-orange-500" aria-hidden />
            )}
            {a.live.has(s.id) && <span className="size-1.5 shrink-0 rounded-full bg-emerald-500" aria-label="ouverte" />}
            <span className="flex-1 truncate text-left">{s.title}</span>
            <span className={cn("shrink-0 text-xs tabular-nums", warn ? "font-medium text-amber-600 dark:text-amber-400" : "text-muted-foreground")}>
              {left} min
            </span>
          </button>
        )
      })}
    </>
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
  counts: { recent: number; all: number; active: number }
  favRows: Row[] // sessions favorites
  favGroups: { vg: VGroup; key: string }[] // groupes favoris, avec la clé de leur dossier
  groupCount: (vg: VGroup) => number
  read: (s: Session) => void
  tree: Node[]
  hot: Row[] // sessions dont le cache des prompts est encore chaud
  now: number
  ttl: number // durée du cache, en secondes
  snippets: number | null // null : bibliothèque non configurée
  auto: { n: number; kb: number; purge: () => void }
  args: string
  setArgs: (v: string) => void
  choice: Choice
  setChoice: (c: Choice) => void
} & Fonts

export function Sidebar({ searchRef, ...p }: SidebarProps) {
  const r = p.route
  return (
    <aside className="flex max-h-[45dvh] shrink-0 flex-col border-b bg-sidebar md:max-h-none md:w-72 md:border-r md:border-b-0 lg:w-80">
      <div className="space-y-3 p-3">
        <div className="flex items-center gap-2 px-1 pt-1">
          <img src="/icons/icon-192.png" alt="" className="size-8 shrink-0" />
          <div className="leading-tight">
            <h1 className="text-sm font-semibold">Glaude Code</h1>
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
          <NavItem icon={Flame} label="En cours et cache chaud" n={p.counts.active} active={r.kind === "active"} onClick={() => p.go({ kind: "active" })} />
          <NavItem icon={Inbox} label="Toutes les sessions" n={p.counts.all} active={r.kind === "all"} onClick={() => p.go({ kind: "all" })} />
          <NavItem icon={History} label="Sessions sauvegardées" active={r.kind === "tabs"} onClick={() => p.go({ kind: "tabs" })} />
          <NavItem icon={Zap} label="Skills" active={r.kind === "skills"} onClick={() => p.go({ kind: "skills", key: "" })} />
          <NavItem icon={SlidersHorizontal} label="Configuration" active={r.kind === "config"} onClick={() => p.go({ kind: "config", tab: "" })} />
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

          {p.hot.length > 0 && <Hot rows={p.hot} now={p.now} ttl={p.ttl} />}

          {(p.favRows.length > 0 || p.favGroups.length > 0) && (
            <>
              <Heading>Favoris</Heading>
              {p.favGroups.map(({ vg, key }) => (
                <ContextMenu key={key + "\0" + vg.name}>
                  <ContextMenuTrigger asChild>
                    <div>
                      <NavItem
                        icon={Tag}
                        label={vg.name}
                        n={p.groupCount(vg)}
                        active={r.kind === "group" && r.name === vg.name && r.key === key}
                        onClick={() => p.go({ kind: "group", name: vg.name, key })}
                      />
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent className="w-72">
                    <GroupItems M={CM} vg={vg} />
                  </ContextMenuContent>
                </ContextMenu>
              ))}
              {p.favRows.map((row) => (
                <ContextMenu key={row.s.id}>
                  <ContextMenuTrigger asChild>
                    <div>
                      <NavItem icon={Star} label={row.s.title} active={false} onClick={() => p.read(row.s)} />
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent className="w-64">
                    <SessionItems M={CM} row={row} full />
                  </ContextMenuContent>
                </ContextMenu>
              ))}
            </>
          )}

          {(p.loading || p.tree.length > 0) && <Heading>Projets</Heading>}
          {p.loading ? (
            <div className="space-y-2 px-2">
              <Skeleton className="h-6 w-full" />
              <Skeleton className="h-6 w-4/5" />
              <Skeleton className="h-6 w-3/5" />
            </div>
          ) : (
            <ProjectTree
              nodes={p.tree}
              sel={r.kind === "dir" ? r.key : ""}
              onSel={(key) => p.go({ kind: "dir", key })}
              selGroup={r.kind === "group" ? r.key + "\0" + r.name : ""}
              onGroup={(vg, key) => p.go({ kind: "group", name: vg.name, key })}
            />
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
          <Settings
            args={p.args}
            setArgs={p.setArgs}
            choice={p.choice}
            setChoice={p.setChoice}
            font={p.font}
            setFont={p.setFont}
            fontSize={p.fontSize}
            setFontSize={p.setFontSize}
          />
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
