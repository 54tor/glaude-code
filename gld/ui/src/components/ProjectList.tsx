import { useTranslation } from "react-i18next"
import { setOpen, useOpenState } from "@/hooks/useOpen"
import { ChevronRight, Folder, Layers, Rocket, TextCursorInput, Trash2, X } from "lucide-react"
import type { Route } from "@/lib/route"
import { tilde } from "@/lib/format"
import { segs } from "@/lib/paths"
import { DIR_MIME } from "@/lib/dnd"
import { cn } from "@/lib/utils"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { useActions } from "./actions"
import { GitMini } from "./GitBadge"
import { GroupRow, ProjectItems } from "./ProjectTree"
import { useGit } from "@/hooks/useGit"

// Projets : ensembles nommés de dossiers de travail, affichés au-dessus de l'arbre des dossiers.
// Un clic sur un projet affiche les sessions de tous ses dossiers ; ses dossiers se déplient dessous.
export function ProjectList({ route, go, q: query = "" }: { route: Route; go: (route: Route) => void; q?: string }) {
  const { t } = useTranslation()
  const actions = useActions()
  const git = useGit()
  const opened = useOpenState()
  const openKey = (name: string) => "p/" + name
  const toggle = (name: string) => setOpen(openKey(name), !opened[openKey(name)])
  const visibleProjects = [...actions.projects]
    .sort((left, right) => left.name.localeCompare(right.name))
    .filter((project) => project.name.toLowerCase().includes(query))
  return (
    <ul className="space-y-px">
      {visibleProjects.map((project) => {
        const active = route.kind === "project" && route.name === project.name
        const isOpen = !!opened[openKey(project.name)]
        return (
          <li key={project.name}>
            <ContextMenu>
              <ContextMenuTrigger asChild>
                <div
                  className={cn(
                    "group relative flex h-7 items-center gap-1 rounded-md pr-2 pl-1 text-xs transition-colors",
                    active ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"
                  )}
                >
                  <button
                    className={cn("relative z-10 grid size-5 shrink-0 place-items-center rounded-sm text-muted-foreground hover:text-foreground", !project.dirs.length && "invisible")}
                    onClick={() => toggle(project.name)}
                    aria-label={isOpen ? t("shell.tree.collapse") : t("shell.tree.expand")}
                    aria-expanded={isOpen}
                  >
                    <ChevronRight className={cn("size-3.5 transition-transform", isOpen && "rotate-90")} />
                  </button>
                  <button
                    className="flex min-w-0 flex-1 items-center gap-2 text-left outline-none after:absolute after:inset-0 after:rounded-md focus-visible:after:ring-2 focus-visible:after:ring-ring/80"
                    onClick={() => go({ kind: "project", name: project.name })}
                    aria-current={active}
                    title={project.name}
                  >
                    <Layers className="size-4 shrink-0 text-info" />
                    <span className="truncate">{project.name}</span>
                  </button>
                  <span className="text-xs text-muted-foreground tabular-nums">{project.dirs.length}</span>
                </div>
              </ContextMenuTrigger>
              <ContextMenuContent className="w-56">
                {project.dirs.length > 0 && (
                  <ContextMenuItem onSelect={() => actions.projectActs.promptAll(project)}>
                    <Rocket /> {t("shell.tree.promptAll")}
                  </ContextMenuItem>
                )}
                {project.dirs.length > 0 && <ContextMenuSeparator />}
                <ContextMenuItem onSelect={() => actions.projectActs.rename(project)}>
                  <TextCursorInput /> {t("shell.menu.rename")}
                </ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem variant="destructive" onSelect={() => actions.projectActs.delete(project)}>
                  <Trash2 /> {t("shell.tree.deleteProject")}
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
            {isOpen && (
              <ul className="relative">
                <span aria-hidden className="pointer-events-none absolute inset-y-0 left-3.5 w-px bg-border" />
                {project.dirs.map((directory) => {
                  const key = segs(directory, actions.home).join("/")
                  const folderGroup = actions.folder(directory)
                  const isActiveDir = route.kind === "dir" && route.key === key
                  return (
                    <li key={directory}>
                      <ContextMenu>
                        <ContextMenuTrigger asChild>
                          <button
                            draggable
                            onDragStart={(event) => event.dataTransfer.setData(DIR_MIME, directory)}
                            onClick={() => go({ kind: "dir", key })}
                            title={directory}
                            className={cn(
                              "flex h-7 w-full items-center gap-2 rounded-md pr-2 pl-9 text-left text-xs transition-colors",
                              isActiveDir ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"
                            )}
                          >
                            <Folder className="size-4 shrink-0 text-muted-foreground" />
                            <span className="min-w-0 flex-1 truncate">{tilde(directory, actions.home)}</span>
                            <GitMini states={[git.status[directory]]} />
                          </button>
                        </ContextMenuTrigger>
                        {folderGroup ? (
                          <ProjectItems g={folderGroup} />
                        ) : (
                          <ContextMenuContent className="w-56">
                            <ContextMenuItem onSelect={() => actions.projectActs.remove(directory, project.name)}>
                              <X /> {t("shell.tree.removeFromProject")}
                            </ContextMenuItem>
                          </ContextMenuContent>
                        )}
                      </ContextMenu>
                      {actions.vgroups
                        .filter((viewGroup) => viewGroup.project === directory)
                        .map((viewGroup) => (
                          <ul key={viewGroup.name}>
                            <GroupRow
                              vg={viewGroup}
                              dirKey={key}
                              project={directory}
                              depth={2}
                              active={route.kind === "group" && route.key === key && route.name === viewGroup.name}
                              onGroup={(group, groupKey) => go({ kind: "group", name: group.name, key: groupKey })}
                            />
                          </ul>
                        ))}
                    </li>
                  )
                })}
              </ul>
            )}
          </li>
        )
      })}
    </ul>
  )
}
