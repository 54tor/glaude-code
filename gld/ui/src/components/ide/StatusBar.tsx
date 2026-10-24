import { useEffect, useId, useState } from "react"
import { useTranslation } from "react-i18next"
import { GitBranch, SquareTerminal } from "lucide-react"
import { cn } from "cn"
import { buttonVariants } from "@/components/ui/button"
import type { GitState } from "@/lib/api"
import type { EditorHandle, EditorStatus } from "./commands"
import type { IndexState } from "./useIndex"
import type { WatchState } from "./useWatch"

// Statut rattaché à la poignée qui l'a émis : après un changement d'onglet, rien de l'ancien éditeur ne s'affiche.
export function useEditorStatus(editor: EditorHandle | null) {
  const [s, setS] = useState<{ h: EditorHandle; s: EditorStatus } | null>(null)
  useEffect(() => editor?.subscribe((x) => setS({ h: editor, s: x })), [editor])
  return s && s.h === editor ? s.s : null
}

// gld ne connaît l'état git que par dépôt : le dossier ouvert prend la branche du dépôt le plus proche qui le contient.
export function branchOf(dir: string, status: Record<string, GitState>) {
  let best = ""
  for (const k of Object.keys(status)) if ((dir === k || dir.startsWith(k + "/")) && k.length > best.length) best = k
  return best ? status[best].branch : undefined
}

type Props = {
  status: EditorStatus | null
  watch: WatchState
  index: IndexState
  branch?: string
  dirty: number
  // reason : lue au rendu de la barre, le contexte des commandes ne se lit pas pendant celui de l'espace (refs).
  term: { open: boolean; reason(): string; onToggle(): void }
}

// Seuls l'état du flux et celui de l'index sont annoncés : la position du curseur change à chaque frappe.
// Écran étroit : les éléments passent à la ligne, l'encodage et l'indentation disparaissent.
export function StatusBar({ status, watch, index, branch, dirty, term }: Props) {
  const { t } = useTranslation()
  const id = useId()
  const indent = status?.indent.startsWith("tabs:") ? t("ide.status.tabs") : status && t("ide.status.spaces", { n: status.indent.split(":")[1] })
  const flux =
    watch.state === "open"
      ? t("ide.status.watch.open")
      : watch.state === "browserRetry"
        ? t("ide.status.watch.closed")
        : watch.state === "waiting"
          ? t("ide.status.watch.retry", { n: watch.retryIn })
          : t("ide.status.watch.connecting")
  const reason = term.reason()
  const off = !!reason
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
      <span className="flex-1" />
      <button
        type="button"
        aria-label={t("ide.cmd.terminal.toggle")}
        aria-expanded={term.open}
        aria-disabled={off || undefined}
        aria-describedby={off ? id : undefined}
        title={reason || undefined}
        className={cn(buttonVariants({ variant: "ghost", size: "icon-xs" }), off && "opacity-50")}
        onClick={() => !off && term.onToggle()}
      >
        <SquareTerminal />
      </button>
      {off && (
        <span id={id} hidden>
          {reason}
        </span>
      )}
    </div>
  )
}
