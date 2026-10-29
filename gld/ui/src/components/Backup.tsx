import { useRef, useState } from "react"
import { Download, TriangleAlert, Upload } from "lucide-react"
import { useTranslation } from "react-i18next"
import { api, type BackupPlan, type BackupResult } from "@/lib/api"
import { ApiError, errMsg } from "@/lib/apiError"
import { tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Choose } from "./Choose"
import { SyncItems, type Picks } from "./SyncItems"

const CATS = ["skills", "plugins", "settings", "memory", "mcp"]
const TONE = { ok: "text-success", failed: "text-destructive", skipped: "text-muted-foreground" } as const

// Sauvegarde : export de la configuration Claude en archive, puis import en deux temps (aperçu, puis application des seuls éléments cochés).
export function Backup({ home }: { home: string }) {
  const { t } = useTranslation()
  const [cats, setCats] = useState<string[]>(() => (new URLSearchParams(window.location.search).get("cats") ?? "").split(",").filter((c) => CATS.includes(c)))
  const [plan, setPlan] = useState<BackupPlan | null>(null)
  const [picks, setPicks] = useState<Picks>({})
  const [targets, setTargets] = useState<Record<string, string>>({})
  const [results, setResults] = useState<BackupResult[] | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const file = useRef<HTMLInputElement>(null)

  const stage = async (f: File | undefined) => {
    if (!f) return
    setError("")
    setBusy(true)
    try {
      const p = await api.backupStage(f)
      const dirs = new Set(p.targets.map((x) => x.dir))
      setPlan(p)
      setPicks({})
      setResults(null)
      setTargets(Object.fromEntries(p.items.filter(isProject).filter((i) => dirs.has(i.label)).map((i) => [i.label, i.label])))
    } catch (e) {
      setError(errMsg(e))
    }
    setBusy(false)
    if (file.current) file.current.value = ""
  }
  const reset = () => {
    setPlan(null)
    setPicks({})
    setResults(null)
  }
  const cancel = async () => {
    if (plan && !results) await api.backupDrop(plan.id).catch(() => {})
    reset()
  }
  const apply = async () => {
    if (!plan) return
    setBusy(true)
    setError("")
    try {
      const r = await api.backupApply(plan.id, Object.entries(picks).map(([id, mode]) => ({ id, mode })), targets)
      setResults(r.results)
    } catch (e) {
      setError(errMsg(e))
      if (e instanceof ApiError && e.status === 404) reset()
    }
    setBusy(false)
  }
  const count = Object.keys(picks).length
  const label = (id: string) => plan?.items.find((i) => i.id === id)?.label ?? id

  return (
    <div className="mx-auto w-full max-w-3xl space-y-8 overflow-y-auto px-4 py-6">
      <section className="space-y-3">
        <h2 className="text-base font-medium">{t("backup.export")}</h2>
        <p className="text-sm text-muted-foreground">{t("backup.exportHint")}</p>
        <p className="text-sm text-muted-foreground">{t("backup.secretsHint")}</p>
        <div className="flex flex-wrap gap-4">
          {CATS.map((c) => (
            <label key={c} className="flex items-center gap-2 text-sm">
              <Checkbox checked={cats.includes(c)} onCheckedChange={(v) => setCats((l) => (v === true ? [...l, c] : l.filter((x) => x !== c)))} aria-label={t(`backup.cat.${c}`)} />
              {t(`backup.cat.${c}`)}
            </label>
          ))}
        </div>
        {cats.length ? (
          <Button asChild>
            <a href={api.backupExportUrl(cats)} download>
              <Download /> {t("backup.download")}
            </a>
          </Button>
        ) : (
          <Button disabled>
            <Download /> {t("backup.download")}
          </Button>
        )}
      </section>

      <section className="space-y-3">
        <h2 className="text-base font-medium">{t("backup.import")}</h2>
        {!plan && (
          <>
            <p className="text-sm text-muted-foreground">{t("backup.importHint")}</p>
            <Button variant="outline" onClick={() => file.current?.click()} disabled={busy}>
              <Upload /> {busy ? t("backup.staging") : t("backup.choose")}
            </Button>
          </>
        )}
        <input ref={file} type="file" accept=".zip" className="hidden" onChange={(e) => stage(e.target.files?.[0])} />
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        {plan && !results && (
          <div className="space-y-4">
            {plan.items.length === 0 && <p className="text-sm text-muted-foreground">{t("backup.none")}</p>}
            {plan.skipped.length > 0 && <p className="text-xs text-muted-foreground">{t("backup.skipped", { list: plan.skipped.join(", ") })}</p>}
            <SyncItems
              items={plan.items}
              picks={picks}
              setPicks={setPicks}
              groupBy="cat"
              extra={(i) => (
                <>
                  {i.kind === "mcp" && i.from && (
                    <p className="mt-1 flex items-center gap-1 text-xs text-warning">
                      <TriangleAlert className="size-3.5" /> {i.from}
                    </p>
                  )}
                  {isProject(i) && (
                    <div className="mt-1">
                      <Choose
                        size="xs"
                        value={targets[i.label] ?? ""}
                        onChange={(v) => setTargets((x) => ({ ...x, [i.label]: v }))}
                        options={plan.targets.map((x) => [x.dir, tilde(x.project, home)] as const)}
                        label={t("backup.project")}
                        className="max-w-xs"
                      />
                    </div>
                  )}
                </>
              )}
            />
            <div className="flex gap-2">
              <Button onClick={apply} disabled={count === 0 || busy}>
                {count ? t("backup.applyCount", { count }) : t("backup.apply")}
              </Button>
              <Button variant="ghost" onClick={cancel} disabled={busy}>
                {t("backup.cancel")}
              </Button>
            </div>
          </div>
        )}
        {results && (
          <div role="status" className="space-y-3">
            <h3 className="text-sm font-medium">{t("backup.report")}</h3>
            <ul className="space-y-1 text-sm">
              {results.map((r) => (
                <li key={r.id}>
                  <code className="text-xs">{label(r.id)}</code> <span className={cn("text-xs", TONE[r.status])}>{t(`backup.status.${r.status}`)}</span>
                  {r.message && <p className="text-xs text-muted-foreground">{r.message}</p>}
                </li>
              ))}
            </ul>
            <Button variant="outline" onClick={reset}>
              {t("backup.again")}
            </Button>
          </div>
        )}
      </section>
    </div>
  )
}

const isProject = (i: { kind: string }) => i.kind === "projmem" || i.kind === "projcmd"
