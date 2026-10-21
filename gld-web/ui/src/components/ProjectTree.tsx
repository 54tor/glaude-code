import { useState } from "react"
import { ChevronRight, Folder, FolderInput, FolderOpen, SquareTerminal, Trash2, TriangleAlert } from "lucide-react"
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
  closed: Set<string>
  toggle: (k: string) => void
}

function Item({ n, sel, onSel, depth, closed, toggle }: Props & { n: Node; depth: number }) {
  const kids = n.kids.size > 0
  const open = !closed.has(n.key)
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
              title={n.g?.project ?? n.name}
              aria-current={active}
            >
              <Icon className={cn("size-4 shrink-0", n.g ? "text-muted-foreground" : "text-muted-foreground/50")} />
              <span className="truncate">{n.name}</span>
              {n.g?.missing && <TriangleAlert className="size-3.5 shrink-0 text-amber-500" aria-label="dossier introuvable" />}
            </button>
            {n.g && <span className="text-xs text-muted-foreground tabular-nums">{n.ss.length}</span>}
          </div>
        </ContextMenuTrigger>
        {n.g && <ProjectItems n={n} />}
      </ContextMenu>
      {kids && open && (
        <ul>
          {sorted(n).map((c) => (
            <Item key={c.key} n={c} sel={sel} onSel={onSel} depth={depth + 1} closed={closed} toggle={toggle} />
          ))}
        </ul>
      )}
    </li>
  )
}

// Arbre des dossiers de projet dans la barre latérale ; toute la ligne sélectionne le dossier, le chevron le replie.
export function ProjectTree({ nodes, sel, onSel }: { nodes: Node[]; sel: string; onSel: (key: string) => void }) {
  const [closed, setClosed] = useState(() => new Set<string>())
  const toggle = (k: string) =>
    setClosed((c) => {
      const n = new Set(c)
      if (n.has(k)) n.delete(k)
      else n.add(k)
      return n
    })
  return (
    <ul className="space-y-px">
      {nodes.map((n) => (
        <Item key={n.key} n={n} sel={sel} onSel={onSel} depth={0} closed={closed} toggle={toggle} />
      ))}
    </ul>
  )
}
