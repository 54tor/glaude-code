import { useMemo, useState } from "react"
import { ChevronRight, Folder, FolderInput, FolderOpen, MessageSquarePlus, SquareTerminal, Star, Tag, Trash2, TriangleAlert } from "lucide-react"
import type { VGroup } from "@/lib/api"
import { CM, GroupItems } from "./SessionMenu"
import { cn } from "@/lib/utils"
import { sorted, type Node } from "@/lib/tree"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { useActions } from "./actions"

// Clic droit sur un dossier de projet : actions sur ses propres sessions.
function ProjectItems({ n }: { n: Node }) {
  const a = useActions()
  const g = n.g!
  const own = n.ss.map((s) => ({ dir: s.dir, id: s.id }))
  return (
    <ContextMenuContent className="w-64">
      {!g.missing && (
        <ContextMenuItem onSelect={() => a.open({ dir: n.ss[0].dir })}>
          <SquareTerminal /> Reprendre les récentes
        </ContextMenuItem>
      )}
      {!g.missing && (
        <ContextMenuItem onSelect={() => a.newSession(g.project)}>
          <MessageSquarePlus /> Nouvelle session…
        </ContextMenuItem>
      )}
      <ContextMenuItem onSelect={() => a.moveGroup(g, own)}>
        <FolderInput /> Réassigner les {own.length} sessions…
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem variant="destructive" onSelect={() => a.remove(own, `les ${own.length} session(s) de ${g.project}`)}>
        <Trash2 /> Supprimer les {own.length} sessions
      </ContextMenuItem>
    </ContextMenuContent>
  )
}

type Props = {
  sel: string
  onSel: (key: string) => void
  selGroup: string // « clé du dossier \0 nom » du groupe affiché
  onGroup: (vg: VGroup, key: string) => void
  isOpen: (n: Node, depth: number) => boolean
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
  const groups = n.g ? a.vgroups.filter((vg) => vg.project === n.g!.project) : []
  const kids = (!leaf && n.kids.size > 0) || groups.length > 0
  const open = isOpen(n, depth)
  const active = sel === n.key
  const Icon = active ? FolderOpen : Folder
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
              <Icon className={cn("size-4 shrink-0", n.g ? "text-muted-foreground" : "text-muted-foreground/50")} />
              <span className="truncate">{label ?? n.name}</span>
              {n.g?.missing && <TriangleAlert className="size-3.5 shrink-0 text-amber-500" aria-label="dossier introuvable" />}
            </button>
            {n.g && <span className="text-xs text-muted-foreground tabular-nums">{n.ss.length}</span>}
          </div>
        </ContextMenuTrigger>
        {n.g && <ProjectItems n={n} />}
      </ContextMenu>
      {kids && open && (
        <ul>
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

const BIG_TREE = 80 // au-delà de ce nombre de dossiers, l'arbre démarre replié
const count = (nodes: Node[]): number => nodes.reduce((t, n) => t + 1 + count([...n.kids.values()]), 0)

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
  const [toggled, setToggled] = useState(() => new Set<string>())
  const big = useMemo(() => count(nodes) > BIG_TREE, [nodes])
  const isOpen = (n: Node, depth: number) => {
    const byDefault = !big || depth === 0 || sel.startsWith(n.key + "/") || sel === n.key
    return toggled.has(n.key) ? !byDefault : byDefault
  }
  const toggle = (k: string) =>
    setToggled((c) => {
      const n = new Set(c)
      if (n.has(k)) n.delete(k)
      else n.add(k)
      return n
    })
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
