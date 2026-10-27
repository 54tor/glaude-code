import { Fragment, useState, useSyncExternalStore } from "react"
import { useTranslation } from "react-i18next"
import { CleanupWarning } from "./CleanupWarning"
import { AlarmClock, ChevronRight, FolderPlus, GalleryHorizontalEnd, FolderOpen, Star, Brain, Puzzle, SlidersHorizontal, Shredder, Flame, Microwave, Snowflake, History, Clock, FileText, Inbox, NotebookPen, Info, Loader2, Search, ShieldCheck, Tag, Users, X } from "lucide-react"
import { tilde } from "@/lib/format"
import { href, type Route } from "@/lib/route"
import type { Node } from "@/lib/tree"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { Tip } from "@/components/ui/tooltip"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { useKeepWarm } from "@/hooks/useKeepWarm"
import { CreditsButton } from "./Credits"
import { PairButton } from "./PairDevice"
import { HomeSwitcher } from "./HomeSwitcher"
import { DropZone } from "./DropZone"
import { ProjectList } from "./ProjectList"
import { ProjectItems, ProjectTree } from "./ProjectTree"
import { segs } from "@/lib/paths"
import { CM, GroupItems, SessionItems } from "./SessionMenu"
import { nest } from "./SessionList"
import type { Session, VGroup } from "@/lib/api"
import { useActions, type Row } from "./actions"


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
      <h2 className="flex-1 text-xs font-semibold text-muted-foreground">{children}</h2>
      {action}
    </div>
  )
}

// Sessions actives depuis moins d'une heure : leur cache est chaud, les reprendre coûte peu.
// En orange, celles dont le cache expire dans moins de 10 minutes.
function Hot({ rows, now, ttl, go }: { rows: Row[]; now: number; ttl: number; go: (r: Route) => void }) {
  const { t } = useTranslation()
  const a = useActions()
  const warm = useKeepWarm()
  // Un dossier par projet de travail, le plus récemment actif d'abord (les lignes arrivent déjà triées par date).
  const folders = new Map<string, Row[]>()
  for (const r of rows) folders.set(r.g.project, [...(folders.get(r.g.project) ?? []), r])
  const [opened, setOpened] = useState<Set<string>>(new Set())
  const toggle = (id: string) =>
    setOpened((o) => {
      const n = new Set(o)
      if (!n.delete(id)) n.add(id)
      return n
    })
  const item = (row: Row, kids: Row[] = [], slot = false): React.ReactNode => {
    const { s } = row
    const open = opened.has(s.id)
    const cold = now - s.mtime >= ttl // ouverte mais cache expiré
    const left = Math.max(1, Math.ceil((ttl - (now - s.mtime)) / 60))
    const warn = !cold && left <= 10
    return (
      <Fragment key={s.id}>
      <div className="flex items-center">
      {kids.length > 0 && (
        <button
          onClick={() => toggle(s.id)}
          aria-expanded={open}
          aria-label={t("shell.list.subSessions", { count: kids.length })}
          className="flex h-8 w-9 shrink-0 items-center text-xs text-muted-foreground hover:text-foreground"
        >
          <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
          <span className="tabular-nums">{kids.length}</span>
        </button>
      )}
      {kids.length === 0 && slot && <span className="w-9 shrink-0" aria-hidden />}
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <button
            onClick={() => a.read(s)}
            onDoubleClick={() => go({ kind: "dir", key: segs(row.g.project, a.home).join("/") })}
            title={cold ? t("shell.sidebar.coldTitle", { title: s.title }) : t("shell.sidebar.hotTitle", { title: s.title, left })}
            className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-sm text-sidebar-foreground/85 transition-colors hover:bg-sidebar-accent/60 hover:text-sidebar-foreground"
          >
            {a.asking.has(s.id) ? (
              <Info className="size-4 shrink-0 animate-pulse text-warning" aria-label={t("shell.sidebar.asking")} />
            ) : a.live.has(s.id) && !a.waiting.has(s.id) ? (
              <Loader2 className="size-4 shrink-0 animate-spin text-success" aria-label={t("shell.sidebar.working")} />
            ) : warn ? (
              <AlarmClock className="size-4 shrink-0 text-warning" aria-label={t("shell.sidebar.cacheExpiring")} />
            ) : warm[s.id]?.on ? (
              <Microwave className="warm-wave size-4 shrink-0" aria-label={t("shell.sidebar.keepWarmOn")} />
            ) : cold ? (
              <Snowflake className="size-4 shrink-0 text-info" aria-label={t("shell.sidebar.cold")} />
            ) : (
              <Flame className="size-4 shrink-0 text-warning" aria-hidden />
            )}
            <span className="flex-1 truncate text-left">{s.title}</span>
            {!cold && (
              <span className={cn("shrink-0 text-xs tabular-nums", warn ? "font-medium text-warning" : "text-muted-foreground")}>
                {t("shell.sidebar.minutes", { left })}
              </span>
            )}
            {a.live.has(s.id) && <span className="size-1.5 shrink-0 rounded-full bg-success" aria-label={t("shell.sidebar.open")} />}
          </button>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-64">
          <SessionItems M={CM} row={row} full />
        </ContextMenuContent>
      </ContextMenu>
      </div>
      {open && <div className="ml-3 border-l pl-1">{kids.map((c) => item(c))}</div>}
      </Fragment>
    )
  }
  return (
    <>
      <Heading>
        <span title={t("shell.sidebar.hotHint")}>
          {t("shell.sidebar.hot")}
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
                  <FolderOpen /> {t("shell.common.showFolder")}
                </ContextMenuItem>
              }
            />
          </ContextMenu>
          <div className="ml-3 border-l pl-1">
            {(() => {
              const { tops, kids } = nest(rs)
              const slot = tops.some((r) => kids.has(r.s.id))
              return tops.map((r) => item(r, kids.get(r.s.id), slot))
            })()}
          </div>
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
  auto: { n: number; purge: () => void }
  args: string // arguments de lancement des Réglages, rappelés en bas du menu
}

// Seuil md de Tailwind : en dessous, le menu est un tiroir.
const MD = "(min-width: 768px)"
const onWide = (cb: () => void) => {
  const m = matchMedia(MD)
  m.addEventListener("change", cb)
  return () => m.removeEventListener("change", cb)
}

// Favoris : une sous-session favorite se range sous sa parente favorite, repliée par défaut.
function FavSessions({ rows, read }: { rows: Row[]; read: (s: Session) => void }) {
  const { t } = useTranslation()
  const [opened, setOpened] = useState<Set<string>>(new Set())
  const { tops, kids } = nest(rows)
  const toggle = (id: string) =>
    setOpened((o) => {
      const n = new Set(o)
      if (!n.delete(id)) n.add(id)
      return n
    })
  const item = (row: Row, ks: Row[] = [], slot = false): React.ReactNode => (
    <Fragment key={row.s.id}>
      <div className="flex items-center">
        {ks.length > 0 && (
          <button
            onClick={() => toggle(row.s.id)}
            aria-expanded={opened.has(row.s.id)}
            aria-label={t("shell.list.subSessions", { count: ks.length })}
            className="flex h-8 w-9 shrink-0 items-center text-xs text-muted-foreground hover:text-foreground"
          >
            <ChevronRight className={cn("size-3.5 transition-transform", opened.has(row.s.id) && "rotate-90")} />
            <span className="tabular-nums">{ks.length}</span>
          </button>
        )}
        {ks.length === 0 && slot && <span className="w-9 shrink-0" aria-hidden />}
        <ContextMenu>
          <ContextMenuTrigger asChild>
            <div className="min-w-0 flex-1">
              <NavItem icon={Star} label={row.s.title} active={false} onClick={() => read(row.s)} />
            </div>
          </ContextMenuTrigger>
          <ContextMenuContent className="w-64">
            <SessionItems M={CM} row={row} full />
          </ContextMenuContent>
        </ContextMenu>
      </div>
      {opened.has(row.s.id) && <div className="ml-3 border-l pl-1">{ks.map((c) => item(c))}</div>}
    </Fragment>
  )
  const slot = tops.some((r) => kids.has(r.s.id))
  return <>{tops.map((r) => item(r, kids.get(r.s.id), slot))}</>
}

export function Sidebar({ searchRef, ...p }: SidebarProps) {
  const { t } = useTranslation()
  const r = p.route
  const link = (to: Route) => ({ to, onClick: () => p.go(to) })
  const a = useActions()
  const wide = useSyncExternalStore(onWide, () => matchMedia(MD).matches)
  const body = (
    <>
      <div className="space-y-3 p-3">
        <div className="flex items-center gap-2.5 px-1 pt-1">
          <img src="/icons/icon-192.png" alt="" className="size-10 shrink-0" />
          <div className="leading-tight">
            <h1 className="text-md font-semibold tracking-tight">Glaude Code</h1>
            <p className="text-xs text-muted-foreground tabular-nums">{p.loading ? t("common.loading") : t("shell.count.sessions", { count: p.total })}</p>
          </div>
          <div className="ml-auto flex items-center gap-0.5">
            <PairButton />
            <CreditsButton />
            <HomeSwitcher />
          </div>
        </div>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={searchRef}
            value={p.q}
            onChange={(e) => p.setQ(e.target.value)}
            // Échap vide la recherche et rend le focus à la page : j/k/t/d reprennent leur rôle de raccourcis.
            onKeyDown={(e) => {
              if (e.key !== "Escape") return
              p.setQ("")
              e.currentTarget.blur()
            }}
            placeholder={t("shell.sidebar.searchPlaceholder")}
            className="bg-background pr-8 pl-8 shadow-none"
            aria-label={t("shell.sidebar.searchLabel")}
          />
          {p.searching ? (
            <Loader2
              className="absolute top-1/2 right-2.5 size-4 -translate-y-1/2 animate-spin text-muted-foreground"
              aria-label={t("shell.sidebar.searching")}
            />
          ) : p.q ? (
            <button
              className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => p.setQ("")}
              aria-label={t("shell.sidebar.clearSearch")}
            >
              <X className="size-4" />
            </button>
          ) : (
            <kbd className="pointer-events-none absolute top-1/2 right-2 -translate-y-1/2 rounded-sm border bg-muted px-1.5 font-mono text-2xs text-muted-foreground">
              /
            </kbd>
          )}
        </div>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <CleanupWarning />
        <nav className="px-2 pb-3" aria-label={t("shell.sidebar.navigation")}>
          <NavItem icon={GalleryHorizontalEnd} label={t("shell.sidebar.carousel")} n={p.carousel.n} active={p.carousel.on} onClick={p.carousel.start} />
          <NavItem
            icon={Clock}
            label={t("shell.sidebar.recent")}
            n={p.counts.recent}
            active={r.kind === "recent"}
            {...link({ kind: "recent" })}
          />
          {(r.kind === "dir" || r.kind === "project") && (
            <NavItem icon={NotebookPen} label={t("shell.sidebar.addNote")} active={false} onClick={() => { window.dispatchEvent(new Event("gld:note-add")); p.onClose() }} />
          )}
          <NavItem icon={Inbox} label={t("shell.app.title.all")} n={p.counts.all} active={r.kind === "all"} {...link({ kind: "all" })} />
          <NavItem icon={History} label={t("shell.app.title.tabs")} active={r.kind === "tabs"} {...link({ kind: "tabs" })} />
          <NavItem icon={Puzzle} label={t("shell.app.title.extensions")} active={r.kind === "extensions" || r.kind === "skills"} {...link({ kind: "extensions" })} />
          <NavItem icon={Users} label={t("teams.title")} active={r.kind === "teams" || r.kind === "team"} {...link({ kind: "teams" })} />
          <NavItem icon={SlidersHorizontal} label={t("shell.app.title.config")} active={r.kind === "config"} {...link({ kind: "config", tab: "" })} />
          <NavItem icon={Brain} label={t("shell.app.title.memory")} active={r.kind === "memory"} {...link({ kind: "memory", key: "" })} />
          {p.snippets !== null && (
            <NavItem
              icon={FileText}
              label={t("shell.app.title.snippets")}
              n={p.snippets}
              active={r.kind === "snippets"}
              {...link({ kind: "snippets", path: "" })}
            />
          )}

          {p.hot.length > 0 && <Hot rows={p.hot} now={p.now} ttl={p.ttl} go={p.go} />}

          {(p.favRows.length > 0 || p.favGroups.length > 0) && (
            <>
              <Heading>{t("shell.sidebar.favorites")}</Heading>
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
              <FavSessions rows={p.favRows} read={p.read} />
            </>
          )}

          {a.projects.length > 0 && (
            <>
              <Heading>{t("shell.sidebar.projects")}</Heading>
              <ProjectList route={r} go={p.go} />
            </>
          )}

          <DropZone onDir={a.addDirPath}>
          <Heading
            action={
              <Tip label={t("shell.sidebar.addDirHint")}>
                <Button variant="ghost" size="icon-xs" onClick={a.addDir} aria-label={t("shell.sidebar.addDir")}>
                  <FolderPlus />
                </Button>
              </Tip>
            }
          >
            {t("shell.sidebar.local")}
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
          </DropZone>
        </nav>
      </ScrollArea>

      {(p.auto.n > 0 || p.args) && (
      <div className="space-y-2 border-t p-3">
        {p.auto.n > 0 && (
          <div className="flex items-center gap-2 px-1 text-xs">
            <ShieldCheck className="size-4 shrink-0 text-muted-foreground" />
            <span className="flex-1 text-muted-foreground">
              {t("shell.sidebar.autoHidden", { n: p.auto.n })}
            </span>
            <Button variant="ghost" size="xs" className="text-destructive hover:text-destructive" onClick={p.auto.purge}>
              <Shredder /> {t("shell.sidebar.clean")}
            </Button>
          </div>
        )}
        {p.args && (
          <code className="block truncate px-1 text-xs text-muted-foreground" title={t("shell.sidebar.argsHint")}>
            {p.args}
          </code>
        )}
      </div>
      )}
    </>
  )
  if (wide)
    return (
      <aside id="menu" aria-label={t("shell.sidebar.menu")} className="flex w-72 shrink-0 flex-col border-r bg-sidebar lg:w-80">
        {body}
      </aside>
    )
  // Petit écran : tiroir modal (focus piégé, Échap, page derrière inerte) au lieu d'une colonne translatée hors de l'écran.
  return (
    <Dialog open={p.open} onOpenChange={(o) => !o && p.onClose()}>
      <DialogContent
        id="menu"
        aria-describedby={undefined}
        showCloseButton={false}
        // Focus sur le tiroir lui-même, pas sur son premier bouton : sinon l'infobulle de ce bouton s'ouvre sur la recherche.
        onOpenAutoFocus={(e) => (e.preventDefault(), (e.currentTarget as HTMLElement).focus())}
        // Le tiroir s'ouvre depuis le bouton de l'en-tête, hors de tout DialogTrigger : on lui rend le focus à la main.
        onCloseAutoFocus={(e) => {
          e.preventDefault()
          document.querySelector<HTMLElement>('[aria-controls="menu"]')?.focus()
        }}
        className="safe-t safe-b top-0 left-0 flex h-dvh w-[85vw] max-w-sm translate-x-0 translate-y-0 flex-col gap-0 rounded-none border-0 border-r bg-sidebar p-0 text-sidebar-foreground duration-200 sm:max-w-sm data-open:zoom-in-100 data-open:slide-in-from-left data-closed:zoom-out-100 data-closed:slide-out-to-left"
      >
        <DialogTitle className="sr-only">{t("shell.sidebar.menu")}</DialogTitle>
        {body}
      </DialogContent>
    </Dialog>
  )
}
