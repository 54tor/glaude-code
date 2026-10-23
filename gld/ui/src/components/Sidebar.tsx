import { useEffect, useState } from "react"
import { SessionTrash } from "./SessionTrash"
import { CleanupWarning } from "./CleanupWarning"
import { Trash2 } from "lucide-react"
import { AlarmClock, ChevronRight, FolderPlus, Repeat, Coins, FolderOpen, Star, Brain, SlidersHorizontal, Zap, Flame, History, Clock, FileText, Inbox, Loader2, Search, ShieldCheck, Tag, X } from "lucide-react"
import { size, tilde } from "@/lib/format"
import type { Choice } from "@/lib/models"
import { href, type Route } from "@/lib/route"
import type { Node } from "@/lib/tree"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { useKeepWarm } from "@/hooks/useKeepWarm"
import { HomeSwitcher } from "./HomeSwitcher"
import { ProjectList } from "./ProjectList"
import { ProjectItems, ProjectTree } from "./ProjectTree"
import { segs } from "@/lib/paths"
import { CM, GroupItems, SessionItems } from "./SessionMenu"
import type { Session, VGroup } from "@/lib/api"
import { useActions, type Row } from "./actions"
import { Settings, type Fonts } from "./SettingsDialog"


// Avec `to`, un vrai lien : clic du milieu ou Ctrl+clic ouvrent la vue dans un autre onglet.
function NavItem({ icon: Icon, label, n, active, onClick, to }: { icon: typeof Inbox; label: string; n?: number; active: boolean; onClick: () => void; to?: Route }) {
  const cls = cn(
    "flex h-8 w-full items-center gap-2.5 rounded-md px-2 text-sm transition-colors",
    active ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground [&>svg]:text-primary" : "text-sidebar-foreground/85 hover:bg-sidebar-accent/60 hover:text-sidebar-foreground"
  )
  const body = (
    <>
      <Icon className="size-4 shrink-0 text-muted-foreground" />
      <span className="flex-1 truncate text-left">{label}</span>
      {n !== undefined && <span className="text-xs text-muted-foreground tabular-nums">{n}</span>}
    </>
  )
  if (to)
    return (
      <a
        href={href(to)}
        aria-current={active ? "page" : undefined}
        className={cls}
        onClick={(e) => {
          if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
          e.preventDefault()
          onClick()
        }}
      >
        {body}
      </a>
    )
  return (
    <button onClick={onClick} aria-current={active} className={cls}>
      {body}
    </button>
  )
}

function Heading({ children, action }: { children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="mt-5 mb-1 flex items-center px-2">
      <p className="flex-1 text-xs font-semibold text-muted-foreground">{children}</p>
      {action}
    </div>
  )
}

// Sessions actives depuis moins d'une heure : leur cache est chaud, les reprendre coûte peu.
// En orange, celles dont le cache expire dans moins de 10 minutes.
function Hot({ rows, now, ttl, go }: { rows: Row[]; now: number; ttl: number; go: (r: Route) => void }) {
  const a = useActions()
  const warm = useKeepWarm()
  // Un dossier par projet de travail, le plus récemment actif d'abord (les lignes arrivent déjà triées par date).
  const folders = new Map<string, Row[]>()
  for (const r of rows) folders.set(r.g.project, [...(folders.get(r.g.project) ?? []), r])
  const item = (row: Row) => {
    const { s } = row
    const left = Math.max(1, Math.ceil((ttl - (now - s.mtime)) / 60))
    const warn = left <= 10
    return (
      <ContextMenu key={s.id}>
        <ContextMenuTrigger asChild>
          <button
            onClick={() => a.read(s)}
            title={`${s.title}\nCache encore environ ${left} min`}
            className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-sm text-sidebar-foreground/85 transition-colors hover:bg-sidebar-accent/60 hover:text-sidebar-foreground"
          >
            {warn ? (
              <AlarmClock className="size-4 shrink-0 text-amber-500" aria-label="cache bientôt expiré" />
            ) : (
              <Flame className="size-4 shrink-0 text-orange-500" aria-hidden />
            )}
            {warm[s.id]?.on && <Coins className="size-3.5 shrink-0 text-amber-500" aria-label="maintien au chaud actif (payant)" />}
            {a.live.has(s.id) && <span className="size-1.5 shrink-0 rounded-full bg-emerald-500" aria-label="ouverte" />}
            <span className="flex-1 truncate text-left">{s.title}</span>
            <span className={cn("shrink-0 text-xs tabular-nums", warn ? "font-medium text-amber-600 dark:text-amber-400" : "text-muted-foreground")}>
              {left} min
            </span>
          </button>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-64">
          <SessionItems M={CM} row={row} full />
        </ContextMenuContent>
      </ContextMenu>
    )
  }
  return (
    <>
      <Heading>
        <span title="Le cache des prompts dure une heure : reprendre une de ces sessions maintenant est plus rapide et moins coûteux">
          Cache chaud
        </span>
      </Heading>
      {[...folders].map(([project, rs]) => (
        <details key={project} open className="group/hot">
          <ContextMenu>
            <ContextMenuTrigger asChild>
              <summary
                title={tilde(project, a.home)}
                className="flex h-7 cursor-pointer list-none items-center gap-1.5 rounded-md px-2 text-xs font-medium text-muted-foreground hover:bg-muted [&::-webkit-details-marker]:hidden"
              >
                <ChevronRight className="size-3.5 shrink-0 transition-transform group-open/hot:rotate-90" aria-hidden />
                <span className="flex-1 truncate">{project.split("/").filter(Boolean).pop() ?? project}</span>
                <span className="tabular-nums">{rs.length}</span>
              </summary>
            </ContextMenuTrigger>
            <ProjectItems
              g={rs[0].g}
              ss={rs.map((r) => r.s)}
              extra={
                <ContextMenuItem onSelect={() => go({ kind: "dir", key: segs(project, a.home).join("/") })}>
                  <FolderOpen /> Afficher le dossier
                </ContextMenuItem>
              }
            />
          </ContextMenu>
          <div className="ml-3 border-l pl-1">{rs.map(item)}</div>
        </details>
      ))}
    </>
  )
}

export type SidebarProps = {
  open: boolean // mobile : le tiroir est ouvert
  onClose: () => void
  loading: boolean
  total: number
  q: string
  setQ: (v: string) => void
  searchRef: React.RefObject<HTMLInputElement | null>
  searching: boolean
  route: Route
  go: (r: Route) => void
  counts: { recent: number; all: number; active: number }
  reload: () => Promise<void>
  carousel: { n: number; on: boolean; start: () => void } // sessions en attente de réponse, parcourues l'une après l'autre
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
  const link = (to: Route) => ({ to, onClick: () => p.go(to) })
  const a = useActions()
  const [trashOpen, setTrashOpen] = useState(false)
  const { open, onClose } = p
  useEffect(() => {
    if (!open) return
    const on = (e: KeyboardEvent) => e.key === "Escape" && onClose()
    window.addEventListener("keydown", on)
    return () => window.removeEventListener("keydown", on)
  }, [open, onClose])
  return (
    <>
      <SessionTrash open={trashOpen} onClose={() => setTrashOpen(false)} reload={p.reload} />
      {/* Mobile : le menu est un tiroir par-dessus la page ; sur grand écran il reste une colonne fixe. */}
      <div className={cn("fixed inset-0 z-30 bg-black/40 transition-opacity md:hidden", p.open ? "opacity-100" : "pointer-events-none opacity-0")} onClick={p.onClose} aria-hidden="true" />
      <aside
        id="menu"
        aria-label="Menu"
        className={cn(
          "fixed inset-y-0 left-0 z-40 flex w-[85vw] max-w-sm shrink-0 flex-col border-r bg-sidebar transition-transform duration-200 md:static md:z-auto md:w-72 md:translate-x-0 md:transition-none lg:w-80",
          p.open ? "translate-x-0 shadow-xl" : "invisible -translate-x-full md:visible"
        )}
      >
      <div className="space-y-3 p-3">
        <div className="flex items-center gap-2.5 px-1 pt-1">
          <img src="/icons/icon-192.png" alt="" className="size-10 shrink-0" />
          <div className="leading-tight">
            <h1 className="text-[15px] font-semibold tracking-tight">Glaude Code</h1>
            <p className="text-xs text-muted-foreground tabular-nums">{p.loading ? "Chargement…" : `${p.total} sessions`}</p>
          </div>
          <div className="ml-auto flex items-center gap-0.5">
            <HomeSwitcher />
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
            className="bg-background pr-8 pl-8 shadow-none"
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
        <CleanupWarning />
        <nav className="px-2 pb-3" aria-label="Navigation">
          {(p.carousel.n > 0 || p.carousel.on) && <NavItem icon={Repeat} label="Carrousel des sessions en attente" n={p.carousel.n} active={p.carousel.on} onClick={p.carousel.start} />}
          <NavItem
            icon={Clock}
            label="Récentes (7 jours)"
            n={p.counts.recent}
            active={r.kind === "recent"}
            {...link({ kind: "recent" })}
          />
          <NavItem icon={Flame} label="En cours et cache chaud" n={p.counts.active} active={r.kind === "active"} {...link({ kind: "active" })} />
          <NavItem icon={Inbox} label="Toutes les sessions" n={p.counts.all} active={r.kind === "all"} {...link({ kind: "all" })} />
          <NavItem icon={Trash2} label="Corbeille des sessions" active={false} onClick={() => setTrashOpen(true)} />
          <NavItem icon={History} label="Sessions sauvegardées" active={r.kind === "tabs"} {...link({ kind: "tabs" })} />
          <NavItem icon={Zap} label="Skills" active={r.kind === "skills"} {...link({ kind: "skills", key: "" })} />
          <NavItem icon={SlidersHorizontal} label="Configuration" active={r.kind === "config"} {...link({ kind: "config", tab: "" })} />
          <NavItem icon={Brain} label="Mémoire" active={r.kind === "memory"} {...link({ kind: "memory", key: "" })} />
          {p.snippets !== null && (
            <NavItem
              icon={FileText}
              label="Snippets"
              n={p.snippets}
              active={r.kind === "snippets"}
              {...link({ kind: "snippets", path: "" })}
            />
          )}

          {p.hot.length > 0 && <Hot rows={p.hot} now={p.now} ttl={p.ttl} go={p.go} />}

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
                        {...link({ kind: "group", name: vg.name, key })}
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

          {a.projects.length > 0 && (
            <>
              <Heading>Projets</Heading>
              <ProjectList route={r} go={p.go} />
            </>
          )}

          <Heading
            action={
              <Button variant="ghost" size="icon-xs" onClick={a.addDir} title="Ajouter un dossier sans session : on y lance un prompt sans mémoire Claude" aria-label="Ajouter un dossier">
                <FolderPlus />
              </Button>
            }
          >
            Sessions locales
          </Heading>
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

      {(p.auto.n > 0 || p.args) && (
      <div className="space-y-2 border-t p-3">
        {p.auto.n > 0 && (
          <div className="flex items-center gap-2 px-1 text-xs">
            <ShieldCheck className="size-4 shrink-0 text-muted-foreground" />
            <span className="flex-1 text-muted-foreground">
              {p.auto.n} revues de sécurité masquées ({size(p.auto.kb)})
            </span>
            <Button variant="ghost" size="xs" className="text-destructive hover:text-destructive" onClick={p.auto.purge}>
              Nettoyer
            </Button>
          </div>
        )}
        {p.args && (
          <code className="block truncate px-1 text-xs text-muted-foreground" title="options ajoutées à claude">
            {p.args}
          </code>
        )}
      </div>
      )}
      </aside>
    </>
  )
}
