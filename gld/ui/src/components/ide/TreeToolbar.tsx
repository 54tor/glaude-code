import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { FilePlus, FileSearch, FolderPlus, ListCollapse, RefreshCw, Trash2, X } from "lucide-react"
import { Toolbar } from "radix-ui"
import { TooltipProvider } from "@/components/ui/tooltip"
import { CmdButton, ToolButton } from "./ToolButton"
import { TrashDialog } from "./TrashDialog"
import type { CmdCtx } from "./commands"
import type { useTree } from "./useTree"

type Props = {
  dir: string
  tree: Pick<ReturnType<typeof useTree>, "refreshAll" | "collapseAll">
  ctx: CmdCtx
  filter: string
  onFilter: (v: string) => void
  onRestored: () => void
}

export function TreeToolbar({ dir, tree, ctx, filter, onFilter, onRestored }: Props) {
  const { t } = useTranslation()
  const [trash, setTrash] = useState(false)
  // Effacer par le bouton le démonte : le focus revient au champ, sinon il tomberait sur la page.
  const input = useRef<HTMLInputElement>(null)
  return (
    <TooltipProvider>
      <div className="flex shrink-0 flex-col gap-1 border-b px-1 py-1">
        <Toolbar.Root aria-label={t("ide.toolbar.tree")} className="flex items-center gap-0.5">
          <CmdButton id="file.newFile" ctx={ctx}>
            <FilePlus />
          </CmdButton>
          <CmdButton id="file.newFolder" ctx={ctx}>
            <FolderPlus />
          </CmdButton>
          <ToolButton label={t("ide.toolbar.refresh")} onClick={tree.refreshAll}>
            <RefreshCw />
          </ToolButton>
          <ToolButton label={t("ide.toolbar.collapseAll")} onClick={tree.collapseAll}>
            <ListCollapse />
          </ToolButton>
          <CmdButton id="file.quickOpen" ctx={ctx}>
            <FileSearch />
          </CmdButton>
          <ToolButton label={t("ide.toolbar.trash")} onClick={() => setTrash(true)}>
            <Trash2 />
          </ToolButton>
        </Toolbar.Root>
        <div className="relative flex items-center">
          <input
            ref={input}
            type="search"
            value={filter}
            aria-label={t("ide.toolbar.filter")}
            placeholder={t("ide.toolbar.filterPlaceholder")}
            onChange={(e) => onFilter(e.target.value)}
            onKeyDown={(e) => e.key === "Escape" && filter && (e.preventDefault(), onFilter(""))}
            className="h-7 w-full rounded-md border border-input bg-background px-2 pr-7 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/80 [&::-webkit-search-cancel-button]:hidden"
          />
          {filter && (
            <button type="button" aria-label={t("ide.toolbar.clearFilter")} onClick={() => (onFilter(""), input.current?.focus())} className="absolute right-1 grid size-5 place-items-center rounded-sm text-muted-foreground hover:bg-muted">
              <X className="size-3.5" />
            </button>
          )}
        </div>
      </div>
      <TrashDialog dir={dir} open={trash} canWrite={ctx.can("fs_write")} onClose={() => setTrash(false)} onRestored={onRestored} />
    </TooltipProvider>
  )
}
