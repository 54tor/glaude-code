import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useEditorDraft, useLeaveGuard } from "@/hooks/useEditorDraft"

// Le fichier de réglages en JSON brut : tout est modifiable (hooks, MCP du projet…), avec validation de la syntaxe, aperçu des
// différences et sauvegarde. Les secrets sont affichés masqués ; laissés tels quels, ils sont conservés à l'enregistrement.
export function RawTab({ file, version, proposeRaw }: { file: string; version: number; proposeRaw: (file: string, text: string) => Promise<string | null> }) {
  const { t } = useTranslation()
  const [loaded, setLoaded] = useState<{ key: string; text: string; err?: string } | null>(null)
  const [draft, setDraft] = useEditorDraft("config-json")
  const [error, setError] = useState("")
  const key = `${file}#${version}`
  useEffect(() => {
    let live = true
    api.configRaw(file).then(
      (r) => live && setLoaded({ key, text: r.text }),
      (e) => live && setLoaded({ key, text: "", err: errMsg(e) }),
    )
    return () => {
      live = false
    }
  }, [file, key])
  useLeaveGuard(!!draft && !(draft.key === key && draft.text === loaded?.text))
  if (loaded?.key !== key) return <Skeleton className="h-64 w-full" />
  const text = draft?.key === key ? draft.text : loaded.text
  const dirty = text !== loaded.text
  const save = async () => setError((await proposeRaw(file, text)) ?? "")
  return (
    <div className="space-y-2">
      {loaded.err && <p className="text-sm text-destructive">{loaded.err}</p>}
      <textarea
        value={text}
        onChange={(e) => (setDraft({ key, text: e.target.value }), setError(""))}
        onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "s" && (e.preventDefault(), dirty && save())}
        spellCheck={false}
        aria-label={t("config.raw.aria")}
        aria-invalid={!!error || undefined}
        aria-describedby={error ? "raw-error" : undefined}
        className="h-[28rem] w-full resize-y rounded-md border border-input bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/80"
      />
      {error && (
        <p id="raw-error" role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={save} disabled={!dirty}>
          {t("config.raw.preview")}
        </Button>
        <Button size="sm" variant="ghost" onClick={() => (setDraft(null), setError(""))} disabled={!dirty}>
          {t("config.raw.discard")}
        </Button>
        <span className="text-xs text-muted-foreground">{t("config.raw.hint")}</span>
      </div>
    </div>
  )
}
