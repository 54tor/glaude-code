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
const HTTP_NOT_FOUND = 404

function categoriesFromUrl() {
  const requestedCategories = new URLSearchParams(window.location.search).get("cats") ?? ""
  return requestedCategories.split(",").filter((category) => CATS.includes(category))
}

// Sauvegarde : export de la configuration Claude en archive, puis import en deux temps (aperçu, puis application des seuls éléments cochés).
export function Backup({ home }: { home: string }) {
  const { t } = useTranslation()
  const [cats, setCats] = useState<string[]>(categoriesFromUrl)
  const [plan, setPlan] = useState<BackupPlan | null>(null)
  const [picks, setPicks] = useState<Picks>({})
  const [targets, setTargets] = useState<Record<string, string>>({})
  const [results, setResults] = useState<BackupResult[] | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)

  const stage = async (archive: File | undefined) => {
    if (!archive) return
    setError("")
    setBusy(true)
    try {
      const stagedPlan = await api.backupStage(archive)
      const targetDirs = new Set(stagedPlan.targets.map((target) => target.dir))
      const matchingProjectItems = stagedPlan.items.filter(isProject).filter((item) => targetDirs.has(item.label))
      setPlan(stagedPlan)
      setPicks({})
      setResults(null)
      setTargets(Object.fromEntries(matchingProjectItems.map((item) => [item.label, item.label])))
    } catch (error) {
      setError(errMsg(error))
    }
    setBusy(false)
    if (fileInput.current) fileInput.current.value = ""
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
      const pickedItems = Object.entries(picks).map(([id, mode]) => ({ id, mode }))
      const applyResponse = await api.backupApply(plan.id, pickedItems, targets)
      setResults(applyResponse.results)
    } catch (error) {
      setError(errMsg(error))
      const isPlanGone = error instanceof ApiError && error.status === HTTP_NOT_FOUND
      if (isPlanGone) reset()
    }
    setBusy(false)
  }
  const count = Object.keys(picks).length
  const label = (id: string) => plan?.items.find((item) => item.id === id)?.label ?? id
  const toggleCategory = (category: string, checked: boolean | "indeterminate") =>
    setCats((selectedCategories) => {
      if (checked === true) return [...selectedCategories, category]
      return selectedCategories.filter((selectedCategory) => selectedCategory !== category)
    })

  return (
    <div className="mx-auto w-full max-w-page space-y-8 overflow-y-auto px-4 py-6">
      <section className="space-y-3">
        <h2 className="text-base font-medium">{t("backup.export")}</h2>
        <p className="text-sm text-muted-foreground">{t("backup.exportHint")}</p>
        <p className="text-sm text-muted-foreground">{t("backup.secretsHint")}</p>
        <div className="flex flex-wrap gap-4">
          {CATS.map((category) => (
            <label key={category} className="flex items-center gap-2 text-sm">
              <Checkbox checked={cats.includes(category)} onCheckedChange={(checked) => toggleCategory(category, checked)} aria-label={t(`backup.cat.${category}`)} />
              {t(`backup.cat.${category}`)}
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
            <Button variant="outline" onClick={() => fileInput.current?.click()} disabled={busy}>
              <Upload /> {busy ? t("backup.staging") : t("backup.choose")}
            </Button>
          </>
        )}
        <input ref={fileInput} type="file" accept=".zip" className="hidden" onChange={(changeEvent) => stage(changeEvent.target.files?.[0])} />
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
              extra={(item) => (
                <>
                  {item.kind === "mcp" && item.from && (
                    <p className="mt-1 flex items-center gap-1 text-xs text-warning">
                      <TriangleAlert className="size-3.5" /> {item.from}
                    </p>
                  )}
                  {isProject(item) && (
                    <div className="mt-1">
                      <Choose
                        size="xs"
                        value={targets[item.label] ?? ""}
                        onChange={(targetDir) => setTargets((previousTargets) => ({ ...previousTargets, [item.label]: targetDir }))}
                        options={plan.targets.map((target) => [target.dir, tilde(target.project, home)] as const)}
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
              {results.map((result) => (
                <li key={result.id}>
                  <code className="text-xs">{label(result.id)}</code> <span className={cn("text-xs", TONE[result.status])}>{t(`backup.status.${result.status}`)}</span>
                  {result.message && <p className="text-xs text-muted-foreground">{result.message}</p>}
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

const isProject = (item: { kind: string }) => item.kind === "projmem" || item.kind === "projcmd"
