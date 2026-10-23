import { loc } from "@/i18n"
import { useTranslation } from "react-i18next"
import { useCallback, useEffect, useRef, useState } from "react"
import { Flame, MoreHorizontal, SquareTerminal, Star, Tag } from "lucide-react"
import { ago, bucket, compact, full, size, tilde, tokTotal } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useActions, type Row as R } from "./actions"
import { CM, SessionDropdown, SessionItems } from "./SessionMenu"

// Point vert : la session tourne en ce moment dans un terminal.
export function Live({ className, waiting }: { className?: string; waiting?: boolean }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 font-medium",
        waiting ? "text-amber-600 dark:text-amber-400" : "text-emerald-600 dark:text-emerald-400",
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
  const parts = text.split("\u0003").flatMap((seg) => {
    const [pre, hit] = seg.split("\u0002")
    return hit === undefined
      ? [{ t: pre, m: false }]
      : [
          { t: pre, m: false },
          { t: hit, m: true },
        ]
  })
  return (
    <span className="mt-1 line-clamp-2 text-xs text-muted-foreground">
      {parts.map((p, i) =>
        p.m ? (
          <mark key={i} className="rounded-sm bg-amber-200/70 px-0.5 text-foreground dark:bg-amber-500/30">
            {p.t}
          </mark>
        ) : (
          p.t
        )
      )}
    </span>
  )
}

function Row({ row, showProject, showCache }: { row: R; showProject: boolean; showCache?: boolean }) {
  const { t } = useTranslation()
  const a = useActions()
  const { s, g } = row
  const live = a.live.has(s.id)
  const left = showCache ? a.hotLeft(s) : null
  const picked = a.picked.has(s.id)
  const hit = a.hit(s.id)
  const tags = a.tags[s.id] ?? []
  // Une session peut appartenir à plusieurs groupes de son dossier.
  const groups = a.vgroups.filter((x) => x.project === g.project && x.ids.includes(s.id) && x.name !== a.inGroup?.name).map((x) => x.name)
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <li
          className={cn(
            "group relative flex items-center gap-3 rounded-md px-3 py-2.5 transition-colors hover:bg-muted/70 has-[[data-state=open]]:bg-muted/70 data-[state=open]:bg-muted/70",
            picked && "bg-accent hover:bg-accent"
          )}
        >
          <Checkbox
            checked={picked}
            onClick={(e) => a.pick(s.id, e.shiftKey)}
            onMouseDown={(e) => e.shiftKey && e.preventDefault()}
            aria-label={t("shell.list.select")}
            className={cn(
              "relative z-10 transition-opacity",
              !picked && a.selected.length === 0 && "md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100"
            )}
          />
          <button
            data-row
            data-id={s.id}
            className="min-w-0 flex-1 text-left outline-none after:absolute after:inset-0 after:rounded-lg focus-visible:after:ring-2 focus-visible:after:ring-ring/50"
            onClick={() => a.read(s)}
          >
            <span className={cn("line-clamp-1 text-[15px] leading-6", s.named ? "font-medium" : "text-foreground/85")}>
              {a.favs.has(s.id) && <Star className="mr-1 inline size-3.5 -translate-y-px fill-amber-400 text-amber-400" aria-label={t("shell.list.favorite")} />}
              {s.title}
            </span>
            <span className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
              {live && <Live waiting={a.waiting.has(s.id)} />}
              {left !== null && (
                <>
                  <span
                    className={cn(
                      "inline-flex shrink-0 items-center gap-1",
                      left <= 10 ? "font-medium text-amber-600 dark:text-amber-400" : "text-orange-500"
                    )}
                  >
                    <Flame className="size-3.5" aria-hidden /> {t("shell.list.cache", { left })}
                  </span>
                </>
              )}
              <span title={full(s.mtime)} className="shrink-0">
                {ago(s.mtime)}
              </span>
              <span className="shrink-0 tabular-nums">{size(s.kb)}</span>
              {tokTotal(s.tok) > 0 && (
                <>
                  <span
                    className="shrink-0 tabular-nums"
                    title={t("shell.list.tokensDetail", {
                      in: s.tok.in.toLocaleString(loc()),
                      out: s.tok.out.toLocaleString(loc()),
                      cw: s.tok.cw.toLocaleString(loc()),
                      cr: s.tok.cr.toLocaleString(loc()),
                    })}
                  >
                    {t("shell.list.tokens", { n: compact(tokTotal(s.tok)) })}
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
                  <span className="truncate">{tilde(g.project, a.home)}</span>
                </>
              )}
              {tags.map((tag) => (
                <button
                  key={tag}
                  className="relative z-10 shrink-0 rounded-sm bg-accent px-1.5 text-accent-foreground hover:bg-primary/20"
                  onClick={() => a.filterTag(tag)}
                  title={t("shell.list.filterTag", { tag })}
                >
                  #{tag}
                </button>
              ))}
              {groups.length > 0 && (
                <span className="flex min-w-0 items-center gap-1 text-violet-600 dark:text-violet-400" title={t("shell.list.groups", { names: groups.join(", ") })}>
                  <Tag className="size-3 shrink-0" />
                  <span className="truncate">{groups.join(", ")}</span>
                </span>
              )}
            </span>
            {hit && <Snip text={hit.snip} />}
          </button>
          {/* Actions au-dessus du bouton étiré : visibles au survol, au focus clavier, ou menu ouvert. */}
          <div className="relative z-10 flex items-center gap-1 transition-opacity md:opacity-0 md:group-focus-within:opacity-100 md:group-hover:opacity-100 md:has-[[data-state=open]]:opacity-100">
            {!g.missing && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="ghost" size="icon-sm" onClick={() => a.open({ dir: s.dir, id: s.id })} aria-label={t("shell.list.resume")}>
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
}

// Sessions triées de la plus récente à la plus ancienne, regroupées par période (sauf flat).
const STEP = 100 // lignes rendues d'un coup ; la suite arrive en faisant défiler

// Fin de liste : quand elle approche de l'écran, on demande la suite.
function More({ onMore }: { onMore: () => void }) {
  const ref = useRef<HTMLLIElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const io = new IntersectionObserver(([e]) => e.isIntersecting && onMore(), {
      rootMargin: "800px",
    })
    io.observe(el)
    return () => io.disconnect()
  }, [onMore])
  return <li ref={ref} aria-hidden className="h-px" />
}

export function SessionList({ items: all, showProject, flat, showCache }: { items: R[]; showProject: boolean; flat?: boolean; showCache?: boolean }) {
  // Chargement paresseux : seules les premières lignes sont rendues (milliers de sessions possibles).
  const [limit, setLimit] = useState(STEP)
  const more = useCallback(() => setLimit((n) => n + STEP), [])
  const items = all.slice(0, limit)
  const rest = all.length > limit
  if (flat)
    return (
      <ul className="space-y-px">
        {items.map((r) => (
          <Row key={r.s.id} row={r} showProject={showProject} showCache={showCache} />
        ))}
        {rest && <More onMore={more} />}
      </ul>
    )
  const groups: [string, R[]][] = []
  for (const it of items) {
    const b = bucket(it.s.mtime)
    if (groups.at(-1)?.[0] !== b) groups.push([b, []])
    groups.at(-1)![1].push(it)
  }
  return (
    <div className="space-y-6">
      {groups.map(([b, list]) => (
        <section key={b}>
          <h3 className="sticky top-0 z-20 border-b bg-background px-3 py-1.5 text-xs font-semibold text-muted-foreground">{b}</h3>
          <ul className="space-y-px">
            {list.map((r) => (
              <Row key={r.s.id} row={r} showProject={showProject} showCache={showCache} />
            ))}
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
