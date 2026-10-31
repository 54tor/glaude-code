import { useTranslation } from "react-i18next"
import { setOpen, useOpen, useOpenState } from "@/hooks/useOpen"
import { Check, ChevronRight, FolderInput, FolderMinus, Layers, Plus, Power, Star, Tag, Trash2, TriangleAlert, RefreshCcwDot } from "lucide-react"
import type { GitState, Group, Session, VGroup } from "@/lib/api"
import { tilde } from "@/lib/format"
import { CM, GroupItems } from "./SessionMenu"
import { cn } from "@/lib/utils"
import { sorted, type Node } from "@/lib/tree"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger, ContextMenuTrigger } from "@/components/ui/context-menu"
import { useActions } from "./actions"
import { GitMini, SessionCount } from "./GitBadge"
import { NoteMark } from "./NoteMark"
import { useGit } from "@/hooks/useGit"

const INDENT_PER_LEVEL = 12
const ROW_BASE_PADDING = 4
const CHEVRON_WIDTH = 24
const REPO_EXTRA_INDENT = 20
const GUIDE_LINE_OFFSET = 10

const rowPadding = (depth: number) => depth * INDENT_PER_LEVEL + ROW_BASE_PADDING

// Clic droit sur un dossier de projet : actions sur ses propres sessions.
export function ProjectItems({ g: group, ss: sessions = group.sessions, extra }: { g: Group; ss?: Session[]; extra?: React.ReactNode }) {
  const { t } = useTranslation()
  const actions = useActions()
  const own = sessions.map((session) => ({ dir: session.dir, id: session.id }))
  const live = own.filter((ownSession) => actions.live.has(ownSession.id)).map((ownSession) => ownSession.id)
  return (
    <ContextMenuContent className="w-64">
      {extra}
      {!group.missing && own.length > 0 && (
        <ContextMenuItem onSelect={() => actions.open({ dir: own[0].dir })}>
          <RefreshCcwDot /> {t("shell.tree.resumeRecent")}
        </ContextMenuItem>
      )}
      {!group.missing && (
        <ContextMenuItem onSelect={() => actions.newSession(group.project)}>
          <Plus /> {t("shell.tree.newSession")}
        </ContextMenuItem>
      )}
      {!group.missing && <ContextMenuSeparator />}
      <ContextMenuSub>
        <ContextMenuSubTrigger>
          <Layers /> {t("shell.tree.project")}
        </ContextMenuSubTrigger>
        <ContextMenuSubContent className="w-56">
          {actions.projects.map((project) => {
            const inIt = project.dirs.includes(group.project)
            const toggleMembership = () => {
              if (inIt) return actions.projectActs.remove(group.project, project.name)
              return actions.projectActs.add(group.project, project.name)
            }
            return (
              <ContextMenuItem key={project.name} onSelect={toggleMembership}>
                <Check className={inIt ? "" : "invisible"} /> <span className="truncate">{project.name}</span>
              </ContextMenuItem>
            )
          })}
          {actions.projects.length > 0 && <ContextMenuSeparator />}
          <ContextMenuItem onSelect={() => actions.projectActs.add(group.project, null)}>
            <Plus /> {t("shell.tree.newProject")}
          </ContextMenuItem>
        </ContextMenuSubContent>
      </ContextMenuSub>
      {own.length > 0 && (
        <>
          <ContextMenuItem onSelect={() => actions.moveGroup(group, own)}>
            <FolderInput /> {t("shell.tree.reassign", { n: own.length })}
          </ContextMenuItem>
          {live.length > 0 && (
            <ContextMenuItem onSelect={() => actions.stop(live)}>
              <Power /> {t("shell.tree.closeOpen", { n: live.length })}
            </ContextMenuItem>
          )}
          <ContextMenuSeparator />
          <ContextMenuItem variant="destructive" onSelect={() => actions.remove(own, t("shell.tree.removeLabel", { n: own.length, project: group.project }))}>
            <Trash2 /> {t("shell.tree.deleteAll", { n: own.length })}
          </ContextMenuItem>
        </>
      )}
      {actions.registeredDirs.includes(group.project) && (
        <>
          {own.length > 0 && <ContextMenuSeparator />}
          <ContextMenuItem onSelect={() => actions.removeDir(group.project)}>
            <FolderMinus /> {t("shell.tree.removeDir")}
          </ContextMenuItem>
        </>
      )}
    </ContextMenuContent>
  )
}

type Props = {
  sel: string
  onSel: (key: string) => void
  selGroup: string // « clé du dossier \0 nom » du groupe affiché
  onGroup: (viewGroup: VGroup, key: string) => void
  isOpen: (node: Node) => boolean
  toggle: (key: string) => void
}

// Un groupe virtuel, affiché comme un sous-dossier de son projet (même retrait, place du chevron comprise).
export function GroupRow({ vg: viewGroup, dirKey, project, depth, active, onGroup }: { vg: VGroup; dirKey: string; project: string; depth: number; active: boolean; onGroup: Props["onGroup"] }) {
  const { t } = useTranslation()
  const actions = useActions()
  return (
    <li>
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <button
            onClick={() => onGroup(viewGroup, dirKey)}
            aria-current={active}
            title={t("shell.tree.groupTitle", { name: viewGroup.name, project })}
            className={cn(
              "flex h-7 w-full items-center gap-2 rounded-md pr-2 text-left text-xs transition-colors data-[state=open]:bg-muted",
              active ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"
            )}
            style={{ paddingLeft: rowPadding(depth + 1) + CHEVRON_WIDTH }}
          >
            <Tag className="size-4 shrink-0 text-info" />
            <span className="truncate">{viewGroup.name}</span>
            {viewGroup.fav && <Star className="size-3 shrink-0 fill-amber-400 text-amber-400" aria-label={t("shell.list.favorite")} />}
            <span className="ml-auto text-xs text-muted-foreground tabular-nums">{actions.groupActs.rows(viewGroup).length}</span>
          </button>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-72">
          <GroupItems M={CM} vg={viewGroup} />
        </ContextMenuContent>
      </ContextMenu>
    </li>
  )
}

// Les dépôts git sous un dossier de sessions (sessions dans ~/php, dépôts dans ~/php/8.1, ~/php/8.2…) : un second niveau replié,
// dont la ligne résume l'état (fichiers modifiés au total) ; cliquer l'ouvre.
function RepoRows({ repos, status, depth, home, id }: { repos: string[]; status: Record<string, GitState>; depth: number; home: string; id: string }) {
  const { t } = useTranslation()
  const [open, flip] = useOpen("r/" + id)
  const pad = rowPadding(depth + 1)
  return (
    <li>
      <button
        onClick={flip}
        aria-expanded={open}
        title={t("shell.tree.reposHint", { n: repos.length })}
        className="flex h-7 w-full items-center gap-1 rounded-md pr-2 text-left text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
        style={{ paddingLeft: pad }}
      >
        <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
        <span className="truncate">{t("shell.tree.repos", { n: repos.length })}</span>
        <span className="ml-auto" />
        <GitMini states={repos.map((repoDir) => status[repoDir])} />
      </button>
      {open && (
        <ul>
          {repos.map((repoDir) => (
            <li key={repoDir} className="flex h-7 items-center gap-2 pr-2 text-xs text-foreground/80" style={{ paddingLeft: pad + REPO_EXTRA_INDENT }} title={tilde(repoDir, home)}>
              <span className="min-w-0 flex-1 truncate">{repoDir.split("/").pop()}</span>
              <GitMini states={[status[repoDir]]} />
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}

function Item({
  node,
  sel,
  onSel,
  selGroup,
  onGroup,
  depth,
  isOpen,
  toggle,
  label,
  leaf,
}: Props & { node: Node; depth: number; label?: string; leaf?: boolean }) {
  const { t } = useTranslation()
  const actions = useActions()
  const git = useGit()
  const folder = node.g
  const groups = folder
    ? actions.vgroups.filter((viewGroup) => viewGroup.project === folder.project).sort((left, right) => left.name.localeCompare(right.name))
    : []
  const repos = folder ? (git.repos[folder.project] ?? []) : []
  const kids = (!leaf && node.kids.size > 0) || groups.length > 0 || repos.length > 0
  const open = isOpen(node)
  const active = sel === node.key
  const sessionsOpen = folder ? node.ss.some((session) => actions.live.has(session.id)) : false
  const selectNode = () => {
    if (kids && !open) setOpen("t/" + node.key, true)
    onSel(node.key)
  }
  return (
    <li>
      <ContextMenu>
        <ContextMenuTrigger asChild disabled={!folder}>
          <div
            className={cn(
              "group relative flex h-7 items-center gap-1 rounded-md pr-2 text-xs transition-colors",
              active ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"
            )}
            style={{ paddingLeft: rowPadding(depth) }}
          >
            <button
              className={cn(
                "relative z-10 grid size-5 shrink-0 place-items-center rounded-sm text-muted-foreground hover:text-foreground",
                !kids && "invisible"
              )}
              onClick={() => toggle(node.key)}
              aria-label={open ? t("shell.tree.collapse") : t("shell.tree.expand")}
              aria-expanded={open}
              tabIndex={kids ? 0 : -1}
            >
              <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
            </button>
            <button
              className="flex min-w-0 flex-1 items-center gap-2 text-left outline-none after:absolute after:inset-0 after:rounded-md focus-visible:after:ring-2 focus-visible:after:ring-ring/80"
              onClick={selectNode}
              title={folder?.project ?? label ?? node.name}
              aria-current={active}
            >
              <span className="truncate">{label ?? node.name}</span>
              {folder?.missing && <TriangleAlert className="size-3.5 shrink-0 text-warning" aria-label={t("shell.tree.dirMissing")} />}
            </button>
            {folder && <NoteMark noteKey={"d/" + folder.project} />}
            {folder && <GitMini slot states={[git.status[folder.project], ...repos.map((repoDir) => git.status[repoDir])]} />}
            {folder && <SessionCount n={node.ss.length} active={sessionsOpen} />}
          </div>
        </ContextMenuTrigger>
        {folder && <ProjectItems g={folder} ss={node.ss} />}
      </ContextMenu>
      {kids && open && (
        <ul className="relative">
          <span aria-hidden className="pointer-events-none absolute inset-y-0 w-px bg-border" style={{ left: rowPadding(depth) + GUIDE_LINE_OFFSET }} />
          {repos.length > 0 && <RepoRows repos={repos} status={git.status} depth={depth} home={actions.home} id={node.key} />}
          {groups.map((viewGroup) => (
            <GroupRow key={viewGroup.name} vg={viewGroup} dirKey={node.key} project={folder?.project ?? node.name} depth={depth} active={selGroup === node.key + "\0" + viewGroup.name} onGroup={onGroup} />
          ))}
          {!leaf &&
            sorted(node).map((child) => (
              <Item
                key={child.key}
                node={child}
                sel={sel}
                onSel={onSel}
                selGroup={selGroup}
                onGroup={onGroup}
                depth={depth + 1}
                isOpen={isOpen}
                toggle={toggle}
              />
            ))}
        </ul>
      )}
    </li>
  )
}

// Arbre des dossiers de projet dans la barre latérale ; toute la ligne sélectionne le dossier, le chevron le replie.
// Les racines communes (~, ~/Projets…) ne servent qu'à indenter : leurs sous-dossiers remontent au premier niveau,
// et elles-mêmes, quand elles ont des sessions, restent des entrées simples, sous leur chemin complet.
type LiftedNode = { node: Node; label?: string; leaf?: boolean }

function lift(nodes: Node[]): LiftedNode[] {
  const top: LiftedNode[] = []
  while (nodes.length === 1 && nodes[0].kids.size > 0) {
    const singleRoot = nodes[0]
    if (singleRoot.g) top.push({ node: singleRoot, label: singleRoot.key.replace(/^\/\//, "/"), leaf: true })
    nodes = sorted(singleRoot)
  }
  return [...top, ...nodes.map((node) => ({ node }))]
}


export function ProjectTree({
  nodes,
  sel,
  onSel,
  selGroup,
  onGroup,
}: {
  nodes: Node[]
  sel: string
  onSel: (key: string) => void
  selGroup: string
  onGroup: Props["onGroup"]
}) {
  // Chargement paresseux : un grand arbre démarre replié (premier niveau seul, plus le chemin du dossier choisi) ;
  // les sous-dossiers d'un nœud replié ne sont pas rendus. Un petit arbre reste déplié. « toggled » : nœuds que
  // l'utilisateur a inversés par rapport à ce défaut.
  const opened = useOpenState()
  // Fermé par défaut, sauf le chemin du dossier affiché ; les choix de l'utilisateur sont gardés dans le navigateur.
  const isOnSelectedPath = (key: string) => sel.startsWith(key + "/") || sel === key
  const isKeyOpen = (key: string) => opened["t/" + key] ?? isOnSelectedPath(key)
  const isOpen = (node: Node) => isKeyOpen(node.key)
  const toggle = (key: string) => setOpen("t/" + key, !isKeyOpen(key))
  return (
    <ul className="space-y-px">
      {lift(nodes).map(({ node, label, leaf }) => (
        <Item
          key={node.key}
          node={node}
          label={label}
          leaf={leaf}
          sel={sel}
          onSel={onSel}
          selGroup={selGroup}
          onGroup={onGroup}
          depth={0}
          isOpen={isOpen}
          toggle={toggle}
        />
      ))}
    </ul>
  )
}
