import { BookOpen, Check, CheckSquare, Copy, Download, FolderInput, Pencil, Plus, SquareTerminal, Tag, Trash2, X } from "lucide-react"
import {
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
} from "@/components/ui/context-menu"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useActions, type Row } from "./actions"

// Les mêmes entrées servent au menu « ⋯ » et au clic droit : seules les briques shadcn changent.
export type Kit = {
  Item: typeof DropdownMenuItem | typeof ContextMenuItem
  Label: typeof DropdownMenuLabel | typeof ContextMenuLabel
  Sep: typeof DropdownMenuSeparator | typeof ContextMenuSeparator
  Sub: typeof DropdownMenuSub | typeof ContextMenuSub
  SubTrigger: typeof DropdownMenuSubTrigger | typeof ContextMenuSubTrigger
  SubContent: typeof DropdownMenuSubContent | typeof ContextMenuSubContent
}
export const DD: Kit = {
  Item: DropdownMenuItem,
  Label: DropdownMenuLabel,
  Sep: DropdownMenuSeparator,
  Sub: DropdownMenuSub,
  SubTrigger: DropdownMenuSubTrigger,
  SubContent: DropdownMenuSubContent,
}
export const CM: Kit = {
  Item: ContextMenuItem,
  Label: ContextMenuLabel,
  Sep: ContextMenuSeparator,
  Sub: ContextMenuSub,
  SubTrigger: ContextMenuSubTrigger,
  SubContent: ContextMenuSubContent,
}

const plural = (n: number) => `${n} session${n > 1 ? "s" : ""}`

// Sous-menu des groupes virtuels : un groupe coché contient déjà toutes les sessions visées.
function Groups({ M, ids }: { M: Kit; ids: string[] }) {
  const a = useActions()
  return (
    <M.Sub>
      <M.SubTrigger>
        <Tag /> Groupe
      </M.SubTrigger>
      <M.SubContent className="w-56">
        {a.vgroups.map((g) => {
          const all = ids.every((id) => g.ids.includes(id))
          return (
            <M.Item key={g.name} onSelect={() => (all ? a.ungroup(ids, g.name) : a.group(ids, g.name))}>
              <Check className={all ? "" : "invisible"} /> <span className="truncate">{g.name}</span>
            </M.Item>
          )
        })}
        {a.vgroups.length > 0 && <M.Sep />}
        <M.Item onSelect={() => a.group(ids, null)}>
          <Plus /> Nouveau groupe…
        </M.Item>
      </M.SubContent>
    </M.Sub>
  )
}

// Entrées d'une session ; si elle fait partie d'une sélection multiple, elles s'appliquent à toute la sélection.
// full : entrées supplémentaires du clic droit (lire, reprendre, sélectionner), déjà visibles ailleurs sinon.
export function SessionItems({ M, row, after, full }: { M: Kit; row: Row; after?: () => void; full?: boolean }) {
  const a = useActions()
  const { s, g } = row
  const many = a.picked.has(s.id) && a.selected.length > 1
  if (many) {
    const rows = a.selected
    const ids = rows.map((r) => r.s.id)
    return (
      <>
        <M.Label>{plural(rows.length)} sélectionnées</M.Label>
        <Groups M={M} ids={ids} />
        {a.inGroup && (
          <M.Item onSelect={() => a.ungroup(ids)}>
            <X /> Retirer du groupe
          </M.Item>
        )}
        <M.Item onSelect={() => a.move(rows)}>
          <FolderInput /> Déplacer vers un autre dossier…
        </M.Item>
        <M.Sep />
        <M.Item
          variant="destructive"
          onSelect={() =>
            a.remove(
              rows.map((r) => ({ dir: r.s.dir, id: r.s.id })),
              `les ${plural(rows.length)} sélectionnées`
            )
          }
        >
          <Trash2 /> Supprimer {plural(rows.length)}
        </M.Item>
      </>
    )
  }
  return (
    <>
      {full && (
        <M.Item onSelect={() => a.read(s)}>
          <BookOpen /> Lire la conversation
        </M.Item>
      )}
      {full && !g.missing && (
        <M.Item onSelect={() => a.open({ dir: s.dir, id: s.id })}>
          <SquareTerminal /> Reprendre dans un terminal
        </M.Item>
      )}
      {full && <M.Sep />}
      <M.Item onSelect={() => a.rename(s)}>
        <Pencil /> Renommer…
      </M.Item>
      <M.Item onSelect={() => a.copy(s)}>
        <Copy /> Copier « claude --resume »
      </M.Item>
      <M.Item onSelect={() => a.exportMd(row)}>
        <Download /> Exporter en Markdown
      </M.Item>
      <Groups M={M} ids={[s.id]} />
      {a.inGroup && (
        <M.Item onSelect={() => a.ungroup([s.id])}>
          <X /> Retirer du groupe
        </M.Item>
      )}
      {full && (
        <M.Item onSelect={() => a.pick(s.id)}>
          <CheckSquare /> {a.picked.has(s.id) ? "Désélectionner" : "Sélectionner"}
        </M.Item>
      )}
      <M.Item onSelect={() => a.move([row])}>
        <FolderInput /> Déplacer vers un autre dossier…
      </M.Item>
      <M.Sep />
      <M.Item variant="destructive" onSelect={() => a.remove([{ dir: s.dir, id: s.id }], `« ${s.title} »`, after)}>
        <Trash2 /> Supprimer
      </M.Item>
    </>
  )
}

// Menu « ⋯ » d'une session.
export function SessionDropdown({ row, after, children }: { row: Row; after?: () => void; children: React.ReactNode }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <SessionItems M={DD} row={row} after={after} />
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
