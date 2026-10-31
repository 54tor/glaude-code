import { asked } from "@/lib/sessionsView"
import { loc } from "@/i18n"
import { useTranslation } from "react-i18next"
import { Fragment, memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { ChevronRight, Coffee, Globe, MoreHorizontal, SquareTerminal, Star, Tag } from "lucide-react"
import { ago, bucket, compact, full, tilde, tokTotal } from "@/lib/format"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useActions, type Row as SessionRow } from "./actions"
import { CM, SessionDropdown, SessionItems } from "./SessionMenu"

// Point vert : la session tourne en ce moment dans un terminal.
// Tâche de fond (`claude --bg`) : ouverte sans terminal, elle s'arrête depuis le menu « Fermer ».
export function BgBadge({ id }: { id: string }) {
  const { t } = useTranslation()
  const actions = useActions()
  if (!actions.background.has(id)) return null
  return (
    <Badge variant="outline" className="shrink-0" title={t("shell.list.backgroundHint")}>
      {t("shell.list.background")}
    </Badge>
  )
}

export function Live({ className, waiting }: { className?: string; waiting?: boolean }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 font-medium",
        waiting ? "text-warning" : "text-success",
        className
      )}
    >
      <span className={cn("size-1.5 rounded-full bg-current", !waiting && "animate-pulse")} /> {waiting ? t("shell.list.waiting") : t("shell.list.running")}
    </span>
  )
}

// Extrait de conversation, termes recherchés surlignés.
// Extrait du serveur : les correspondances sont entourées de \u0002…\u0003.
function Snip({ text }: { text: string }) {
  const parts = text.split("\u0003").flatMap((segment) => {
    const [before, hit] = segment.split("\u0002")
    if (hit === undefined) return [{ text: before, isHit: false }]
    return [
      { text: before, isHit: false },
      { text: hit, isHit: true },
    ]
  })
  return (
    <span className="mt-1 line-clamp-2 text-xs text-muted-foreground">
      {parts.map((part, partIndex) =>
        part.isHit ? (
          <mark key={partIndex} className="rounded-sm bg-amber-200/70 px-0.5 text-foreground dark:bg-amber-500/30">
            {part.text}
          </mark>
        ) : (
          part.text
        )
      )}
    </span>
  )
}

// memo : une ligne ne se redessine que si sa session, ses options ou le contexte des actions changent.
const Row = memo(function Row({
  row,
  showProject,
  showCache,
  kids = 0,
  open,
  onToggle,
  child,
}: {
  row: SessionRow
  showProject: boolean
  showCache?: boolean
  kids?: number
  open?: boolean
  onToggle?: () => void
  child?: boolean
}) {
  const { t } = useTranslation()
  const actions = useActions()
  const { s: session, g: group } = row
  const live = actions.live.has(session.id)
  const left = showCache ? actions.hotLeft(session) : null
  const picked = actions.picked.has(session.id)
  const hit = actions.hit(session.id)
  // Une session peut appartenir à plusieurs groupes de son dossier.
  const isOtherGroupOfSession = (viewGroup: (typeof actions.vgroups)[number]) =>
    viewGroup.project === group.project && viewGroup.ids.includes(session.id) && viewGroup.name !== actions.inGroup?.name
  const groups = actions.vgroups.filter(isOtherGroupOfSession).map((viewGroup) => viewGroup.name)
  const tokenTotal = tokTotal(session.tok)
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <li
          className={cn(
            "group relative flex items-center gap-3 rounded-md px-3 py-2.5 transition-colors hover:bg-muted/70 has-[[data-state=open]]:bg-muted/70 data-[state=open]:bg-muted/70",
            picked && "bg-accent hover:bg-accent",
            child && "ml-8"
          )}
        >
          <Checkbox
            checked={picked}
            onClick={(event) => actions.pick(session.id, event.shiftKey)}
            onMouseDown={(event) => event.shiftKey && event.preventDefault()}
            aria-label={t("shell.list.select")}
            className={cn(
              "relative z-10 transition-opacity",
              !picked && actions.selected.length === 0 && "md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100 pointer-coarse:hidden"
            )}
          />
          {kids > 0 && (
            <button
              onClick={onToggle}
              aria-expanded={open}
              aria-label={t("shell.list.subSessions", { count: kids })}
              className="relative z-10 flex shrink-0 items-center gap-0.5 rounded-sm text-xs text-muted-foreground hover:text-foreground"
            >
              <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
              <span className="tabular-nums">{kids}</span>
            </button>
          )}
          <button
            data-row
            data-id={session.id}
            className="min-w-0 flex-1 text-left outline-none after:absolute after:inset-0 after:rounded-lg focus-visible:after:ring-2 focus-visible:after:ring-ring/80"
            onClick={() => actions.read(session)}
          >
            <span className={cn("line-clamp-1 text-md", session.named ? "font-medium" : "text-foreground/85")}>
              {actions.favs.has(session.id) && <Star className="mr-1 inline size-3.5 -translate-y-px fill-amber-400 text-amber-400" aria-label={t("shell.list.favorite")} />}
              {session.title}
            </span>
            <span className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
              {live && <Live waiting={actions.waiting.has(session.id)} />}
              {live && <BgBadge id={session.id} />}
              {live && actions.rc.has(session.id) && <Globe className="size-3.5 shrink-0 text-success" aria-label={t("shell.list.remoteControl")} />}
              {left !== null && (
                <>
                  <span
                    className={cn(
                      "inline-flex shrink-0 items-center gap-1",
                      left <= CACHE_LOW_THRESHOLD ? "font-medium text-warning" : "text-warning"
                    )}
                  >
                    <Coffee className="size-3.5" aria-hidden /> {t("shell.list.cache", { left })}
                  </span>
                </>
              )}
              <span title={full(session.mtime)} className="shrink-0">
                {ago(session.mtime)}
              </span>
              {tokenTotal > 0 && (
                <>
                  <span
                    className="shrink-0 tabular-nums"
                    title={t("shell.list.tokensDetail", {
                      in: session.tok.in.toLocaleString(loc()),
                      out: session.tok.out.toLocaleString(loc()),
                      cw: session.tok.cw.toLocaleString(loc()),
                      cr: session.tok.cr.toLocaleString(loc()),
                    })}
                  >
                    {t("shell.list.tokens", { n: compact(tokenTotal) })}
                  </span>
                </>
              )}
              {hit && (
                <>
                  <span className="shrink-0 text-primary">
                    {t("shell.list.messages", { count: hit.n })}
                  </span>
                </>
              )}
              {showProject && (
                <>
                  <span className="truncate">{tilde(group.project, actions.home)}</span>
                </>
              )}
              {groups.length > 0 && (
                <span className="flex min-w-0 items-center gap-1 text-info" title={t("shell.list.groups", { names: groups.join(", ") })}>
                  <Tag className="size-3 shrink-0" />
                  <span className="truncate">{groups.join(", ")}</span>
                </span>
              )}
            </span>
            {hit && <Snip text={hit.snip} />}
          </button>
          {/* Actions au-dessus du bouton étiré : visibles au survol, au focus clavier, ou menu ouvert. */}
          <div className="relative z-10 flex items-center gap-1 transition-opacity md:opacity-0 md:group-focus-within:opacity-100 md:group-hover:opacity-100 md:has-[[data-state=open]]:opacity-100">
            {!group.missing && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="ghost" size="icon-sm" onClick={() => actions.open({ dir: session.dir, id: session.id })} aria-label={t("shell.list.resume")}>
                    <SquareTerminal />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{live ? t("shell.list.resumeAnyway") : t("shell.list.resume")}</TooltipContent>
              </Tooltip>
            )}
            <SessionDropdown row={row}>
              <Button variant="ghost" size="icon-sm" aria-label={t("shell.list.moreActions")}>
                <MoreHorizontal />
              </Button>
            </SessionDropdown>
          </div>
        </li>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-64">
        <SessionItems M={CM} row={row} full />
      </ContextMenuContent>
    </ContextMenu>
  )
})

const CACHE_LOW_THRESHOLD = 10

// Sessions triées de la plus récente à la plus ancienne, regroupées par période (sauf flat).
const STEP = 100 // lignes rendues d'un coup ; la suite arrive en faisant défiler

// Fin de liste : quand elle approche de l'écran, on demande la suite.
function More({ onMore }: { onMore: () => void }) {
  const ref = useRef<HTMLLIElement>(null)
  useEffect(() => {
    const sentinel = ref.current
    if (!sentinel) return
    const observer = new IntersectionObserver(([entry]) => entry.isIntersecting && onMore(), {
      rootMargin: "800px",
    })
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [onMore])
  return <li ref={ref} aria-hidden className="h-px" />
}

// Une session lancée par une autre (sous-agent d'un plan) se range sous sa parente, repliée par défaut.
export function nest(all: SessionRow[]) {
  const ids = new Set(all.map((row) => row.s.id))
  const kids = new Map<string, SessionRow[]>()
  const tops: SessionRow[] = []
  for (const row of all) {
    const parentId = row.s.parent
    if (parentId && ids.has(parentId)) kids.set(parentId, [...(kids.get(parentId) ?? []), row])
    else tops.push(row)
  }
  return { tops, kids }
}

export function SessionList({ items: every, showProject, flat, showCache }: { items: SessionRow[]; showProject: boolean; flat?: boolean; showCache?: boolean }) {
  const { tops: all, kids } = useMemo(() => nest(every), [every])
  const [opened, setOpened] = useState<Set<string>>(new Set())
  const toggle = useCallback(
    (id: string) =>
      setOpened((openedIds) => {
        const nextOpenedIds = new Set(openedIds)
        const wasOpen = nextOpenedIds.delete(id)
        if (!wasOpen) nextOpenedIds.add(id)
        return nextOpenedIds
      }),
    []
  )
  // Chargement paresseux : seules les premières lignes sont rendues (milliers de sessions possibles).
  const [limit, setLimit] = useState(STEP)
  const more = useCallback(() => setLimit((currentLimit) => currentLimit + STEP), [])
  const items = all.slice(0, limit)
  const rest = all.length > limit
  const line = (row: SessionRow) => {
    const children = kids.get(row.s.id) ?? []
    const open = opened.has(row.s.id)
    return (
      <Fragment key={row.s.id}>
        <Row row={row} showProject={showProject} showCache={showCache} kids={children.length} open={open} onToggle={() => toggle(row.s.id)} />
        {open && children.map((childRow) => <Row key={childRow.s.id} row={childRow} showProject={showProject} showCache={showCache} child />)}
      </Fragment>
    )
  }
  if (flat)
    return (
      <ul className="space-y-px">
        {items.map(line)}
        {rest && <More onMore={more} />}
      </ul>
    )
  const groups: [string, SessionRow[]][] = []
  for (const item of items) {
    const period = bucket(asked(item.s))
    if (groups.at(-1)?.[0] !== period) groups.push([period, []])
    groups.at(-1)![1].push(item)
  }
  return (
    <div className="space-y-6">
      {groups.map(([period, list]) => (
        <section key={period}>
          <h3 className="sticky top-0 z-20 border-b bg-background px-3 py-1.5 text-xs font-semibold text-muted-foreground">{period}</h3>
          <ul className="space-y-px">
            {list.map(line)}
          </ul>
        </section>
      ))}
      {rest && (
        <ul>
          <More onMore={more} />
        </ul>
      )}
    </div>
  )
}
