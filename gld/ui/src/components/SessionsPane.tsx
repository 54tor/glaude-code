import { ChevronRight, Search, SquareTerminal } from "lucide-react"
import type { VGroup } from "@/lib/api"
import { tilde } from "@/lib/format"
import { segs } from "@/lib/paths"
import type { Route } from "@/lib/route"
import { plural } from "@/lib/sessionsView"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import type { Actions, Row } from "./actions"
import { ProjectHead } from "./ProjectHead"
import { Notes } from "./Notes"
import { ProjectMemory } from "./ProjectMemory"
import { Live, SessionList } from "./SessionList"

function Empty({ title, text, children }: { title: string; text: string; children?: React.ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 py-20 text-center">
      <Search className="size-8 text-muted-foreground/50" />
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
}: {
  loading: boolean
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
}) {
  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="mx-auto max-w-7xl px-3 py-4 pb-24 md:px-6">
        {loading && (
          <div className="space-y-3 px-3">
            {[0, 1, 2, 3, 4].map((i) => (
              <Skeleton key={i} className="h-12 w-full" />
            ))}
          </div>
        )}

        {!loading && route.kind === "dir" && project && <ProjectHead key={project} project={project} missing={dirMissing} />}
        {!loading && route.kind === "dir" && project && <Notes key={"notes:" + project} noteKey={"d/" + project} folder={dirMissing ? undefined : project} rows={rows} />}
        {!loading && route.kind === "project" && <ProjectHead key={"head-p:" + route.name} dirs={actions.projects.find((p) => p.name === route.name)?.dirs ?? []} />}
        {!loading && route.kind === "project" && (
          <Notes key={"notes-p:" + route.name} noteKey={"p/" + route.name} folder={actions.projects.find((p) => p.name === route.name)?.dirs[0]} rows={rows} />
        )}

        {!loading && projectDir && (route.kind === "dir" || route.kind === "group") && <ProjectMemory key={projectDir} dir={projectDir} />}

        {!loading && route.kind === "recent" && (
          <div className="space-y-6">
            {[...byProject].map(([project, rs]) => (
              <section key={project}>
                <div className="flex items-center gap-2 px-3 py-1.5">
                  <button
                    className="min-w-0 truncate text-sm font-medium hover:underline"
                    onClick={() => go({ kind: "dir", key: segs(project, home).join("/") })}
                    title="Afficher le dossier"
                  >
                    {tilde(project, home)}
                  </button>
                  <span className="shrink-0 text-xs text-muted-foreground">{plural(rs.length)}</span>
                  {rs.some((r) => live.has(r.s.id)) && <Live className="text-xs" waiting={rs.some((r) => waiting.has(r.s.id))} />}
                  {!rs[0].g.missing && (
                    <Button
                      variant="ghost"
                      size="xs"
                      className="ml-auto text-muted-foreground"
                      onClick={() => actions.open({ dir: rs[0].s.dir })}
                      title="Un onglet de terminal par session des 7 derniers jours (hors sessions ouvertes)"
                    >
                      <SquareTerminal /> Reprendre le dossier
                    </Button>
                  )}
                </div>
                <SessionList items={rs} showProject={false} flat />
              </section>
            ))}
          </div>
        )}

        {!loading && route.kind !== "recent" && rows.length > 0 && (
          <SessionList items={rows} showProject={route.kind !== "dir" && route.kind !== "group"} showCache={route.kind === "active"} flat={route.kind === "active"} />
        )}

        {!loading && subs.length > 0 && (
          <details open={rows.length === 0} className="group/subs mt-8 border-t pt-4">
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

        {!loading && rows.length === 0 && subs.length === 0 && (
          <Empty
            title={route.kind === "group" && !vg ? "Groupe introuvable" : route.kind === "dir" && dirMissing ? "Dossier introuvable" : "Aucune session"}
            text={
              qt
                ? "Aucun titre, projet ni message ne correspond à cette recherche."
                : route.kind === "group"
                  ? "Ajoute des sessions à ce groupe : clic droit sur une session › Groupe."
                  : route.kind === "active"
                    ? "Aucune session ouverte ni active depuis moins d'une heure."
                    : route.kind === "recent"
                      ? "Aucune session ces 7 derniers jours."
                      : "Rien à afficher ici."
            }
          >
            {qt && (
              <Button variant="outline" size="sm" className="mt-2" onClick={clearSearch}>
                Effacer la recherche
              </Button>
            )}
          </Empty>
        )}
      </div>
    </ScrollArea>
  )
}
