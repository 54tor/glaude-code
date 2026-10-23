import { useState } from "react"
import { Copy, Download, FilePen, Upload } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

type Doc = Record<string, unknown> | undefined

// Le fragment de settings.json qui contient les règles : {"permissions": {"allow": […], "ask": […], "deny": […]}}.
// Il peut être gardé, partagé, ou collé dans un autre fichier de réglages. defaultMode n'en fait pas partie.
export function permsFragment(doc: Doc): string {
  const p = (doc?.permissions ?? {}) as Record<string, unknown>
  const out: Record<string, string[]> = {}
  for (const k of ["allow", "ask", "deny"]) if (Array.isArray(p[k]) && (p[k] as unknown[]).length) out[k] = (p[k] as unknown[]).filter((x): x is string => typeof x === "string")
  return JSON.stringify({ permissions: out }, null, 2) + "\n"
}

export function PermIOButtons({ file, label, doc, onImport, onRaw }: { file: string; label: string; doc: Doc; onImport: () => void; onRaw: () => void }) {
  const { t } = useTranslation()
  const count = ["allow", "ask", "deny"].reduce((n, k) => n + (Array.isArray((doc?.permissions as Record<string, unknown> | undefined)?.[k]) ? ((doc!.permissions as Record<string, unknown[]>)[k]).length : 0), 0)
  const download = () => {
    const a = document.createElement("a")
    a.href = URL.createObjectURL(new Blob([permsFragment(doc)], { type: "application/json" }))
    a.download = `permissions-${file === "u" ? t("config.permIO.fileUser") : label.replace(/[^\w.-]+/g, "-").slice(0, 40)}.json`
    a.click()
    URL.revokeObjectURL(a.href)
  }
  const copy = () =>
    navigator.clipboard.writeText(permsFragment(doc)).then(
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
  const load = async (f: File | undefined) => {
    if (!f) return
    if (f.size > 1 << 20) return void setError(t("config.permIO.tooBig"))
    setText(await f.text())
    setError("")
  }
  const preview = async () => {
    const err = await propose(file, text, mode)
    if (err) setError(err)
    else onClose()
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("config.permIO.dialogTitle")}</DialogTitle>
          <DialogDescription>{t("config.permIO.dialogDesc", { label })}</DialogDescription>
        </DialogHeader>
        <input type="file" accept=".json,application/json" onChange={(e) => load(e.target.files?.[0])} aria-label={t("config.permIO.fileAria")} className="text-sm file:mr-3 file:rounded-md file:border file:bg-muted file:px-3 file:py-1" />
        <textarea
          value={text}
          onChange={(e) => (setText(e.target.value), setError(""))}
          placeholder={t("config.permIO.pastePlaceholder")}
          spellCheck={false}
          aria-label={t("config.permIO.textAria")}
          className="h-56 w-full resize-y rounded-md border bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
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
