import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { GitBranch } from "lucide-react"
import type { GitState } from "@/lib/api"
import { useGit } from "@/hooks/useGit"
import { type CmdCtx, type EditorHandle, type EditorStatus } from "./commands"
import { isDirty } from "./ideState"
import type { IndexState } from "./useIndex"
import type { WatchState } from "./useWatch"

// Statut rattaché à la poignée qui l'a émis : après un changement d'onglet, rien de l'ancien éditeur ne s'affiche.
export function useEditorStatus(editor: EditorHandle | null) {
  const [emitted, setEmitted] = useState<{ handle: EditorHandle; status: EditorStatus } | null>(null)
  useEffect(() => editor?.subscribe((status) => setEmitted({ handle: editor, status })), [editor])
  if (!emitted || emitted.handle !== editor) return null
  return emitted.status
}

// gld ne connaît l'état git que par dépôt : le dossier ouvert prend la branche du dépôt le plus proche qui le contient.
export function branchOf(dir: string, status: Record<string, GitState>) {
  let best = ""
  for (const repoPath of Object.keys(status)) {
    const containsDir = dir === repoPath || dir.startsWith(repoPath + "/")
    if (containsDir && repoPath.length > best.length) best = repoPath
  }
  return best ? status[best].branch : undefined
}

type Props = {
  editor: EditorHandle | null
  watch: WatchState
  index: IndexState
  branch?: string
  dirty: number
}

const watchLabel = (watch: WatchState, t: TFunction) => {
  if (watch.state === "open") return t("ide.status.watch.open")
  if (watch.state === "browserRetry") return t("ide.status.watch.closed")
  if (watch.state === "waiting") return t("ide.status.watch.retry", { n: watch.retryIn })
  return t("ide.status.watch.connecting")
}

// L'abonnement au curseur vit ici : une frappe ou un déplacement ne refait le rendu que de la barre, pas de tout l'espace.
// Seuls l'état du flux et celui de l'index sont annoncés : la position du curseur change à chaque frappe.
// Écran étroit : les éléments passent à la ligne, l'encodage et l'indentation disparaissent.
export function StatusBar({ editor, watch, index, branch, dirty }: Props) {
  const { t } = useTranslation()
  const status = useEditorStatus(editor)
  const indent = status?.indent.startsWith("tabs:") ? t("ide.status.tabs") : status && t("ide.status.spaces", { n: status.indent.split(":")[1] })
  const flux = watchLabel(watch, t)
  return (
    <div aria-label={t("ide.status.label")} role="group" className="flex shrink-0 flex-wrap items-center gap-x-3 border-t px-2 text-xs text-muted-foreground">
      {status && (
        <>
          <span>{t("ide.status.pos", { line: status.line, col: status.col })}</span>
          {status.selected > 0 && <span>{t("ide.status.selected", { count: status.selected })}</span>}
          <span>{status.eol}</span>
          {status.readOnly && <span className="rounded border px-1">{t("ide.status.readOnly")}</span>}
          <span>{status.language}</span>
          <span className="hidden sm:inline">{indent}</span>
        </>
      )}
      <span className="hidden sm:inline">{t("ide.status.encoding")}</span>
      {branch && (
        <span className="inline-flex items-center gap-1">
          <GitBranch className="size-3" />
          <span className="sr-only">{t("ide.status.branch")}</span>
          {branch}
        </span>
      )}
      <span role="status" className="inline-flex gap-3">
        <span>{flux}</span>
        <span>{t(`ide.status.index.${index}`)}</span>
      </span>
      {dirty > 0 && <span>{t("ide.status.dirty", { count: dirty })}</span>}
    </div>
  )
}

// Barre de l'espace : branche, onglets modifiés tirés du contexte des commandes.
export function WorkspaceStatus({ dir, ctx, watch, index }: { dir: string; ctx: CmdCtx; watch: WatchState; index: IndexState }) {
  const repos = useGit()
  return <StatusBar editor={ctx.editor} watch={watch} index={index} branch={branchOf(dir, repos.status)} dirty={ctx.tabs.filter(isDirty).length} />
}
