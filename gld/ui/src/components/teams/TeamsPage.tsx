import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "wouter"
import { href, TEAM_NAME } from "@/lib/route"
import { useActions } from "@/components/actions"
import { Badge } from "@/components/ui/badge"
import { teamsCatalog, teamsLaunch, teamsList, type Catalog, type TeamBrief } from "@/lib/teams"
import { TeamPicker } from "./TeamPicker"

export function TeamsPage({ home }: { home: string }) {
  const { t } = useTranslation()
  const a = useActions()
  const [catalog, setCatalog] = useState<Catalog | null>(null)
  const [err, setErr] = useState("")
  const [running, setRunning] = useState<TeamBrief[]>([])

  const refresh = useCallback(() => {
    if (document.visibilityState !== "visible") return
    void teamsList().then((r) => r.ok && setRunning(r.teams))
  }, [])
  useEffect(() => {
    void teamsCatalog().then((r) => (r.ok ? setCatalog(r) : setErr(r.out)))
    refresh()
    const iv = setInterval(refresh, 5000)
    return () => clearInterval(iv)
  }, [refresh])

  if (err) return <p className="p-4 text-sm text-destructive">{err}</p>
  if (!catalog) return null
  return (
    <div className="mx-auto max-w-4xl space-y-8 p-4">
      <TeamPicker
        catalog={catalog}
        home={home}
        pickDir={a.pickDir}
        onLaunch={async (ids, goal, dir) => {
          const r = await teamsLaunch(ids, goal, dir)
          refresh()
          return r.ok ? r : { ok: false, out: r.out, run: r.run }
        }}
      />
      {running.length > 0 && (
        <section>
          <h2 className="mb-2 text-sm font-semibold">{t("teams.running")}</h2>
          <ul className="divide-y rounded-lg border text-sm">
            {running.map((r) => (
              <li key={r.Name} className="flex items-center gap-2 px-3 py-2">
                {TEAM_NAME.test(r.Name) ? (
                  <Link href={href({ kind: "team", name: r.Name })} className="font-medium hover:underline">
                    {r.Name}
                  </Link>
                ) : (
                  <span className="font-medium">{r.Name}</span>
                )}
                <span className="text-muted-foreground">{t("teams.members", { count: r.Members })}</span>
                {r.Live && <Badge variant="secondary">{t("teams.live")}</Badge>}
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
