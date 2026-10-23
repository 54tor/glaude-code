import { setOpen, useOpen, useOpenState } from "@/hooks/useOpen"
import { Check, ChevronRight, FolderInput, FolderMinus, Layers, MessageSquarePlus, Plus, SquareTerminal, Star, Tag, Trash2, TriangleAlert } from "lucide-react"
import type { GitState, Group, Session, VGroup } from "@/lib/api"
import { tilde } from "@/lib/format"
import { CM, GroupItems } from "./SessionMenu"
import { cn } from "@/lib/utils"
import { sorted, type Node } from "@/lib/tree"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger, ContextMenuTrigger } from "@/components/ui/context-menu"
import { useActions } from "./actions"
import { GitMini, SessionCount } from "./GitBadge"
import { useGit } from "@/hooks/useGit"

// Clic droit sur un dossier de projet : actions sur ses propres sessions.
export function ProjectItems({ g, ss = g.sessions, extra }: { g: Group; ss?: Session[]; extra?: React.ReactNode }) {
  const a = useActions()
  const own = ss.map((s) => ({ dir: s.dir, id: s.id }))
  return (
    <ContextMenuContent className="w-64">
      {extra}
      {!g.missing && own.length > 0 && (
        <ContextMenuItem onSelect={() => a.open({ dir: own[0].dir })}>
          <SquareTerminal /> Reprendre les récentes
        </ContextMenuItem>
      )}
      {!g.missing && (
        <ContextMenuItem onSelect={() => a.newSession(g.project)}>
          <MessageSquarePlus /> Nouvelle session…
        </ContextMenuItem>
      )}
      <ContextMenuSub>
        <ContextMenuSubTrigger>
          <Layers /> Projet
        </ContextMenuSubTrigger>
        <ContextMenuSubContent className="w-56">
          {a.projects.map((p) => {
            const inIt = p.dirs.includes(g.project)
            return (
              <ContextMenuItem key={p.name} onSelect={() => (inIt ? a.projectActs.remove(g.project, p.name) : a.projectActs.add(g.project, p.name))}>
                <Check className={inIt ? "" : "invisible"} /> <span className="truncate">{p.name}</span>
              </ContextMenuItem>
            )
          })}
          {a.projects.length > 0 && <ContextMenuSeparator />}
          <ContextMenuItem onSelect={() => a.projectActs.add(g.project, null)}>
            <Plus /> Nouveau projet…
          </ContextMenuItem>
        </ContextMenuSubContent>
      </ContextMenuSub>
      {own.length > 0 && (
        <>
          <ContextMenuItem onSelect={() => a.moveGroup(g, own)}>
            <FolderInput /> Réassigner les {own.length} sessions…
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem variant="destructive" onSelect={() => a.remove(own, `les ${own.length} session(s) de ${g.project}`)}>
            <Trash2 /> Supprimer les {own.length} sessions
          </ContextMenuItem>
        </>
      )}
      {a.registeredDirs.includes(g.project) && (
        <>
          {own.length > 0 && <ContextMenuSeparator />}
          <ContextMenuItem onSelect={() => a.removeDir(g.project)}>
            <FolderMinus /> Retirer de la liste des dossiers
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
  onGroup: (vg: VGroup, key: string) => void
  isOpen: (n: Node) => boolean
  toggle: (k: string) => void
}

// Un groupe virtuel, affiché comme un sous-dossier de son projet (même retrait, place du chevron comprise).
function GroupRow({ vg, n, depth, active, onGroup }: { vg: VGroup; n: Node; depth: number; active: boolean; onGroup: Props["onGroup"] }) {
  const a = useActions()
  return (
    <li>
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <button
            onClick={() => onGroup(vg, n.key)}
            aria-current={active}
            title={`Groupe « ${vg.name} » de ${n.g?.project ?? n.name}`}
            className={cn(
              "flex h-8 w-full items-center gap-2 rounded-md pr-2 text-left text-sm transition-colors data-[state=open]:bg-muted",
              active ? "bg-accent font-medium text-accent-foreground" : "text-foreground/80 hover:bg-muted hover:text-foreground"
            )}
            style={{ paddingLeft: (depth + 1) * 12 + 4 + 24 }}
          >
            <Tag className="size-4 shrink-0 text-violet-500" />
            <span className="truncate">{vg.name}</span>
            {vg.fav && <Star className="size-3 shrink-0 fill-amber-400 text-amber-400" aria-label="favori" />}
            <span className="ml-auto text-xs text-muted-foreground tabular-nums">{a.groupActs.rows(vg).length}</span>
          </button>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-72">
          <GroupItems M={CM} vg={vg} />
        </ContextMenuContent>
      </ContextMenu>
    </li>
  )
}

// Les dépôts git sous un dossier de sessions (sessions dans ~/php, dépôts dans ~/php/8.1, ~/php/8.2…) : un second niveau replié,
// dont la ligne résume l'état (fichiers modifiés au total) ; cliquer l'ouvre.
function RepoRows({ repos, status, depth, home, id }: { repos: string[]; status: Record<string, GitState>; depth: number; home: string; id: string }) {
  const [open, flip] = useOpen("r/" + id)
  const pad = (depth + 1) * 12 + 4
  return (
    <li>
      <button
        onClick={flip}
        aria-expanded={open}
        title={`${repos.length} dépôts git dans ce dossier`}
        className="flex h-7 w-full items-center gap-1 rounded-md pr-2 text-left text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
        style={{ paddingLeft: pad }}
      >
        <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
        <span className="truncate">{repos.length} dépôts</span>
        <span className="ml-auto" />
        <GitMini states={repos.map((d) => status[d])} />
      </button>
      {open && (
        <ul>
          {repos.map((d) => (
            <li key={d} className="flex h-7 items-center gap-2 pr-2 text-xs text-foreground/80" style={{ paddingLeft: pad + 20 }} title={tilde(d, home)}>
              <span className="min-w-0 flex-1 truncate">{d.split("/").pop()}</span>
              <GitMini states={[status[d]]} />
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}

function Item({
  n,
  sel,
  onSel,
  selGroup,
  onGroup,
  depth,
  isOpen,
  toggle,
  label,
  leaf,
}: Props & { n: Node; depth: number; label?: string; leaf?: boolean }) {
  const a = useActions()
  const git = useGit()
  const groups = n.g ? a.vgroups.filter((vg) => vg.project === n.g!.project) : []
  const repos = n.g ? (git.repos[n.g.project] ?? []) : []
  const kids = (!leaf && n.kids.size > 0) || groups.length > 0 || repos.length > 0
  const open = isOpen(n)
  const active = sel === n.key
  const sessionsOpen = n.g ? n.ss.some((s) => a.live.has(s.id)) : false
  return (
    <li>
      <ContextMenu>
        <ContextMenuTrigger asChild disabled={!n.g}>
          <div
            className={cn(
              "group relative flex h-8 items-center gap-1 rounded-md pr-2 text-sm transition-colors",
              active ? "bg-accent font-medium text-accent-foreground" : "text-foreground/80 hover:bg-muted hover:text-foreground"
            )}
            style={{ paddingLeft: depth * 12 + 4 }}
          >
            <button
              className={cn(
                "relative z-10 grid size-5 shrink-0 place-items-center rounded text-muted-foreground hover:text-foreground",
                !kids && "invisible"
              )}
              onClick={() => toggle(n.key)}
              aria-label={open ? "Replier" : "Déplier"}
              aria-expanded={open}
              tabIndex={kids ? 0 : -1}
            >
              <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
            </button>
            <button
              className="flex min-w-0 flex-1 items-center gap-2 text-left outline-none after:absolute after:inset-0 after:rounded-md focus-visible:after:ring-2 focus-visible:after:ring-ring/50"
              onClick={() => onSel(n.key)}
              title={n.g?.project ?? label ?? n.name}
              aria-current={active}
            >
              <span className="truncate">{label ?? n.name}</span>
              {n.g?.missing && <TriangleAlert className="size-3.5 shrink-0 text-amber-500" aria-label="dossier introuvable" />}
            </button>
            {n.g && <GitMini states={[git.status[n.g.project], ...repos.map((d) => git.status[d])]} />}
            {n.g && <SessionCount n={n.ss.length} active={sessionsOpen} />}
          </div>
        </ContextMenuTrigger>
        {n.g && <ProjectItems g={n.g} ss={n.ss} />}
      </ContextMenu>
      {kids && open && (
        <ul>
          {repos.length > 0 && <RepoRows repos={repos} status={git.status} depth={depth} home={a.home} id={n.key} />}
          {groups.map((vg) => (
            <GroupRow key={vg.name} vg={vg} n={n} depth={depth} active={selGroup === n.key + "\0" + vg.name} onGroup={onGroup} />
          ))}
          {!leaf &&
            sorted(n).map((c) => (
              <Item
                key={c.key}
                n={c}
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
function lift(nodes: Node[]): { n: Node; label?: string; leaf?: boolean }[] {
  const top: { n: Node; label?: string; leaf?: boolean }[] = []
  while (nodes.length === 1 && nodes[0].kids.size > 0) {
    const n = nodes[0]
    if (n.g) top.push({ n, label: n.key.replace(/^\/\//, "/"), leaf: true })
    nodes = sorted(n)
  }
  return [...top, ...nodes.map((n) => ({ n }))]
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
  const isOpen = (n: Node) => opened["t/" + n.key] ?? (sel.startsWith(n.key + "/") || sel === n.key)
  const toggle = (k: string) => setOpen("t/" + k, !(opened["t/" + k] ?? (sel.startsWith(k + "/") || sel === k)))
  return (
    <ul className="space-y-px">
      {lift(nodes).map(({ n, label, leaf }) => (
        <Item
          key={n.key}
          n={n}
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
