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

const PULL_REFRESH_THRESHOLD_PX = 70
const MAX_PULL_PX = 100
const REFRESHING_INDICATOR_HEIGHT_PX = 40
const SKELETON_ROW_COUNT = 5

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
  go: (localRoute: Route) => void
  clearSearch: () => void
  grouped: { n: number; hide: boolean; setHide: (flag: boolean) => void } // sessions déjà groupées du dossier affiché
}) {
  const { t } = useTranslation()
  // Tirer vers le bas depuis le haut de la liste : au-delà de 70 px, on recharge. Suivi en pixels pour l'indicateur.
  const [pull, setPull] = useState(0)
  const [refreshing, setRefreshing] = useState(false)
  const [startY, setStartY] = useState(-1)
  const viewport = (scrollArea: Element) => scrollArea.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]')
  const onTouchStart = (touchEvent: React.TouchEvent) => {
    const isSingleTouch = touchEvent.touches.length === 1
    const isAtTop = (viewport(touchEvent.currentTarget)?.scrollTop ?? 1) === 0
    setStartY(isSingleTouch && isAtTop ? touchEvent.touches[0].clientY : -1)
  }
  const onTouchMove = (touchEvent: React.TouchEvent) => {
    if (startY < 0 || refreshing) return
    const pullDistance = (touchEvent.touches[0].clientY - startY) / 2
    setPull(Math.min(MAX_PULL_PX, Math.max(0, pullDistance)))
  }
  const resetPull = () => {
    setPull(0)
    setStartY(-1)
  }
  const pulledFarEnough = pull >= PULL_REFRESH_THRESHOLD_PX
  const onTouchEnd = async () => {
    const shouldRefresh = pulledFarEnough
    resetPull()
    if (!shouldRefresh) return
    setRefreshing(true)
    try {
      await onRefresh()
    } finally {
      setRefreshing(false)
    }
  }
  const pullIndicatorClassName = () => {
    if (refreshing) return "animate-spin"
    if (pulledFarEnough) return "text-primary"
    return ""
  }
  const displayedProject = route.kind === "project" ? actions.projects.find((candidateProject) => candidateProject.name === route.name) : undefined
  const emptyTitle = () => {
    if (route.kind === "group" && !vg) return t("shell.pane.groupMissing")
    if (route.kind === "dir" && dirMissing) return t("shell.pane.dirMissing")
    return t("shell.pane.noSession")
  }
  const emptyText = () => {
    if (qt) return t("shell.pane.noMatch")
    if (route.kind === "group") return t("shell.pane.emptyGroup")
    if (route.kind === "active") return t("shell.pane.noneActive")
    if (route.kind === "recent") return t("shell.pane.noneRecent")
    return t("shell.pane.nothing")
  }
  return (
    <ScrollArea className="min-h-0 flex-1" onTouchStart={onTouchStart} onTouchMove={onTouchMove} onTouchEnd={onTouchEnd} onTouchCancel={resetPull}>
      {(pull > 0 || refreshing) && (
        <div className="flex items-center justify-center overflow-hidden text-muted-foreground" style={{ height: refreshing ? REFRESHING_INDICATOR_HEIGHT_PX : pull }} aria-live="polite">
          <RefreshCcwDot className={"size-4 " + pullIndicatorClassName()} aria-label={t("shell.pane.pullRefresh")} />
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
            {Array.from({ length: SKELETON_ROW_COUNT }, (_, skeletonIndex) => (
              <Skeleton key={skeletonIndex} className="h-12 w-full" />
            ))}
          </div>
        )}

        {!loading && route.kind === "dir" && project && <ProjectHead key={project} project={project} missing={dirMissing} />}
        {!loading && route.kind === "dir" && project && <Notes key={"notes:" + project} noteKey={"d/" + project} folder={dirMissing ? undefined : project} scope={[project]} rows={rows} />}
        {!loading && route.kind === "project" && <ProjectHead key={"head-p:" + route.name} dirs={displayedProject?.dirs ?? []} />}
        {!loading && route.kind === "project" && <ProjectOverview key={"ov-p:" + route.name} dirs={displayedProject?.dirs ?? []} go={go} />}
        {!loading && route.kind === "project" && (
          <Notes key={"notes-p:" + route.name} noteKey={"p/" + route.name} folder={displayedProject?.dirs[0]} scope={displayedProject?.dirs ?? []} rows={rows} />
        )}

        {!loading && projectDir && (route.kind === "dir" || route.kind === "group") && <ProjectMemory key={projectDir} dir={projectDir} />}

        {!loading && route.kind === "recent" && (
          <div className="space-y-8">
            {[...byProject].map(([project, projectRows]) => (
              <section key={project}>
                <div className="mb-1 flex items-center gap-3 border-b px-3 py-1.5">
                  <button
                    className="min-w-0 truncate text-sm font-semibold text-primary underline underline-offset-2 hover:decoration-2 pointer-coarse:min-h-8"
                    onClick={() => go({ kind: "dir", key: segs(project, home).join("/") })}
                    title={t("shell.common.showFolder")}
                  >
                    {tilde(project, home)}
                  </button>
                  <span className="hidden shrink-0 text-xs text-muted-foreground sm:inline">{t("shell.count.sessions", { count: projectRows.length })}</span>
                  {projectRows.some((projectRow) => live.has(projectRow.s.id)) && <Live className="text-xs" waiting={projectRows.some((projectRow) => waiting.has(projectRow.s.id))} />}
                  {!projectRows[0].g.missing && (
                    <Button
                      variant="ghost"
                      size="xs"
                      className="ml-auto text-muted-foreground"
                      onClick={() => actions.open({ dir: projectRows[0].s.dir })}
                      title={t("shell.pane.resumeFolderHint")}
                    >
                      <RefreshCcwDot /> <span className="hidden lg:inline">{t("shell.pane.resumeFolder")}</span>
                    </Button>
                  )}
                </div>
                <SessionList items={projectRows} showProject={false} flat />
              </section>
            ))}
          </div>
        )}

        {!loading && route.kind === "dir" && grouped.n > 0 && (
          <label className="mb-2 flex w-fit cursor-pointer items-center gap-2 px-3 text-xs text-muted-foreground select-none hover:text-foreground">
            <Checkbox checked={grouped.hide} onCheckedChange={(checkedState) => grouped.setHide(checkedState === true)} />
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
            title={emptyTitle()}
            text={emptyText()}
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
