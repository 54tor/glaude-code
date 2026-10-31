import { Fragment, useEffect, useState, useSyncExternalStore } from "react"
import { useTranslation } from "react-i18next"
import { CleanupWarning } from "./CleanupWarning"
import { CACHE_EXPIRY_WARNING_MINUTES } from "./KeepWarm"
import { AlarmClock, LayoutDashboard, ChevronRight, FolderPlus, GalleryHorizontalEnd, FolderOpen, Star, Brain, Puzzle, SlidersHorizontal, Shredder, Coffee, Microwave, Snowflake, History, FileText, Inbox, Info, Fan, Search, ShieldCheck, Tag, Users, X } from "lucide-react"
import { tilde } from "@/lib/format"
import { href, type Route } from "@/lib/route"
import type { Node } from "@/lib/tree"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { swipeHandlers } from "@/lib/swipe"
import { Tip } from "@/components/ui/tooltip"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { useKeepWarm } from "@/hooks/useKeepWarm"
import { useLocal } from "@/hooks/useLocal"
import { CreditsButton } from "./Credits"
import { PairButton } from "./PairDevice"
import { InvitesButton } from "./InvitesButton"
import { HomeSwitcher } from "./HomeSwitcher"
import { DropZone } from "./DropZone"
import { ProjectList } from "./ProjectList"
import { ProjectItems, ProjectTree } from "./ProjectTree"
import { segs } from "@/lib/paths"
import { CM, GroupItems, SessionItems } from "./SessionMenu"
import { nest } from "./SessionList"
import { api } from "@/lib/api"
import type { Session, VGroup } from "@/lib/api"
import { useActions, type Row } from "./actions"


// Avec `to`, un vrai lien : clic du milieu ou Ctrl+clic ouvrent la vue dans un autre onglet.
function NavItem({ icon: Icon, label, n, active, onClick, to }: { icon: typeof Inbox; label: string; n?: number; active: boolean; onClick: () => void; to?: Route }) {
  const itemClassName = cn(
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
        className={itemClassName}
        onClick={(clickEvent) => {
          const opensElsewhere = clickEvent.button !== 0 || clickEvent.metaKey || clickEvent.ctrlKey || clickEvent.shiftKey || clickEvent.altKey
          if (opensElsewhere) return
          clickEvent.preventDefault()
          onClick()
        }}
      >
        {body}
      </a>
    )
  return (
    <button onClick={onClick} aria-current={active} className={itemClassName}>
      {body}
    </button>
  )
}

function KidsBadge({ n, open, live, onClick }: { n: number; open: boolean; live?: boolean; onClick: () => void }) {
  const { t } = useTranslation()
  return (
    <button
      onClick={onClick}
      aria-expanded={open}
      aria-label={t("shell.list.subSessions", { count: n })}
      className="flex h-6 shrink-0 items-center gap-0.5 rounded-md px-1 text-xs tabular-nums text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground"
    >
      ×{n}
      <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
      {live && <span className="ml-0.5 size-1.5 rounded-full bg-success" aria-label={t("shell.sidebar.open")} />}
    </button>
  )
}

function Heading({ children, action, fold }: { children: React.ReactNode; action?: React.ReactNode; fold?: { folded: boolean; toggle: () => void } }) {
  return (
    <div className="mt-5 mb-1 flex items-center px-2">
      <h2 className="flex-1 text-xs font-semibold text-muted-foreground">
        {fold ? (
          <button onClick={fold.toggle} aria-expanded={!fold.folded} className="flex items-center gap-1 hover:text-foreground">
            <ChevronRight className={cn("size-3.5 transition-transform", !fold.folded && "rotate-90")} aria-hidden />
            {children}
          </button>
        ) : (
          children
        )}
      </h2>
      {action}
    </div>
  )
}

// Sessions actives depuis moins d'une heure : leur cache est chaud, les reprendre coûte peu.
// En orange, celles dont le cache expire dans moins de 10 minutes.
function Hot({ rows, now, ttl, go, title, hint, foldKey }: { rows: Row[]; now: number; ttl: number; go: (route: Route) => void; title: string; hint: string; foldKey?: string }) {
  const { t } = useTranslation()
  const actions = useActions()
  const [folded, setFolded] = useLocal(foldKey ?? "gld.hot.unfoldable", "")
  const fold = !!foldKey && folded === "1"
  const warm = useKeepWarm()
  // Un dossier par projet de travail, par ordre alphabétique ; dans un dossier, les sessions restent triées par date.
  const folders = new Map<string, Row[]>()
  for (const row of rows) folders.set(row.g.project, [...(folders.get(row.g.project) ?? []), row])
  const label = (projectPath: string) => projectPath.split("/").filter(Boolean).pop() ?? projectPath
  const ordered = [...folders].sort(([firstProject], [secondProject]) => label(firstProject).localeCompare(label(secondProject)) || firstProject.localeCompare(secondProject))
  const [opened, setOpened] = useState<Set<string>>(new Set())
  const toggle = (id: string) =>
    setOpened((previouslyOpened) => {
      const nextOpened = new Set(previouslyOpened)
      if (!nextOpened.delete(id)) nextOpened.add(id)
      return nextOpened
    })
  const item = (row: Row, kids: Row[] = []): React.ReactNode => {
    const { s: session } = row
    const open = opened.has(session.id)
    const cold = now - session.mtime >= ttl // ouverte mais cache expiré
    const left = Math.max(1, Math.ceil((ttl - (now - session.mtime)) / 60))
    const warn = !cold && left <= CACHE_EXPIRY_WARNING_MINUTES
    const statusIcon = () => {
      if (actions.asking.has(session.id)) return <Info className="size-4 shrink-0 animate-pulse text-warning" aria-label={t("shell.sidebar.asking")} />
      if (actions.live.has(session.id) && !actions.waiting.has(session.id)) return <Fan className="size-4 shrink-0 animate-spin text-success" aria-label={t("shell.sidebar.working")} />
      if (warn) return <AlarmClock className="size-4 shrink-0 text-warning" aria-label={t("shell.sidebar.cacheExpiring")} />
      if (warm[session.id]?.on) return <Microwave className="warm-wave size-4 shrink-0" aria-label={t("shell.sidebar.keepWarmOn")} />
      if (cold) return <Snowflake className="size-4 shrink-0 text-info" aria-label={t("shell.sidebar.cold")} />
      return <Coffee className="size-4 shrink-0 text-warning" aria-hidden />
    }
    return (
      <Fragment key={session.id}>
      <div className="flex items-center">
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <button
            onClick={() => actions.read(session)}
            onDoubleClick={() => go({ kind: "dir", key: segs(row.g.project, actions.home).join("/") })}
            title={cold ? t("shell.sidebar.coldTitle", { title: session.title }) : t("shell.sidebar.hotTitle", { title: session.title, left })}
            className="flex h-8 min-w-0 flex-1 items-center gap-2 rounded-md px-2 text-sm text-sidebar-foreground/85 transition-colors hover:bg-sidebar-accent/60 hover:text-sidebar-foreground"
          >
            {statusIcon()}
            <span className="flex-1 truncate text-left">{session.title}</span>
            {!cold && (
              <span className={cn("shrink-0 text-xs tabular-nums", warn ? "font-medium text-warning" : "text-muted-foreground")}>
                {t("shell.sidebar.minutes", { left })}
              </span>
            )}
            {actions.live.has(session.id) && <span className="size-1.5 shrink-0 rounded-full bg-success" aria-label={t("shell.sidebar.open")} />}
          </button>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-64">
          <SessionItems M={CM} row={row} full />
        </ContextMenuContent>
      </ContextMenu>
      {kids.length > 0 && <KidsBadge n={kids.length} open={open} live={kids.some((childRow) => actions.live.has(childRow.s.id))} onClick={() => toggle(session.id)} />}
      </div>
      {open && <div className="ml-4 border-l pl-[7px]">{kids.map((childRow) => item(childRow))}</div>}
      </Fragment>
    )
  }
  return (
    <>
      <Heading>
        {foldKey ? (
          <button onClick={() => setFolded(fold ? "" : "1")} aria-expanded={!fold} title={hint} className="flex items-center gap-1 hover:text-foreground">
            <ChevronRight className={cn("size-3.5 transition-transform", !fold && "rotate-90")} aria-hidden />
            {title} <span className="tabular-nums">{rows.length}</span>
          </button>
        ) : (
          <span title={hint}>{title}</span>
        )}
      </Heading>
      {!fold && ordered.map(([project, projectRows]) => {
        const { tops, kids } = nest(projectRows)
        return (
        <details key={project} open className="group/hot">
          <ContextMenu>
            <ContextMenuTrigger asChild>
              <summary
                title={tilde(project, actions.home)}
                className="flex h-7 cursor-pointer list-none items-center gap-1.5 rounded-md px-2 text-xs font-medium text-muted-foreground hover:bg-muted [&::-webkit-details-marker]:hidden"
              >
                <ChevronRight className="size-3.5 shrink-0 transition-transform group-open/hot:rotate-90" aria-hidden />
                <span className="flex-1 truncate">{label(project)}</span>
                <span className="tabular-nums">{projectRows.length}</span>
              </summary>
            </ContextMenuTrigger>
            <ProjectItems
              g={projectRows[0].g}
              ss={projectRows.map((projectRow) => projectRow.s)}
              extra={
                <ContextMenuItem onSelect={() => go({ kind: "dir", key: segs(project, actions.home).join("/") })}>
                  <FolderOpen /> {t("shell.common.showFolder")}
                </ContextMenuItem>
              }
            />
          </ContextMenu>
          <div className="ml-3 border-l pl-1">
            {tops.map((topRow) => item(topRow, kids.get(topRow.s.id)))}
          </div>
        </details>
        )
      })}
    </>
  )
}

export type SidebarProps = {
  open: boolean // mobile : le tiroir est ouvert
  onClose: () => void
  loading: boolean
  total: number
  searchRef: React.RefObject<HTMLInputElement | null>
  route: Route
  go: (r: Route) => void
  counts: { all: number; active: number }
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
  auto: { n: number; purge: () => void; on: boolean }
  args: string // arguments de lancement des Réglages, rappelés en bas du menu
}

// Seuil md de Tailwind : en dessous, le menu est un tiroir.
export const MD = "(min-width: 768px)"
const onWide = (onChange: () => void) => {
  const wideQuery = matchMedia(MD)
  wideQuery.addEventListener("change", onChange)
  return () => wideQuery.removeEventListener("change", onChange)
}

// Favoris : une sous-session favorite se range sous sa parente favorite, repliée par défaut.
function FavSessions({ rows, read }: { rows: Row[]; read: (session: Session) => void }) {
  const [opened, setOpened] = useState<Set<string>>(new Set())
  const { tops, kids } = nest(rows)
  const toggle = (id: string) =>
    setOpened((previouslyOpened) => {
      const nextOpened = new Set(previouslyOpened)
      if (!nextOpened.delete(id)) nextOpened.add(id)
      return nextOpened
    })
  const item = (row: Row, childRows: Row[] = []): React.ReactNode => (
    <Fragment key={row.s.id}>
      <div className="flex items-center">
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
        {childRows.length > 0 && <KidsBadge n={childRows.length} open={opened.has(row.s.id)} onClick={() => toggle(row.s.id)} />}
      </div>
      {opened.has(row.s.id) && <div className="ml-4 border-l pl-[7px]">{childRows.map((childRow) => item(childRow))}</div>}
    </Fragment>
  )
  return <>{tops.map((topRow) => item(topRow, kids.get(topRow.s.id)))}</>
}

// Garde un nœud dont le nom correspond (avec tout son sous-arbre) ou dont un descendant correspond.
function filterTree(nodes: Node[], query: string): Node[] {
  return nodes.flatMap((node) => {
    if (node.name.toLowerCase().includes(query)) return [node]
    const matchingKids = filterTree([...node.kids.values()], query)
    if (!matchingKids.length) return []
    return [{ ...node, kids: new Map(matchingKids.map((kid) => [kid.name, kid])) }]
  })
}

// Pied de navbar : version courante (un build de travail affiche son `git describe`) et lien vers la release plus récente.
function VersionFooter() {
  const { t } = useTranslation()
  const [updateInfo, setUpdateInfo] = useState<{ current: string; latest: string; url: string; newer: boolean }>()
  useEffect(() => {
    api.updateInfo().then(setUpdateInfo, () => {})
  }, [])
  if (!updateInfo) return null
  return (
    <div className="flex items-center gap-2 border-t px-4 py-2 text-xs text-muted-foreground tabular-nums">
      <span className="truncate" title={updateInfo.current}>{updateInfo.current}</span>
      {updateInfo.newer && (
        <a href={updateInfo.url} target="_blank" rel="noopener noreferrer" className="ml-auto shrink-0 font-medium text-primary hover:underline">
          {t("shell.sidebar.updateTo", { latest: updateInfo.latest })}
        </a>
      )}
    </div>
  )
}

export function Sidebar({ searchRef, ...props }: SidebarProps) {
  const { t } = useTranslation()
  // Recherche propre à la navbar : indépendante de celle de la barre du haut (paramètre d'URL q).
  const [navQuery, setNavQuery] = useState("")
  const normalizedNavQuery = navQuery.trim().toLowerCase()
  const show = (label: string) => !normalizedNavQuery || label.toLowerCase().includes(normalizedNavQuery)
  const tree = normalizedNavQuery ? filterTree(props.tree, normalizedNavQuery) : props.tree
  const favGroups = props.favGroups.filter(({ vg }) => show(vg.name))
  const actions = useActions()
  const [projFold, setProjFold] = useLocal("gld.projects.folded", "")
  const [localFold, setLocalFold] = useLocal("gld.local.folded", "")
  const openRows = props.hot.filter((hotRow) => actions.live.has(hotRow.s.id))
  const warmRows = props.hot.filter((hotRow) => !actions.live.has(hotRow.s.id) && props.now - hotRow.s.mtime < props.ttl)
  const favRows = props.favRows.filter((favRow) => show(favRow.s.title))
  const currentRoute = props.route
  const link = (to: Route) => ({ to, onClick: () => props.go(to) })
  const wide = useSyncExternalStore(onWide, () => matchMedia(MD).matches)
  const body = (
    <>
      <div className="space-y-3 p-3">
        <div className="flex items-center gap-2.5 px-1 pt-1">
          <img src="/icons/icon-192.png" alt="" className="size-10 shrink-0" />
          <div className="leading-tight">
            <h1 className="text-md font-semibold tracking-tight">Glaude Code</h1>
            <p className="text-xs text-muted-foreground tabular-nums">{props.loading ? t("common.loading") : t("shell.count.sessions", { count: props.total })}</p>
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
            value={navQuery}
            onChange={(changeEvent) => setNavQuery(changeEvent.target.value)}
            // Échap vide la recherche et rend le focus à la page : j/k/t/d reprennent leur rôle de raccourcis.
            onKeyDown={(keyboardEvent) => {
              if (keyboardEvent.key !== "Escape") return
              setNavQuery("")
              keyboardEvent.currentTarget.blur()
            }}
            placeholder={t("shell.sidebar.searchPlaceholder")}
            className="bg-background pr-8 pl-8 shadow-none"
            aria-label={t("shell.sidebar.searchLabel")}
          />
          {navQuery ? (
            <button
              className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => setNavQuery("")}
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
          {show(t("shell.app.title.home")) && <NavItem icon={LayoutDashboard} label={t("shell.app.title.home")} active={currentRoute.kind === "home"} {...link({ kind: "home" })} />}
          {show(t("shell.sidebar.carousel")) && <NavItem icon={GalleryHorizontalEnd} label={t("shell.sidebar.carousel")} n={props.carousel.n} active={props.carousel.on} onClick={props.carousel.start} />}
          {show(t("shell.app.title.all")) && <NavItem icon={Inbox} label={t("shell.app.title.all")} n={props.counts.all} active={currentRoute.kind === "all"} {...link({ kind: "all" })} />}
          {show(t("shell.app.title.tabs")) && <NavItem icon={History} label={t("shell.app.title.tabs")} active={currentRoute.kind === "tabs"} {...link({ kind: "tabs" })} />}
          {show(t("shell.app.title.extensions")) && <NavItem icon={Puzzle} label={t("shell.app.title.extensions")} active={currentRoute.kind === "extensions" || currentRoute.kind === "skills"} {...link({ kind: "extensions" })} />}
          {show(t("teams.title")) && <NavItem icon={Users} label={t("teams.title")} active={currentRoute.kind === "teams" || currentRoute.kind === "team"} {...link({ kind: "teams" })} />}
          {!normalizedNavQuery && <InvitesButton />}
          {show(t("shell.app.title.config")) && <NavItem icon={SlidersHorizontal} label={t("shell.app.title.config")} active={currentRoute.kind === "config"} {...link({ kind: "config", tab: "" })} />}
          {show(t("shell.app.title.memory")) && <NavItem icon={Brain} label={t("shell.app.title.memory")} active={currentRoute.kind === "memory"} {...link({ kind: "memory", key: "" })} />}
          {props.snippets !== null && show(t("shell.app.title.snippets")) && (
            <NavItem
              icon={FileText}
              label={t("shell.app.title.snippets")}
              n={props.snippets}
              active={currentRoute.kind === "snippets"}
              {...link({ kind: "snippets", path: "" })}
            />
          )}

          {!normalizedNavQuery && openRows.length > 0 && <Hot rows={openRows} now={props.now} ttl={props.ttl} go={props.go} title={t("shell.sidebar.hot")} hint={t("shell.sidebar.hotHint")} foldKey="gld.hot.openFolded" />}
          {!normalizedNavQuery && warmRows.length > 0 && <Hot rows={warmRows} now={props.now} ttl={props.ttl} go={props.go} title={t("shell.sidebar.warmClosed")} hint={t("shell.sidebar.warmClosedHint")} foldKey="gld.hot.warmFolded" />}

          {(favRows.length > 0 || favGroups.length > 0) && (
            <>
              <Heading>{t("shell.sidebar.favorites")}</Heading>
              {favGroups.map(({ vg, key }) => (
                <ContextMenu key={key + "\0" + vg.name}>
                  <ContextMenuTrigger asChild>
                    <div>
                      <NavItem
                        icon={Tag}
                        label={vg.name}
                        n={props.groupCount(vg)}
                        active={currentRoute.kind === "group" && currentRoute.name === vg.name && currentRoute.key === key}
                        {...link({ kind: "group", name: vg.name, key })}
                      />
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent className="w-72">
                    <GroupItems M={CM} vg={vg} />
                  </ContextMenuContent>
                </ContextMenu>
              ))}
              <FavSessions rows={favRows} read={props.read} />
            </>
          )}

          {actions.projects.some((project) => show(project.name)) && (
            <>
              <Heading fold={{ folded: projFold === "1", toggle: () => setProjFold(projFold ? "" : "1") }}>{t("shell.sidebar.projects")}</Heading>
              {projFold !== "1" && <ProjectList route={currentRoute} go={props.go} q={normalizedNavQuery} />}
            </>
          )}

          <DropZone onDir={actions.addDirPath}>
          <Heading
            fold={{ folded: localFold === "1", toggle: () => setLocalFold(localFold ? "" : "1") }}
            action={
              <Tip label={t("shell.sidebar.addDirHint")}>
                <Button variant="ghost" size="icon-xs" onClick={actions.addDir} aria-label={t("shell.sidebar.addDir")}>
                  <FolderPlus />
                </Button>
              </Tip>
            }
          >
            {t("shell.sidebar.local")}
          </Heading>
          {localFold !== "1" && props.loading && (
            <div className="space-y-2 px-2">
              <Skeleton className="h-6 w-full" />
              <Skeleton className="h-6 w-4/5" />
              <Skeleton className="h-6 w-3/5" />
            </div>
          )}
          {localFold !== "1" && !props.loading && (
            <ProjectTree
              nodes={tree}
              sel={currentRoute.kind === "dir" ? currentRoute.key : ""}
              onSel={(key) => props.go({ kind: "dir", key })}
              selGroup={currentRoute.kind === "group" ? currentRoute.key + "\0" + currentRoute.name : ""}
              onGroup={(virtualGroup, key) => props.go({ kind: "group", name: virtualGroup.name, key })}
            />
          )}
          </DropZone>
        </nav>
      </ScrollArea>

      {(props.auto.n > 0 || props.args) && (
      <div className="space-y-2 border-t p-3">
        {props.auto.n > 0 && (
          <div className="flex items-center gap-1">
            <div className="min-w-0 flex-1">
              <NavItem icon={ShieldCheck} label={t("shell.app.title.auto")} n={props.auto.n} active={props.auto.on} {...link({ kind: "auto" })} />
            </div>
            <Button variant="ghost" size="xs" className="text-destructive hover:text-destructive" onClick={props.auto.purge}>
              <Shredder /> {t("shell.sidebar.clean")}
            </Button>
          </div>
        )}
        {props.args && (
          <code className="block truncate px-1 text-xs text-muted-foreground" title={t("shell.sidebar.argsHint")}>
            {props.args}
          </code>
        )}
      </div>
      )}
      <VersionFooter />
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
    <Dialog open={props.open} onOpenChange={(isOpen) => !isOpen && props.onClose()}>
      <DialogContent sheet={false}
        id="menu"
        aria-describedby={undefined}
        showCloseButton={false}
        {...swipeHandlers((swipeDirection) => swipeDirection === "left" && props.onClose())}
        // Focus sur le tiroir lui-même, pas sur son premier bouton : sinon l'infobulle de ce bouton s'ouvre sur la recherche.
        onOpenAutoFocus={(focusEvent) => {
          focusEvent.preventDefault()
          const drawer = focusEvent.currentTarget as HTMLElement
          drawer.focus()
        }}
        // Le tiroir s'ouvre depuis le bouton de l'en-tête, hors de tout DialogTrigger : on lui rend le focus à la main.
        onCloseAutoFocus={(focusEvent) => {
          focusEvent.preventDefault()
          document.querySelector<HTMLElement>('[data-menu-trigger]')?.focus()
        }}
        className="safe-t safe-b top-0 left-0 flex h-dvh w-[85vw] max-w-sm translate-x-0 translate-y-0 flex-col gap-0 rounded-none border-0 border-r bg-sidebar p-0 text-sidebar-foreground duration-200 sm:max-w-sm data-open:zoom-in-100 data-open:slide-in-from-left data-closed:zoom-out-100 data-closed:slide-out-to-left"
      >
        <DialogTitle className="sr-only">{t("shell.sidebar.menu")}</DialogTitle>
        {body}
      </DialogContent>
    </Dialog>
  )
}
