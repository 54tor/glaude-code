import { useTranslation } from "react-i18next"
import { TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"

// readOnly : aucune action d'écriture (Écraser, Enregistrer sous le même nom).
type Props = { kind: "changed" | "deleted"; readOnly?: boolean; onReload(): void; onOverwrite(): void; onDiff(): void; onRecreate(): void; onClose(): void }

export function ConflictBar({ kind, readOnly, onReload, onOverwrite, onDiff, onRecreate, onClose }: Props) {
  const { t } = useTranslation()
  return (
    <div role="alert" className="flex flex-wrap items-center gap-2 border-b bg-warning/10 px-3 py-1.5 text-sm">
      <TriangleAlert className="size-4 shrink-0 text-warning" />
      <span className="min-w-0 flex-1">{t(kind === "deleted" ? "ide.conflict.deleted" : "ide.conflict.changed")}</span>
      {kind === "changed" ? (
        <>
          <Button size="sm" variant="outline" onClick={onReload}>
            {t("ide.conflict.reload")}
          </Button>
          {!readOnly && (
            <Button size="sm" variant="destructive" onClick={onOverwrite}>
              {t("ide.conflict.overwrite")}
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={onDiff}>
            {t("ide.conflict.diff")}
          </Button>
        </>
      ) : (
        <>
          {!readOnly && (
            <Button size="sm" variant="outline" onClick={onRecreate}>
              {t("ide.conflict.recreate")}
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={onClose}>
            {t("ide.conflict.close")}
          </Button>
        </>
      )}
    </div>
  )
}
