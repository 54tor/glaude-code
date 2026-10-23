import { X } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import type { Row } from "./actions"
import { DD, SessionItems } from "./SessionMenu"

// Barre de sélection multiple : tout sélectionner, actions sur la sélection, vider.
export function SessionSelectionBar({ selected, onSelectAll, onClear }: { selected: Row[]; onSelectAll: () => void; onClear: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="absolute inset-x-0 bottom-4 z-30 flex justify-center px-4">
      <div className="flex items-center gap-1 rounded-xl border bg-popover p-1.5 pl-4 text-sm shadow-lg">
        <span className="mr-2 font-medium">{t("shell.selection.count", { count: selected.length })}</span>
        <Button variant="ghost" size="sm" onClick={() => onSelectAll()}>
          {t("shell.selection.selectAll")}
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm">{t("shell.selection.actions")}</Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="top" className="w-64">
            <SessionItems M={DD} row={selected[0]} />
          </DropdownMenuContent>
        </DropdownMenu>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() => onClear()}
          aria-label={t("shell.selection.clear")}
          title={t("shell.selection.clearHint")}
        >
          <X />
        </Button>
      </div>
    </div>
  )
}
