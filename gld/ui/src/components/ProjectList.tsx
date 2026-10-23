import { useTranslation } from "react-i18next"
import { setOpen, useOpenState } from "@/hooks/useOpen"
import { ChevronRight, Folder, Layers, Pencil, Trash2, X } from "lucide-react"
import type { Route } from "@/lib/route"
import { tilde } from "@/lib/format"
import { segs } from "@/lib/paths"
import { cn } from "@/lib/utils"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { useActions } from "./actions"
import { GitMini } from "./GitBadge"
import { ProjectItems } from "./ProjectTree"
import { useGit } from "@/hooks/useGit"

// Projets : ensembles nommés de dossiers de travail, affichés au-dessus de l'arbre des dossiers.
// Un clic sur un projet affiche les sessions de tous ses dossiers ; ses dossiers se déplient dessous.
export function ProjectList({ route, go }: { route: Route; go: (r: Route) => void }) {
  const { t } = useTranslation()
  const a = useActions()
  const git = useGit()
  const opened = useOpenState()
  const toggle = (name: string) => setOpen("p/" + name, !opened["p/" + name])
  return (
    <ul className="space-y-px">
      {a.projects.map((p) => {
        const active = route.kind === "project" && route.name === p.name
        const isOpen = !!opened["p/" + p.name]
        return (
          <li key={p.name}>
            <ContextMenu>
              <ContextMenuTrigger asChild>
                <div
                  className={cn(
                    "group relative flex h-8 items-center gap-1 rounded-md pr-2 pl-1 text-sm transition-colors",
                    active ? "bg-accent font-medium text-accent-foreground" : "text-foreground/80 hover:bg-muted hover:text-foreground"
                  )}
                >
                  <button
                    className={cn("relative z-10 grid size-5 shrink-0 place-items-center rounded-sm text-muted-foreground hover:text-foreground", !p.dirs.length && "invisible")}
                    onClick={() => toggle(p.name)}
                    aria-label={isOpen ? t("shell.tree.collapse") : t("shell.tree.expand")}
                    aria-expanded={isOpen}
                  >
                    <ChevronRight className={cn("size-3.5 transition-transform", isOpen && "rotate-90")} />
                  </button>
                  <button
                    className="flex min-w-0 flex-1 items-center gap-2 text-left outline-none after:absolute after:inset-0 after:rounded-md focus-visible:after:ring-2 focus-visible:after:ring-ring/50"
                    onClick={() => go({ kind: "project", name: p.name })}
                    aria-current={active}
                    title={p.name}
                  >
                    <Layers className="size-4 shrink-0 text-info" />
                    <span className="truncate">{p.name}</span>
                  </button>
                  <span className="text-xs text-muted-foreground tabular-nums">{p.dirs.length}</span>
                </div>
              </ContextMenuTrigger>
              <ContextMenuContent className="w-56">
                <ContextMenuItem onSelect={() => a.projectActs.rename(p)}>
                  <Pencil /> {t("shell.menu.rename")}
                </ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem variant="destructive" onSelect={() => a.projectActs.delete(p)}>
                  <Trash2 /> {t("shell.tree.deleteProject")}
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
            {isOpen && (
              <ul>
                {p.dirs.map((d) => {
                  const key = segs(d, a.home).join("/")
                  return (
                    <li key={d}>
                      <ContextMenu>
                        <ContextMenuTrigger asChild>
                          <button
                            onClick={() => go({ kind: "dir", key })}
                            title={d}
                            className={cn(
                              "flex h-8 w-full items-center gap-2 rounded-md pr-2 pl-9 text-left text-sm transition-colors",
                              route.kind === "dir" && route.key === key ? "bg-accent font-medium text-accent-foreground" : "text-foreground/80 hover:bg-muted hover:text-foreground"
                            )}
                          >
                            <Folder className="size-4 shrink-0 text-muted-foreground" />
                            <span className="min-w-0 flex-1 truncate">{tilde(d, a.home)}</span>
                            <GitMini states={[git.status[d]]} />
                          </button>
                        </ContextMenuTrigger>
                        {a.folder(d) ? (
                          <ProjectItems g={a.folder(d)!} />
                        ) : (
                          <ContextMenuContent className="w-56">
                            <ContextMenuItem onSelect={() => a.projectActs.remove(d, p.name)}>
                              <X /> {t("shell.tree.removeFromProject")}
                            </ContextMenuItem>
                          </ContextMenuContent>
                        )}
                      </ContextMenu>
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
