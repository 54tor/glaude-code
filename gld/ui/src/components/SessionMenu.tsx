import { BookOpen, Check, CheckSquare, Copy, Download, FolderInput, Info, MessageSquareReply, Plus, Power, Share2, Star, StarOff, Tag, TextCursorInput, Trash2, X, RefreshCcwDot } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { VGroup } from "@/lib/api"
import { useShares } from "@/hooks/useShares"
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
import { touchOnly } from "@/lib/utils"

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
  const actions = useActions()
  const projects = new Set(rows.map((row) => row.g.project))
  if (projects.size > 1)
    return (
      <M.Item disabled>
        <Tag /> {t("shell.menu.groupMixed")}
      </M.Item>
    )
  const project = rows[0].g.project
  const ids = rows.map((row) => row.s.id)
  const projectGroups = actions.vgroups.filter((virtualGroup) => virtualGroup.project === project)
  return (
    <M.Sub>
      <M.SubTrigger>
        <Tag /> {t("shell.menu.group")}
      </M.SubTrigger>
      <M.SubContent className="w-56">
        {projectGroups.map((virtualGroup) => {
          const containsAll = ids.every((id) => virtualGroup.ids.includes(id))
          const toggleMembership = () => {
            if (containsAll) actions.ungroup(ids, virtualGroup)
            else actions.group(ids, project, virtualGroup.name)
          }
          return (
            <M.Item key={virtualGroup.name} onSelect={toggleMembership}>
              <Check className={containsAll ? "" : "invisible"} /> <span className="truncate">{virtualGroup.name}</span>
            </M.Item>
          )
        })}
        {projectGroups.length > 0 && <M.Sep />}
        <M.Item onSelect={() => actions.group(ids, project, null)}>
          <Plus /> {t("shell.menu.newGroup")}
        </M.Item>
      </M.SubContent>
    </M.Sub>
  )
}

function Fav({ M, ids }: { M: Kit; ids: string[] }) {
  const { t } = useTranslation()
  const actions = useActions()
  const allFavorite = ids.every((id) => actions.favs.has(id))
  return (
    <M.Item onSelect={() => actions.toggleFav(ids)}>
      {allFavorite ? <StarOff /> : <Star />} {allFavorite ? t("shell.menu.unfav") : t("shell.menu.fav")}
    </M.Item>
  )
}

// Entrées d'une session ; si elle fait partie d'une sélection multiple, elles s'appliquent à toute la sélection.
// full : entrées supplémentaires du clic droit (lire, reprendre, sélectionner), déjà visibles ailleurs sinon.
export function SessionItems({ M, row, after, full }: { M: Kit; row: Row; after?: () => void; full?: boolean }) {
  const { t } = useTranslation()
  const actions = useActions()
  const { s: session, g: group } = row
  const appliesToSelection = actions.picked.has(session.id) && actions.selected.length > 1
  if (appliesToSelection) {
    const rows = actions.selected
    const ids = rows.map((selectedRow) => selectedRow.s.id)
    const liveIds = ids.filter((id) => actions.live.has(id))
    return (
      <>
        <M.Label>{t("shell.selection.count", { count: rows.length })}</M.Label>
        <Fav M={M} ids={ids} />
        <Groups M={M} rows={rows} />
        {actions.inGroup && (
          <M.Item onSelect={() => actions.ungroup(ids)}>
            <X /> {t("shell.menu.ungroup")}
          </M.Item>
        )}
        <M.Item onSelect={() => actions.move(rows)}>
          <FolderInput /> {t("shell.menu.move")}
        </M.Item>
        {liveIds.length > 0 && (
          <M.Item onSelect={() => actions.stop(liveIds)}>
            <Power /> {t("shell.menu.closeOpen")}
          </M.Item>
        )}
        <M.Sep />
        <M.Item
          variant="destructive"
          onSelect={() =>
            actions.remove(
              rows.map((selectedRow) => ({ dir: selectedRow.s.dir, id: selectedRow.s.id })),
              t("shell.menu.removeSelected", { count: rows.length })
            )
          }
        >
          <Trash2 /> {t("shell.menu.deleteN", { sessions: t("shell.count.sessions", { count: rows.length }) })}
        </M.Item>
      </>
    )
  }
  return (
    <>
      {full && (
        <M.Item onSelect={() => actions.read(session)}>
          <BookOpen /> {t("shell.menu.read")}
        </M.Item>
      )}
      {full && !group.missing && (
        <M.Item onSelect={() => actions.open({ dir: session.dir, id: session.id })}>
          <RefreshCcwDot /> {t("shell.list.resume")}
        </M.Item>
      )}
      {!group.missing && (
        <M.Item onSelect={() => actions.resumeWith(row)}>
          <MessageSquareReply /> {t("shell.menu.resumeWith")}
        </M.Item>
      )}
      {!group.missing && (
        <M.Item onSelect={() => actions.newSession(group.project)}>
          <Plus /> {t("shell.menu.newInProject")}
        </M.Item>
      )}
      <M.Item onSelect={() => actions.info(row)}>
        <Info /> {t("shell.menu.info")}
      </M.Item>
      <M.Sep />
      <M.Item onSelect={() => actions.copy(session)}>
        <Copy /> {t("shell.menu.copyId")}
      </M.Item>
      <M.Item onSelect={() => actions.exportMd(row)}>
        <Download /> {t("shell.menu.exportMd")}
      </M.Item>
      <M.Sep />
      <Fav M={M} ids={[session.id]} />
      <M.Item onSelect={() => actions.rename(session)}>
        <TextCursorInput /> {t("shell.menu.rename")}
      </M.Item>
      <ShareItem M={M} row={row} />
      <Groups M={M} rows={[row]} />
      {actions.inGroup && (
        <M.Item onSelect={() => actions.ungroup([session.id])}>
          <X /> {t("shell.menu.ungroup")}
        </M.Item>
      )}
      {(full || touchOnly()) && (
        <M.Item onSelect={() => actions.pick(session.id)}>
          <CheckSquare /> {actions.picked.has(session.id) ? t("shell.menu.deselect") : t("shell.menu.select")}
        </M.Item>
      )}
      <M.Item onSelect={() => actions.move([row])}>
        <FolderInput /> {t("shell.menu.move")}
      </M.Item>
      {actions.live.has(session.id) && (
        <M.Item onSelect={() => actions.stop([session.id])}>
          <Power /> {t("shell.menu.closeSession")}
        </M.Item>
      )}
      <M.Sep />
      <M.Item variant="destructive" onSelect={() => actions.remove([{ dir: session.dir, id: session.id }], t("shell.menu.quoted", { title: session.title }), after)}>
        <Trash2 /> {t("common.delete")}
      </M.Item>
    </>
  )
}

// Les entrées ne sont montées qu'à l'ouverture du menu : l'état du partage se lit alors.
export function ShareItem({ M, row }: { M: Kit; row: Row }) {
  const { t } = useTranslation()
  const actions = useActions()
  const shared = !!useShares().shares?.find((share) => share.id === row.s.id)?.invites.length
  return (
    <M.Item onSelect={() => actions.share(row)}>
      <Share2 /> {shared ? t("shell.menu.shared") : t("shell.menu.share")}
    </M.Item>
  )
}

// Entrées d'un groupe virtuel (clic droit dans l'arbre, menu « ⋯ » de sa page).
export function GroupItems({ M, vg, page }: { M: Kit; vg: VGroup; page?: boolean }) {
  const { t } = useTranslation()
  const actions = useActions()
  const sessionCount = actions.groupActs.rows(vg).length
  const groupActions = actions.groupActs
  return (
    <>
      <M.Label className="truncate">{vg.name}</M.Label>
      {!page && (
        <M.Item onSelect={() => groupActions.show(vg)}>
          <BookOpen /> {t("shell.menu.show")}
        </M.Item>
      )}
      <M.Item onSelect={() => groupActions.open(vg)} disabled={!sessionCount}>
        <RefreshCcwDot /> {t("shell.menu.resumeAll", { sessions: t("shell.count.sessions", { count: sessionCount }) })}
      </M.Item>
      <M.Item onSelect={() => groupActions.prompt(vg)} disabled={!sessionCount}>
        <Plus /> {t("shell.menu.newWithRefs")}
      </M.Item>
      <M.Sep />
      <M.Item onSelect={() => groupActions.refs(vg)} disabled={!sessionCount}>
        <Copy /> {t("shell.menu.copyRefs")}
      </M.Item>
      <M.Item onSelect={() => groupActions.export(vg)} disabled={!sessionCount}>
        <Download /> {t("shell.menu.exportMd")}
      </M.Item>
      <M.Sep />
      <M.Item onSelect={() => groupActions.fav(vg)}>
        {vg.fav ? <StarOff /> : <Star />} {vg.fav ? t("shell.menu.unfav") : t("shell.menu.fav")}
      </M.Item>
      <M.Item onSelect={() => groupActions.rename(vg)}>
        <TextCursorInput /> {t("shell.menu.rename")}
      </M.Item>
      <M.Item onSelect={() => groupActions.select(vg)} disabled={!sessionCount}>
        <CheckSquare /> {t("shell.menu.selectSessions")}
      </M.Item>
      <M.Sep />
      <M.Item variant="destructive" onSelect={() => groupActions.remove(vg)}>
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
