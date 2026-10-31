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
  const actions = useActions()
  const [info, setInfo] = useState<HomesInfo | null>(null)
  const [from, setFrom] = useState("")
  const [to, setTo] = useState("")
  const [items, setItems] = useState<Item[] | null>(null)
  const [picks, setPicks] = useState<Record<string, Mode>>({})
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.homes().then((homesInfo) => {
      setInfo(homesInfo)
      if (homesInfo.homes.length > 0) setTo(homesInfo.homes[0].name)
    }, () => {})
  }, [])

  const load = useCallback(async (source: string, dest: string) => {
    setItems(null)
    setPicks({})
    setError("")
    if (source === dest) {
      setError(t("config.sync.pickTwo"))
      return
    }
    try {
      const syncPlan = await api.syncPlan(source, dest)
      setItems(syncPlan.items ?? [])
    } catch (error) {
      setError(errMsg(error))
    }
  }, [t])
  useEffect(() => {
    if (info) load(from, to) // eslint-disable-line react-hooks/set-state-in-effect
  }, [info, from, to, load])

  if (!info) return <p className="py-8 text-sm text-muted-foreground">{t("common.loading")}</p>
  if (info.homes.length === 0)
    return <p className="py-6 text-sm text-muted-foreground">{t("config.sync.addSecond")}</p>

  const mainOption: [string, string] = ["", t("config.sync.main", { dir: tilde(info.main, actions.home) })]
  const homeOptions = info.homes.map((home): [string, string] => [home.name, t("config.sync.named", { name: home.name, dir: tilde(home.dir, actions.home) })])
  const options: [string, string][] = [mainOption, ...homeOptions]
  const todo = (items ?? []).filter((item) => item.status !== "same")
  const chosen = Object.keys(picks)
  const setAll = (modeFor: (item: Item) => Mode | null) => {
    const chosenEntries = todo.flatMap((item) => {
      const mode = modeFor(item)
      return mode ? [[item.id, mode]] : []
    })
    setPicks(Object.fromEntries(chosenEntries))
  }
  const defaultModeFor = (item: Item): Mode | null => {
    if (item.merge) return "merge"
    if (item.status === "new") return "replace"
    return null
  }
  const apply = async () => {
    setBusy(true)
    const applied = await reportAction(() => api.syncApply(from, to, chosen.map((id) => ({ id, mode: picks[id] }))))
    setBusy(false)
    if (applied) load(from, to)
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
            <Button size="sm" variant="outline" onClick={() => setAll(defaultModeFor)} title={t("config.sync.mergeAllTitle")}>
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
