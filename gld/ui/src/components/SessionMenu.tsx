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
function Groups({ M: kit, rows }: { M: Kit; rows: Row[] }) {
  const { t } = useTranslation()
  const actions = useActions()
  const projects = new Set(rows.map((row) => row.g.project))
  if (projects.size > 1)
    return (
      <kit.Item disabled>
        <Tag /> {t("shell.menu.groupMixed")}
      </kit.Item>
    )
  const project = rows[0].g.project
  const ids = rows.map((row) => row.s.id)
  const projectGroups = actions.vgroups.filter((virtualGroup) => virtualGroup.project === project)
  return (
    <kit.Sub>
      <kit.SubTrigger>
        <Tag /> {t("shell.menu.group")}
      </kit.SubTrigger>
      <kit.SubContent className="w-56">
        {projectGroups.map((virtualGroup) => {
          const containsAll = ids.every((id) => virtualGroup.ids.includes(id))
          const toggleMembership = () => {
            if (containsAll) actions.ungroup(ids, virtualGroup)
            else actions.group(ids, project, virtualGroup.name)
          }
          return (
            <kit.Item key={virtualGroup.name} onSelect={toggleMembership}>
              <Check className={containsAll ? "" : "invisible"} /> <span className="truncate">{virtualGroup.name}</span>
            </kit.Item>
          )
        })}
        {projectGroups.length > 0 && <kit.Sep />}
        <kit.Item onSelect={() => actions.group(ids, project, null)}>
          <Plus /> {t("shell.menu.newGroup")}
        </kit.Item>
      </kit.SubContent>
    </kit.Sub>
  )
}

function Fav({ M: kit, ids }: { M: Kit; ids: string[] }) {
  const { t } = useTranslation()
  const actions = useActions()
  const allFavorite = ids.every((id) => actions.favs.has(id))
  return (
    <kit.Item onSelect={() => actions.toggleFav(ids)}>
      {allFavorite ? <StarOff /> : <Star />} {allFavorite ? t("shell.menu.unfav") : t("shell.menu.fav")}
    </kit.Item>
  )
}

// Entrées d'une session ; si elle fait partie d'une sélection multiple, elles s'appliquent à toute la sélection.
// full : entrées supplémentaires du clic droit (lire, reprendre, sélectionner), déjà visibles ailleurs sinon.
export function SessionItems({ M: kit, row, after, full }: { M: Kit; row: Row; after?: () => void; full?: boolean }) {
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
        <kit.Label>{t("shell.selection.count", { count: rows.length })}</kit.Label>
        <Fav M={kit} ids={ids} />
        <Groups M={kit} rows={rows} />
        {actions.inGroup && (
          <kit.Item onSelect={() => actions.ungroup(ids)}>
            <X /> {t("shell.menu.ungroup")}
          </kit.Item>
        )}
        <kit.Item onSelect={() => actions.move(rows)}>
          <FolderInput /> {t("shell.menu.move")}
        </kit.Item>
        {liveIds.length > 0 && (
          <kit.Item onSelect={() => actions.stop(liveIds)}>
            <Power /> {t("shell.menu.closeOpen")}
          </kit.Item>
        )}
        <kit.Sep />
        <kit.Item
          variant="destructive"
          onSelect={() =>
            actions.remove(
              rows.map((selectedRow) => ({ dir: selectedRow.s.dir, id: selectedRow.s.id })),
              t("shell.menu.removeSelected", { count: rows.length })
            )
          }
        >
          <Trash2 /> {t("shell.menu.deleteN", { sessions: t("shell.count.sessions", { count: rows.length }) })}
        </kit.Item>
      </>
    )
  }
  return (
    <>
      {full && (
        <kit.Item onSelect={() => actions.read(session)}>
          <BookOpen /> {t("shell.menu.read")}
        </kit.Item>
      )}
      {full && !group.missing && (
        <kit.Item onSelect={() => actions.open({ dir: session.dir, id: session.id })}>
          <RefreshCcwDot /> {t("shell.list.resume")}
        </kit.Item>
      )}
      {!group.missing && (
        <kit.Item onSelect={() => actions.resumeWith(row)}>
          <MessageSquareReply /> {t("shell.menu.resumeWith")}
        </kit.Item>
      )}
      {!group.missing && (
        <kit.Item onSelect={() => actions.newSession(group.project)}>
          <Plus /> {t("shell.menu.newInProject")}
        </kit.Item>
      )}
      <kit.Item onSelect={() => actions.info(row)}>
        <Info /> {t("shell.menu.info")}
      </kit.Item>
      <kit.Sep />
      <kit.Item onSelect={() => actions.copy(session)}>
        <Copy /> {t("shell.menu.copyId")}
      </kit.Item>
      <kit.Item onSelect={() => actions.exportMd(row)}>
        <Download /> {t("shell.menu.exportMd")}
      </kit.Item>
      <kit.Sep />
      <Fav M={kit} ids={[session.id]} />
      <kit.Item onSelect={() => actions.rename(session)}>
        <TextCursorInput /> {t("shell.menu.rename")}
      </kit.Item>
      <ShareItem M={kit} row={row} />
      <Groups M={kit} rows={[row]} />
      {actions.inGroup && (
        <kit.Item onSelect={() => actions.ungroup([session.id])}>
          <X /> {t("shell.menu.ungroup")}
        </kit.Item>
      )}
      {(full || touchOnly()) && (
        <kit.Item onSelect={() => actions.pick(session.id)}>
          <CheckSquare /> {actions.picked.has(session.id) ? t("shell.menu.deselect") : t("shell.menu.select")}
        </kit.Item>
      )}
      <kit.Item onSelect={() => actions.move([row])}>
        <FolderInput /> {t("shell.menu.move")}
      </kit.Item>
      {actions.live.has(session.id) && (
        <kit.Item onSelect={() => actions.stop([session.id])}>
          <Power /> {t("shell.menu.closeSession")}
        </kit.Item>
      )}
      <kit.Sep />
      <kit.Item variant="destructive" onSelect={() => actions.remove([{ dir: session.dir, id: session.id }], t("shell.menu.quoted", { title: session.title }), after)}>
        <Trash2 /> {t("common.delete")}
      </kit.Item>
    </>
  )
}

// Les entrées ne sont montées qu'à l'ouverture du menu : l'état du partage se lit alors.
export function ShareItem({ M: kit, row }: { M: Kit; row: Row }) {
  const { t } = useTranslation()
  const actions = useActions()
  const shared = !!useShares().shares?.find((share) => share.id === row.s.id)?.invites.length
  return (
    <kit.Item onSelect={() => actions.share(row)}>
      <Share2 /> {shared ? t("shell.menu.shared") : t("shell.menu.share")}
    </kit.Item>
  )
}

// Entrées d'un groupe virtuel (clic droit dans l'arbre, menu « ⋯ » de sa page).
export function GroupItems({ M: kit, vg, page }: { M: Kit; vg: VGroup; page?: boolean }) {
  const { t } = useTranslation()
  const actions = useActions()
  const sessionCount = actions.groupActs.rows(vg).length
  const groupActions = actions.groupActs
  return (
    <>
      <kit.Label className="truncate">{vg.name}</kit.Label>
      {!page && (
        <kit.Item onSelect={() => groupActions.show(vg)}>
          <BookOpen /> {t("shell.menu.show")}
        </kit.Item>
      )}
      <kit.Item onSelect={() => groupActions.open(vg)} disabled={!sessionCount}>
        <RefreshCcwDot /> {t("shell.menu.resumeAll", { sessions: t("shell.count.sessions", { count: sessionCount }) })}
      </kit.Item>
      <kit.Item onSelect={() => groupActions.prompt(vg)} disabled={!sessionCount}>
        <Plus /> {t("shell.menu.newWithRefs")}
      </kit.Item>
      <kit.Sep />
      <kit.Item onSelect={() => groupActions.refs(vg)} disabled={!sessionCount}>
        <Copy /> {t("shell.menu.copyRefs")}
      </kit.Item>
      <kit.Item onSelect={() => groupActions.export(vg)} disabled={!sessionCount}>
        <Download /> {t("shell.menu.exportMd")}
      </kit.Item>
      <kit.Sep />
      <kit.Item onSelect={() => groupActions.fav(vg)}>
        {vg.fav ? <StarOff /> : <Star />} {vg.fav ? t("shell.menu.unfav") : t("shell.menu.fav")}
      </kit.Item>
      <kit.Item onSelect={() => groupActions.rename(vg)}>
        <TextCursorInput /> {t("shell.menu.rename")}
      </kit.Item>
      <kit.Item onSelect={() => groupActions.select(vg)} disabled={!sessionCount}>
        <CheckSquare /> {t("shell.menu.selectSessions")}
      </kit.Item>
      <kit.Sep />
      <kit.Item variant="destructive" onSelect={() => groupActions.remove(vg)}>
        <Trash2 /> {t("shell.menu.deleteGroup")}
      </kit.Item>
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
