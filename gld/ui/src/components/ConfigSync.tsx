import { useCallback, useEffect, useState } from "react"
import { ArrowRight } from "lucide-react"
import { useTranslation } from "react-i18next"
import { api, type HomesInfo, type SyncItem } from "@/lib/api"
import { tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Choose } from "./Choose"
import { SyncItems, type Mode } from "./SyncItems"
import { useActions } from "./actions"
import { reportAction } from "@/lib/report"
import { errMsg } from "@/lib/apiError"

type Item = SyncItem

// Copie des réglages d'un dossier de configuration Claude vers un autre : chaque élément se compare, se prévisualise et se choisit
// (fusionner ou remplacer). Le plan vient du serveur, qui revérifie chaque choix avant d'écrire et sauvegarde ce qu'il remplace.
export function SyncTab() {
  const { t } = useTranslation()
  const a = useActions()
  const [info, setInfo] = useState<HomesInfo | null>(null)
  const [from, setFrom] = useState("")
  const [to, setTo] = useState("")
  const [items, setItems] = useState<Item[] | null>(null)
  const [picks, setPicks] = useState<Record<string, Mode>>({})
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.homes().then((r) => {
      setInfo(r)
      if (r.homes.length > 0) setTo(r.homes[0].name)
    }, () => {})
  }, [])

  const load = useCallback(async (f: string, dest: string) => {
    setItems(null)
    setPicks({})
    setError("")
    if (f === dest) return setError(t("config.sync.pickTwo"))
    try {
      setItems((await api.syncPlan(f, dest)).items ?? [])
    } catch (e) {
      setError(errMsg(e))
    }
  }, [t])
  useEffect(() => {
    if (info) load(from, to) // eslint-disable-line react-hooks/set-state-in-effect
  }, [info, from, to, load])

  if (!info) return <p className="py-8 text-sm text-muted-foreground">{t("common.loading")}</p>
  if (info.homes.length === 0)
    return <p className="py-6 text-sm text-muted-foreground">{t("config.sync.addSecond")}</p>

  const options: [string, string][] = [["", t("config.sync.main", { dir: tilde(info.main, a.home) })], ...info.homes.map((h): [string, string] => [h.name, t("config.sync.named", { name: h.name, dir: tilde(h.dir, a.home) })])]
  const todo = (items ?? []).filter((i) => i.status !== "same")
  const chosen = Object.keys(picks)
  const setAll = (f: (i: Item) => Mode | null) => setPicks(Object.fromEntries(todo.flatMap((i) => (f(i) ? [[i.id, f(i)!]] : []))))
  const apply = async () => {
    setBusy(true)
    const ok = await reportAction(() => api.syncApply(from, to, chosen.map((id) => ({ id, mode: picks[id] }))))
    setBusy(false)
    if (ok) load(from, to)
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Choose value={from} onChange={setFrom} options={options} label={t("config.sync.source")} className="min-w-0 max-w-xs" />
        <ArrowRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <Choose value={to} onChange={setTo} options={options} label={t("config.sync.target")} className="min-w-0 max-w-xs" />
      </div>
      <p className="text-xs text-muted-foreground">
        {t("config.sync.noteBefore")} <code>claude mcp add</code>
        {t("config.sync.noteAfter")}
      </p>
      {error && <p className="text-sm text-destructive">{error}</p>}
      {items && (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => setAll((i) => (i.status === "new" || i.merge ? (i.merge ? "merge" : "replace") : null))} title={t("config.sync.mergeAllTitle")}>
              {t("config.sync.mergeAll")}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setAll(() => "replace")} title={t("config.sync.replaceAllTitle")}>
              {t("config.sync.replaceAll")}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setPicks({})} disabled={chosen.length === 0}>
              {t("config.sync.none")}
            </Button>
            <Button size="sm" className="ml-auto" onClick={apply} disabled={chosen.length === 0 || busy}>
              {chosen.length ? t("config.sync.copyItems", { count: chosen.length }) : t("config.sync.copyNone")}
            </Button>
          </div>
          {todo.length === 0 && <p className="text-sm text-muted-foreground">{t("config.sync.nothingToCopy")}</p>}
          <SyncItems items={items} picks={picks} setPicks={setPicks} />
        </>
      )}
    </div>
  )
}
