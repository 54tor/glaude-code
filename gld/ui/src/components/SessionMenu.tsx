import { BookOpen, Check, CheckSquare, ClipboardList, Copy, Download, FolderInput, MessageSquarePlus, Info, MessageSquareReply, Pencil, Plus, Power, Shredder, Star, StarOff, Tag, Trash2, X, RefreshCcwDot } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { VGroup } from "@/lib/api"
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

// Sous-menu des groupes du dossier de travail (un groupe ne mélange jamais plusieurs dossiers).
// Un groupe coché contient déjà toutes les sessions visées.
function Groups({ M, rows }: { M: Kit; rows: Row[] }) {
  const { t } = useTranslation()
  const a = useActions()
  const projects = new Set(rows.map((r) => r.g.project))
  if (projects.size > 1)
    return (
      <M.Item disabled>
        <Tag /> {t("shell.menu.groupMixed")}
      </M.Item>
    )
  const project = rows[0].g.project
  const ids = rows.map((r) => r.s.id)
  const mine = a.vgroups.filter((g) => g.project === project)
  return (
    <M.Sub>
      <M.SubTrigger>
        <Tag /> {t("shell.menu.group")}
      </M.SubTrigger>
      <M.SubContent className="w-56">
        {mine.map((g) => {
          const all = ids.every((id) => g.ids.includes(id))
          return (
            <M.Item key={g.name} onSelect={() => (all ? a.ungroup(ids, g) : a.group(ids, project, g.name))}>
              <Check className={all ? "" : "invisible"} /> <span className="truncate">{g.name}</span>
            </M.Item>
          )
        })}
        {mine.length > 0 && <M.Sep />}
        <M.Item onSelect={() => a.group(ids, project, null)}>
          <Plus /> {t("shell.menu.newGroup")}
        </M.Item>
      </M.SubContent>
    </M.Sub>
  )
}

function Fav({ M, ids }: { M: Kit; ids: string[] }) {
  const { t } = useTranslation()
  const a = useActions()
  const all = ids.every((id) => a.favs.has(id))
  return (
    <M.Item onSelect={() => a.toggleFav(ids)}>
      {all ? <StarOff /> : <Star />} {all ? t("shell.menu.unfav") : t("shell.menu.fav")}
    </M.Item>
  )
}

// Entrées d'une session ; si elle fait partie d'une sélection multiple, elles s'appliquent à toute la sélection.
// full : entrées supplémentaires du clic droit (lire, reprendre, sélectionner), déjà visibles ailleurs sinon.
export function SessionItems({ M, row, after, full }: { M: Kit; row: Row; after?: () => void; full?: boolean }) {
  const { t } = useTranslation()
  const a = useActions()
  const { s, g } = row
  const many = a.picked.has(s.id) && a.selected.length > 1
  if (many) {
    const rows = a.selected
    const ids = rows.map((r) => r.s.id)
    return (
      <>
        <M.Label>{t("shell.selection.count", { count: rows.length })}</M.Label>
        <Fav M={M} ids={ids} />
        <Groups M={M} rows={rows} />
        {a.inGroup && (
          <M.Item onSelect={() => a.ungroup(ids)}>
            <X /> {t("shell.menu.ungroup")}
          </M.Item>
        )}
        <M.Item onSelect={() => a.move(rows)}>
          <FolderInput /> {t("shell.menu.move")}
        </M.Item>
        {rows.some((r) => a.live.has(r.s.id)) && (
          <M.Item onSelect={() => a.stop(rows.filter((r) => a.live.has(r.s.id)).map((r) => r.s.id))}>
            <Power /> {t("shell.menu.closeOpen")}
          </M.Item>
        )}
        <M.Sep />
        <M.Item
          variant="destructive"
          onSelect={() =>
            a.remove(
              rows.map((r) => ({ dir: r.s.dir, id: r.s.id })),
              t("shell.menu.removeSelected", { count: rows.length })
            )
          }
        >
          <Shredder /> {t("shell.menu.deleteN", { sessions: t("shell.count.sessions", { count: rows.length }) })}
        </M.Item>
      </>
    )
  }
  return (
    <>
      {full && (
        <M.Item onSelect={() => a.read(s)}>
          <BookOpen /> {t("shell.menu.read")}
        </M.Item>
      )}
      {full && !g.missing && (
        <M.Item onSelect={() => a.open({ dir: s.dir, id: s.id })}>
          <RefreshCcwDot /> {t("shell.list.resume")}
        </M.Item>
      )}
      {!g.missing && (
        <M.Item onSelect={() => a.resumeWith(row)}>
          <MessageSquareReply /> {t("shell.menu.resumeWith")}
        </M.Item>
      )}
      {!g.missing && (
        <M.Item onSelect={() => a.newSession(g.project)}>
          <MessageSquarePlus /> {t("shell.menu.newInProject")}
        </M.Item>
      )}
      {full && <M.Sep />}
      <M.Item onSelect={() => a.info(row)}>
        <Info /> {t("shell.menu.info")}
      </M.Item>
      <Fav M={M} ids={[s.id]} />
      <M.Item onSelect={() => a.rename(s)}>
        <Pencil /> {t("shell.menu.rename")}
      </M.Item>
      <M.Item onSelect={() => a.copy(s)}>
        <Copy /> {t("shell.menu.copyId")}
      </M.Item>
      <M.Item onSelect={() => a.exportMd(row)}>
        <Download /> {t("shell.menu.exportMd")}
      </M.Item>
      <Groups M={M} rows={[row]} />
      {a.inGroup && (
        <M.Item onSelect={() => a.ungroup([s.id])}>
          <X /> {t("shell.menu.ungroup")}
        </M.Item>
      )}
      {full && (
        <M.Item onSelect={() => a.pick(s.id)}>
          <CheckSquare /> {a.picked.has(s.id) ? t("shell.menu.deselect") : t("shell.menu.select")}
        </M.Item>
      )}
      <M.Item onSelect={() => a.move([row])}>
        <FolderInput /> {t("shell.menu.move")}
      </M.Item>
      {a.live.has(s.id) && (
        <M.Item onSelect={() => a.stop([s.id])}>
          <Power /> {t("shell.menu.closeSession")}
        </M.Item>
      )}
      <M.Sep />
      <M.Item variant="destructive" onSelect={() => a.remove([{ dir: s.dir, id: s.id }], t("shell.menu.quoted", { title: s.title }), after)}>
        <Shredder /> {t("common.delete")}
      </M.Item>
    </>
  )
}

// Entrées d'un groupe virtuel (clic droit dans l'arbre, menu « ⋯ » de sa page).
export function GroupItems({ M, vg, page }: { M: Kit; vg: VGroup; page?: boolean }) {
  const { t } = useTranslation()
  const a = useActions()
  const n = a.groupActs.rows(vg).length
  const ga = a.groupActs
  return (
    <>
      <M.Label className="truncate">{vg.name}</M.Label>
      {!page && (
        <M.Item onSelect={() => ga.show(vg)}>
          <BookOpen /> {t("shell.menu.show")}
        </M.Item>
      )}
      <M.Item onSelect={() => ga.open(vg)} disabled={!n}>
        <RefreshCcwDot /> {t("shell.menu.resumeAll", { sessions: t("shell.count.sessions", { count: n }) })}
      </M.Item>
      <M.Item onSelect={() => ga.prompt(vg)} disabled={!n}>
        <MessageSquarePlus /> {t("shell.menu.newWithRefs")}
      </M.Item>
      <M.Item onSelect={() => ga.refs(vg)} disabled={!n}>
        <ClipboardList /> {t("shell.menu.copyRefs")}
      </M.Item>
      <M.Item onSelect={() => ga.export(vg)} disabled={!n}>
        <Download /> {t("shell.menu.exportMd")}
      </M.Item>
      <M.Item onSelect={() => ga.select(vg)} disabled={!n}>
        <CheckSquare /> {t("shell.menu.selectSessions")}
      </M.Item>
      <M.Sep />
      <M.Item onSelect={() => ga.fav(vg)}>
        {vg.fav ? <StarOff /> : <Star />} {vg.fav ? t("shell.menu.unfav") : t("shell.menu.fav")}
      </M.Item>
      <M.Item onSelect={() => ga.rename(vg)}>
        <Pencil /> {t("shell.menu.rename")}
      </M.Item>
      <M.Item variant="destructive" onSelect={() => ga.remove(vg)}>
        <Trash2 /> {t("shell.menu.deleteGroup")}
      </M.Item>
    </>
  )
}

// Menu « ⋯ » d'une session.
// extra : entrées propres à l'appelant, en tête du menu (commandes du lecteur repliées sur petit écran).
export function SessionDropdown({ row, after, extra, children }: { row: Row; after?: () => void; extra?: React.ReactNode; children: React.ReactNode }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        {extra}
        <SessionItems M={DD} row={row} after={after} />
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
