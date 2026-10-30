import { useState } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRight, Lock, Search, WifiOff, RefreshCcwDot } from "lucide-react"
import type { VGroup } from "@/lib/api"
import { tilde } from "@/lib/format"
import { segs } from "@/lib/paths"
import type { Route } from "@/lib/route"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import type { Actions, Row } from "./actions"
import { ProjectHead } from "./ProjectHead"
import { ProjectOverview } from "./ProjectOverview"
import { Notes } from "./Notes"
import { ProjectMemory } from "./ProjectMemory"
import { Live, SessionList } from "./SessionList"

function Empty({ title, text, icon: Icon = Search, children }: { title: string; text: string; icon?: typeof Search; children?: React.ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 py-20 text-center">
      <Icon className="size-7 text-muted-foreground/40" />
      <p className="font-medium">{title}</p>
      <p className="max-w-sm text-sm text-muted-foreground">{text}</p>
      {children}
    </div>
  )
}

// Corps des vues de sessions : squelette de chargement, « Récentes » par projet, liste, sous-dossiers, message vide.
export function SessionsPane({
  loading,
  route,
  rows,
  subs,
  byProject,
  qt,
  vg,
  dirMissing,
  projectDir,
  project,
  home,
  live,
  waiting,
  actions,
  go,
  clearSearch,
  grouped,
  error,
  denied,
  retry,
  onRefresh,
}: {
  loading: boolean
  error: string | null // dernier chargement en échec (serveur arrêté, HTTP 5xx)
  denied?: boolean // droit sessions_read absent sur ce home
  retry: () => void
  onRefresh: () => Promise<unknown> | void // tirer vers le bas en haut de liste (tactile)
  route: Route
  rows: Row[]
  subs: Row[] // sessions des sous-dossiers
  byProject: Map<string, Row[]> // vue « Récentes »
  qt: string
  vg: VGroup | undefined
  dirMissing: boolean // le dossier demandé n'existe pas dans l'arbre
  projectDir?: string // dossier de transcripts du projet affiché : sa mémoire locale s'affiche en tête
  project?: string // chemin du dossier affiché (route « dir ») : état git et nouveau prompt en tête
  home: string
  live: Set<string>
  waiting: Set<string>
  actions: Actions
  go: (r: Route) => void
  clearSearch: () => void
  grouped: { n: number; hide: boolean; setHide: (v: boolean) => void } // sessions déjà groupées du dossier affiché
}) {
  const { t } = useTranslation()
  // Tirer vers le bas depuis le haut de la liste : au-delà de 70 px, on recharge. Suivi en pixels pour l'indicateur.
  const [pull, setPull] = useState(0)
  const [refreshing, setRefreshing] = useState(false)
  const [startY, setStartY] = useState(-1)
  const viewport = (el: Element) => el.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]')
  const onTouchStart = (e: React.TouchEvent) => setStartY(e.touches.length === 1 && (viewport(e.currentTarget)?.scrollTop ?? 1) === 0 ? e.touches[0].clientY : -1)
  const onTouchMove = (e: React.TouchEvent) => {
    if (startY < 0 || refreshing) return
    setPull(Math.min(100, Math.max(0, (e.touches[0].clientY - startY) / 2)))
  }
  const onTouchEnd = async () => {
    const go = pull >= 70
    setPull(0)
    setStartY(-1)
    if (!go) return
    setRefreshing(true)
    try {
      await onRefresh()
    } finally {
      setRefreshing(false)
    }
  }
  return (
    <ScrollArea className="min-h-0 flex-1" onTouchStart={onTouchStart} onTouchMove={onTouchMove} onTouchEnd={onTouchEnd} onTouchCancel={() => (setPull(0), setStartY(-1))}>
      {(pull > 0 || refreshing) && (
        <div className="flex items-center justify-center overflow-hidden text-muted-foreground" style={{ height: refreshing ? 40 : pull }} aria-live="polite">
          <RefreshCcwDot className={"size-4 " + (refreshing ? "animate-spin" : pull >= 70 ? "text-primary" : "")} aria-label={t("shell.pane.pullRefresh")} />
        </div>
      )}
      <div className="mx-auto max-w-page px-3 py-4 pb-24 md:px-6">
        {loading && error && (
          <Empty icon={denied ? Lock : WifiOff} title={denied ? t("auth.denied.title") : t("shell.pane.unreachable")} text={error}>
            <Button variant="outline" size="sm" className="mt-2" onClick={retry}>
              {t("common.retry")}
            </Button>
          </Empty>
        )}
        {loading && !error && (
          <div className="space-y-3 px-3">
            {[0, 1, 2, 3, 4].map((i) => (
              <Skeleton key={i} className="h-12 w-full" />
            ))}
          </div>
        )}

        {!loading && route.kind === "dir" && project && <ProjectHead key={project} project={project} missing={dirMissing} />}
        {!loading && route.kind === "dir" && project && <Notes key={"notes:" + project} noteKey={"d/" + project} folder={dirMissing ? undefined : project} rows={rows} />}
        {!loading && route.kind === "project" && <ProjectHead key={"head-p:" + route.name} dirs={actions.projects.find((p) => p.name === route.name)?.dirs ?? []} />}
        {!loading && route.kind === "project" && <ProjectOverview key={"ov-p:" + route.name} dirs={actions.projects.find((p) => p.name === route.name)?.dirs ?? []} go={go} />}
        {!loading && route.kind === "project" && (
          <Notes key={"notes-p:" + route.name} noteKey={"p/" + route.name} folder={actions.projects.find((p) => p.name === route.name)?.dirs[0]} rows={rows} />
        )}

        {!loading && projectDir && (route.kind === "dir" || route.kind === "group") && <ProjectMemory key={projectDir} dir={projectDir} />}

        {!loading && route.kind === "recent" && (
          <div className="space-y-8">
            {[...byProject].map(([project, rs]) => (
              <section key={project}>
                <div className="mb-1 flex items-center gap-3 border-b px-3 py-1.5">
                  <button
                    className="min-w-0 truncate text-sm font-semibold text-primary underline underline-offset-2 hover:decoration-2"
                    onClick={() => go({ kind: "dir", key: segs(project, home).join("/") })}
                    title={t("shell.common.showFolder")}
                  >
                    {tilde(project, home)}
                  </button>
                  <span className="hidden shrink-0 text-xs text-muted-foreground sm:inline">{t("shell.count.sessions", { count: rs.length })}</span>
                  {rs.some((r) => live.has(r.s.id)) && <Live className="text-xs" waiting={rs.some((r) => waiting.has(r.s.id))} />}
                  {!rs[0].g.missing && (
                    <Button
                      variant="ghost"
                      size="xs"
                      className="ml-auto text-muted-foreground"
                      onClick={() => actions.open({ dir: rs[0].s.dir })}
                      title={t("shell.pane.resumeFolderHint")}
                    >
                      <RefreshCcwDot /> <span className="hidden lg:inline">{t("shell.pane.resumeFolder")}</span>
                    </Button>
                  )}
                </div>
                <SessionList items={rs} showProject={false} flat />
              </section>
            ))}
          </div>
        )}

        {!loading && route.kind === "dir" && grouped.n > 0 && (
          <label className="mb-2 flex w-fit cursor-pointer items-center gap-2 px-3 text-xs text-muted-foreground select-none hover:text-foreground">
            <Checkbox checked={grouped.hide} onCheckedChange={(c) => grouped.setHide(c === true)} />
            {t("shell.header.hideGrouped", { n: grouped.n })}
          </label>
        )}

        {!loading && route.kind !== "recent" && rows.length > 0 && (
          <SessionList items={rows} showProject={route.kind !== "dir" && route.kind !== "group"} showCache={route.kind === "active"} flat={route.kind === "active"} />
        )}

        {!loading && subs.length > 0 && (
          <details open={rows.length === 0} className="group/subs mt-8 border-t pt-4">
            <summary className="flex cursor-pointer list-none items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground hover:text-foreground">
              <ChevronRight className="size-4 transition-transform group-open/subs:rotate-90" />
              {t("shell.pane.subfolders")}
              <span className="text-xs font-normal tabular-nums">{subs.length}</span>
            </summary>
            <div className="mt-2">
              <SessionList items={subs} showProject />
            </div>
          </details>
        )}

        {!loading && rows.length === 0 && subs.length === 0 && (
          <Empty
            title={route.kind === "group" && !vg ? t("shell.pane.groupMissing") : route.kind === "dir" && dirMissing ? t("shell.pane.dirMissing") : t("shell.pane.noSession")}
            text={
              qt
                ? t("shell.pane.noMatch")
                : route.kind === "group"
                  ? t("shell.pane.emptyGroup")
                  : route.kind === "active"
                    ? t("shell.pane.noneActive")
                    : route.kind === "recent"
                      ? t("shell.pane.noneRecent")
                      : t("shell.pane.nothing")
            }
          >
            {qt && (
              <Button variant="outline" size="sm" className="mt-2" onClick={clearSearch}>
                {t("shell.sidebar.clearSearch")}
              </Button>
            )}
          </Empty>
        )}
      </div>
    </ScrollArea>
  )
}
