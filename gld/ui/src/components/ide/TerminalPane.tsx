import { lazy, Suspense, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { api, type Data } from "@/lib/api"
import { useCan } from "@/lib/auth"
import { useLocal } from "@/hooks/useLocal"

const FullTerminal = lazy(() => import("../FullTerminal"))

// Le serveur ne lance jamais de shell : le terminal s'attache à une session Claude déjà vivante dont le dossier est le projet.
function Pane({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const [on, setOn] = useState<boolean | null>(null)
  const [data, setData] = useState<Data | null>(null)
  const [seen, setSeen] = useState("")
  const [pick, setPick] = useLocal(`gld.ide.${dir}.term`, "")
  useEffect(() => {
    api.termInfo().then(
      (r) => setOn(r.enabled),
      () => setOn(false)
    )
  }, [])
  useEffect(() => {
    if (!on) return
    let alive = true
    const load = () =>
      api.data().then(
        (d) => alive && setData(d),
        () => {}
      )
    load()
    const timer = setInterval(load, 10000)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [on])

  const bare = (p: string) => p.replace(/[/\\]+$/, "")
  const live = new Set(data?.live ?? [])
  const sessions = (data?.groups ?? [])
    .flatMap((g) => g.sessions)
    .filter((s) => bare(s.cwd) === bare(dir) && live.has(s.id))
    .sort((a, b) => b.mtime - a.mtime)
  // Collante : le tri par activité change à chaque sondage, la session montée ne doit pas changer avec lui.
  const cur = sessions.find((s) => s.id === pick) ?? sessions.find((s) => s.id === seen) ?? sessions[0]
  const curId = cur?.id
  if (curId && curId !== seen) setSeen(curId)
  if (on === null) return null
  if (!on)
    return (
      <p className="p-3 text-sm text-muted-foreground">
        {t("ide.term.disabled")}
      </p>
    )
  if (!cur)
    return (
      <p className="p-3 text-sm text-muted-foreground">
        {t(seen ? "ide.term.ended" : "ide.term.none")}
      </p>
    )
  return (
    <div
      className="flex size-full min-h-0 flex-col"
      aria-label={t("ide.term.label")}
    >
      {sessions.length > 1 && (
        <select
          aria-label={t("ide.term.pick")}
          value={cur.id}
          onChange={(e) => setPick(e.target.value)}
          className="m-1 w-fit rounded-md border bg-background px-2 py-1 text-xs"
        >
          {sessions.map((s) => (
            <option key={s.id} value={s.id}>
              {s.title || s.id.slice(0, 8)}
            </option>
          ))}
        </select>
      )}
      <Suspense fallback={null}>
        <FullTerminal key={cur.id} id={cur.id} takeHand={false} />
      </Suspense>
    </div>
  )
}

export function TerminalPane({ dir }: { dir: string }) {
  return useCan()("exec") ? <Pane dir={dir} /> : null
}
