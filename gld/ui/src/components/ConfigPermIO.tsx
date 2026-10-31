import { useState } from "react"
import { Copy, Download, FilePen, RotateCcw, Upload } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { copyText } from "@/lib/utils"

type Doc = Record<string, unknown> | undefined

const RULE_LISTS = ["allow", "ask", "deny"]
const MAX_IMPORT_BYTES = 1 << 20
const MAX_FILENAME_LABEL_LENGTH = 40
const OBJECT_URL_REVOKE_DELAY_MS = 1000

const permissionsOf = (doc: Doc) => (doc?.permissions ?? {}) as Record<string, unknown>

// Le fragment de settings.json qui contient les règles : {"permissions": {"allow": […], "ask": […], "deny": […]}}.
// Il peut être gardé, partagé, ou collé dans un autre fichier de réglages. defaultMode n'en fait pas partie.
export function permsFragment(doc: Doc): string {
  const permissions = permissionsOf(doc)
  const rulesByList: Record<string, string[]> = {}
  for (const listName of RULE_LISTS) {
    const rules = permissions[listName]
    if (!Array.isArray(rules) || rules.length === 0) continue
    rulesByList[listName] = rules.filter((rule): rule is string => typeof rule === "string")
  }
  return JSON.stringify({ permissions: rulesByList }, null, 2) + "\n"
}

export function PermIOButtons({ file, label, doc, onImport, onRaw, onReset }: { file: string; label: string; doc: Doc; onImport: () => void; onRaw: () => void; onReset: () => void }) {
  const { t } = useTranslation()
  const permissions = doc?.permissions as Record<string, unknown> | undefined
  const dirs = permissions?.additionalDirectories
  const count = RULE_LISTS.reduce((total, listName) => {
    const rules = permissions?.[listName]
    return total + (Array.isArray(rules) ? rules.length : 0)
  }, 0)
  const hasAdditionalDirs = Array.isArray(dirs) && dirs.length > 0
  const download = () => {
    const downloadLink = document.createElement("a")
    const fileSuffix = file === "u" ? t("config.permIO.fileUser") : label.replace(/[^\w.-]+/g, "-").slice(0, MAX_FILENAME_LABEL_LENGTH)
    downloadLink.href = URL.createObjectURL(new Blob([permsFragment(doc)], { type: "application/json" }))
    downloadLink.download = `permissions-${fileSuffix}.json`
    downloadLink.click()
    setTimeout(() => URL.revokeObjectURL(downloadLink.href), OBJECT_URL_REVOKE_DELAY_MS)
  }
  const copy = () =>
    copyText(permsFragment(doc)).then(
      () => toast.success(t("config.permIO.copied")),
      () => toast.error(t("config.shared.copyFailed"))
    )
  return (
    <div className="mb-4 flex flex-wrap items-center gap-2">
      <Button size="sm" variant="outline" onClick={download} disabled={count === 0} title={t("config.permIO.exportTitle")}>
        <Download /> {t("config.permIO.export")} ({count})
      </Button>
      <Button size="sm" variant="ghost" onClick={copy} disabled={count === 0}>
        <Copy /> {t("common.copy")}
      </Button>
      <Button size="sm" variant="outline" onClick={onRaw} title={t("config.permIO.rawTitle")}>
        <FilePen /> {t("config.permIO.raw")}
      </Button>
      <Button size="sm" variant="outline" onClick={onImport} title={t("config.permIO.importTitle")}>
        <Upload /> {t("config.permIO.import")}
      </Button>
      <Button size="sm" variant="outline" className="text-destructive" onClick={onReset} disabled={count === 0 && !hasAdditionalDirs} title={t("config.permIO.resetTitle")}>
        <RotateCcw /> {t("config.permIO.reset")}
      </Button>
    </div>
  )
}

// Importer des règles : un fichier exporté, ou un fragment collé. Fusionner ajoute ce qui manque ; Remplacer substitue les
// listes présentes dans le fragment. Un aperçu des différences précède toute écriture.
export function PermImportDialog({
  open,
  file,
  label,
  propose,
  onClose,
}: {
  open: boolean
  file: string
  label: string
  propose: (file: string, text: string, mode: string) => Promise<string | null>
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [text, setText] = useState("")
  const [mode, setMode] = useState<"merge" | "replace">("merge")
  const [error, setError] = useState("")
  const load = async (selectedFile: File | undefined) => {
    if (!selectedFile) return
    if (selectedFile.size > MAX_IMPORT_BYTES) {
      setError(t("config.permIO.tooBig"))
      return
    }
    setText(await selectedFile.text())
    setError("")
  }
  const preview = async () => {
    const proposeError = await propose(file, text, mode)
    if (proposeError) setError(proposeError)
    else onClose()
  }
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("config.permIO.dialogTitle")}</DialogTitle>
          <DialogDescription>{t("config.permIO.dialogDesc", { label })}</DialogDescription>
        </DialogHeader>
        <input type="file" accept=".json,application/json" onChange={(changeEvent) => load(changeEvent.target.files?.[0])} aria-label={t("config.permIO.fileAria")} className="text-sm file:mr-3 file:rounded-md file:border file:bg-muted file:px-3 file:py-1" />
        <textarea
          value={text}
          onChange={(changeEvent) => {
            setText(changeEvent.target.value)
            setError("")
          }}
          placeholder={t("config.permIO.pastePlaceholder")}
          spellCheck={false}
          aria-label={t("config.permIO.textAria")}
          className="h-56 w-full resize-y rounded-md border border-input bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/80"
        />
        <div className="flex flex-wrap items-center gap-2 text-sm" role="radiogroup" aria-label={t("config.permIO.modeAria")}>
          <Button size="sm" variant={mode === "merge" ? "secondary" : "ghost"} role="radio" aria-checked={mode === "merge"} onClick={() => setMode("merge")}>
            {t("config.shared.merge")}
          </Button>
          <Button size="sm" variant={mode === "replace" ? "secondary" : "ghost"} role="radio" aria-checked={mode === "replace"} onClick={() => setMode("replace")}>
            {t("config.shared.replace")}
          </Button>
          <span className="text-xs text-muted-foreground">{mode === "merge" ? t("config.permIO.mergeHint") : t("config.permIO.replaceHint")}</span>
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button onClick={preview} disabled={!text.trim()}>
            {t("config.permIO.preview")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
