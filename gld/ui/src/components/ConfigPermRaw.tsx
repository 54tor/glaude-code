import { useEffect, useMemo, useState } from "react"
import { CircleAlert, TriangleAlert } from "lucide-react"
import { useTranslation } from "react-i18next"
import { formatRaw, parseRaw, type RawPerms } from "@/lib/permLint"
import { api, type CfgIssue } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { DiffView } from "./MemoryDiff"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

type Doc = Record<string, unknown> | undefined

const PREVIEW_DEBOUNCE_MS = 400

const rulesOf = (doc: Doc, listName: string): string[] => {
  const rules = (doc?.permissions as Record<string, unknown> | undefined)?.[listName]
  if (!Array.isArray(rules)) return []
  return rules.filter((rule): rule is string => typeof rule === "string")
}

// Édition de toutes les règles (allow, ask, deny) et des dossiers supplémentaires d'un fichier en texte brut, vérifiée à la frappe. L'enregistrement passe par
// l'import en mode « remplacer » : aperçu des différences, sauvegarde, puis écriture.
function Editor({ file, label, doc, propose, onClose }: { file: string; label: string; doc: Doc; propose: (file: string, text: string, mode: string, then?: () => void, now?: boolean) => Promise<string | null>; onClose: () => void }) {
  const { t } = useTranslation()
  const [text, setText] = useState(() => formatRaw({ allow: rulesOf(doc, "allow"), ask: rulesOf(doc, "ask"), deny: rulesOf(doc, "deny"), additionalDirectories: rulesOf(doc, "additionalDirectories") } as RawPerms))
  const [error, setError] = useState("")
  const { perms, lint } = useMemo(() => parseRaw(text), [text])
  const errors = lint.filter((lintEntry) => lintEntry.level === "error").length
  // Aperçu en direct : essai à blanc du serveur (différences et avertissements, dont les dossiers introuvables).
  const [lastPreview, setLastPreview] = useState<{ before: string; after: string; warns: CfgIssue[] } | null>(null)
  const body = useMemo(() => JSON.stringify({ permissions: perms }), [perms])
  useEffect(() => {
    if (errors > 0) return
    let isMounted = true
    const previewTimer = setTimeout(async () => {
      try {
        const dryRun = await api.configPermImport({ file, text: body, mode: "replace", dry: true })
        if (isMounted) setLastPreview({ before: dryRun.before, after: dryRun.after, warns: dryRun.warnings ?? [] })
      } catch (error) {
        if (isMounted) setError(errMsg(error))
      }
    }, PREVIEW_DEBOUNCE_MS)
    return () => {
      isMounted = false
      clearTimeout(previewTimer)
    }
  }, [body, errors, file])
  const preview = errors > 0 ? null : lastPreview
  const hasChanges = !!preview && preview.before !== preview.after
  const isUnchanged = !!preview && preview.before === preview.after
  const save = async () => {
    const proposeError = await propose(file, body, "replace", undefined, true)
    if (proposeError) setError(proposeError)
    else onClose()
  }
  return (
    <DialogContent className="sm:max-w-3xl">
      <DialogHeader>
        <DialogTitle>{t("config.permRaw.title")}</DialogTitle>
        <DialogDescription>{t("config.permRaw.desc", { label })}</DialogDescription>
      </DialogHeader>
      <textarea
        value={text}
        onChange={(changeEvent) => {
          setText(changeEvent.target.value)
          setError("")
        }}
        spellCheck={false}
        aria-label={t("config.permRaw.textAria")}
        className="h-72 w-full resize-y rounded-md border border-input bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/80"
      />
      <ul className="max-h-32 space-y-0.5 overflow-y-auto text-xs" aria-label={t("config.permRaw.lintAria")}>
        {lint.map((lintEntry, lintIndex) => (
          <li key={lintIndex} className={lintEntry.level === "error" ? "flex gap-1.5 text-destructive" : "flex gap-1.5 text-warning"}>
            {lintEntry.level === "error" ? <CircleAlert className="mt-0.5 size-3.5 shrink-0" /> : <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />}
            <span className="tabular-nums">{t("config.permRaw.line", { n: lintEntry.line })}</span> {lintEntry.msg}
          </li>
        ))}
        {lint.length === 0 && <li className="text-muted-foreground">{t("config.permRaw.clean", { rules: perms.allow.length + perms.ask.length + perms.deny.length, dirs: perms.additionalDirectories.length })}</li>}
      </ul>
      {hasChanges && (
        <>
          {preview.warns.length > 0 && (
            <ul className="space-y-0.5 text-xs text-warning" aria-label={t("config.permRaw.warnAria")}>
              {preview.warns.map((warning, warningIndex) => (
                <li key={warningIndex}>⚠ {warning.msg}</li>
              ))}
            </ul>
          )}
          <div className="max-h-48 overflow-auto rounded-md border" aria-label={t("config.permRaw.diffAria")}>
            <DiffView old={preview.before} cur={preview.after} compact />
          </div>
        </>
      )}
      {isUnchanged && errors === 0 && <p className="text-xs text-muted-foreground">{t("config.permRaw.unchanged")}</p>}
      {error && <p className="text-sm text-destructive">{error}</p>}
      <DialogFooter>
        <Button variant="ghost" onClick={onClose}>
          {t("common.cancel")}
        </Button>
        <Button onClick={save} disabled={errors > 0 || !hasChanges} title={errors ? t("config.permRaw.fixErrors") : undefined}>
          {t("common.save")}
        </Button>
      </DialogFooter>
    </DialogContent>
  )
}

export function PermRawDialog({ open, ...editorProps }: { open: boolean; file: string; label: string; doc: Doc; propose: (file: string, text: string, mode: string, then?: () => void, now?: boolean) => Promise<string | null>; onClose: () => void }) {
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && editorProps.onClose()}>
      {open && <Editor key={editorProps.file} {...editorProps} />}
    </Dialog>
  )
}
