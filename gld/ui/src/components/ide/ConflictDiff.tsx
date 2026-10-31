import { lazy, Suspense } from "react"
import { useTranslation } from "react-i18next"
import { X } from "lucide-react"
import { Button } from "@/components/ui/button"

const DiffView = lazy(() => import("./CodeEditor").then((editorModule) => ({ default: editorModule.DiffView })))

export function ConflictDiff({ path, original, modified, theme, onClose }: { path: string; original: string; modified: string; theme: "light" | "dark"; onClose(): void }) {
  const { t } = useTranslation()
  return (
    <div className="absolute inset-0 z-10 flex flex-col bg-background">
      <div className="flex items-center gap-2 border-b px-3 py-1 text-xs text-muted-foreground">
        <span className="min-w-0 flex-1 truncate">{t("ide.diff.title")}</span>
        <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label={t("ide.diff.close")}>
          <X />
        </Button>
      </div>
      <div className="min-h-0 flex-1">
        <Suspense fallback={<p className="p-6 text-sm text-muted-foreground">{t("ide.editor.loading")}</p>}>
          <DiffView path={path} original={original} modified={modified} theme={theme} />
        </Suspense>
      </div>
    </div>
  )
}
