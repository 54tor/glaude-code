import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "wouter"
import { href, TEAM_NAME } from "@/lib/route"
import { useActions } from "@/components/actions"
import { Badge } from "@/components/ui/badge"
import { useCan } from "@/lib/auth"
import {
  teamsCatalog,
  teamsLaunch,
  teamsList,
  type Agent,
  type Catalog,
  type Team,
  type TeamBrief,
} from "@/lib/teams"
import { Button } from "@/components/ui/button"
import { AgentEditor } from "./AgentEditor"
import { TeamEditor } from "./TeamEditor"
import { TeamPicker } from "./TeamPicker"

export function TeamsPage({ home }: { home: string }) {
  const { t } = useTranslation()
  const a = useActions()
  const [catalog, setCatalog] = useState<Catalog | null>(null)
  const [err, setErr] = useState("")
  const [running, setRunning] = useState<TeamBrief[]>([])

  const canEdit = useCan()("config_write")
  // undefined : fermé ; null : création
  const [editAgent, setEditAgent] = useState<Agent | null | undefined>(
    undefined
  )
  const [editTeam, setEditTeam] = useState<Team | null | undefined>(undefined)
  const load = useCallback(
    () =>
      void teamsCatalog().then((r) => (r.ok ? setCatalog(r) : setErr(r.out))),
    []
  )
  const saved = () => {
    setEditAgent(undefined)
    setEditTeam(undefined)
    load()
  }

  const refresh = useCallback(() => {
    if (document.visibilityState !== "visible") return
    void teamsList().then((r) => r.ok && setRunning(r.teams))
  }, [])
  useEffect(() => {
    load()
    refresh()
    const iv = setInterval(refresh, 5000)
    return () => clearInterval(iv)
  }, [refresh, load])

  if (err) return <p className="p-4 text-sm text-destructive">{err}</p>
  if (!catalog) return null
  return (
    <div className="mx-auto max-w-4xl space-y-8 p-4">
      <TeamPicker
        catalog={catalog}
        home={home}
        onEdit={canEdit ? setEditTeam : undefined}
        pickDir={a.pickDir}
        onLaunch={async (ids, goal, dir) => {
          const r = await teamsLaunch(ids, goal, dir)
          refresh()
          return r.ok ? r : { ok: false, out: r.out, run: r.run }
        }}
      />
      {canEdit && (
        <>
          <Button variant="outline" onClick={() => setEditTeam(null)}>
            {t("teams.edit.newTeam")}
          </Button>
          <section>
            <div className="mb-2 flex items-center justify-between">
              <h2 className="text-sm font-semibold">{t("teams.agents")}</h2>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEditAgent(null)}
              >
                {t("teams.edit.newAgent")}
              </Button>
            </div>
            <ul className="divide-y rounded-lg border text-sm">
              {catalog.agents.map((ag) => (
                <li key={ag.ID} className="flex items-center gap-2 px-3 py-2">
                  <span className="font-medium">{ag.Name}</span>
                  <span className="text-muted-foreground">
                    {ag.Role} ·{" "}
                    {ag.Access === "lead"
                      ? t("teams.lead")
                      : t(
                          `teams.edit.${ag.Access === "read" ? "read" : "write"}`
                        )}
                  </span>
                  {!ag.Builtin && (
                    <Badge variant="secondary">
                      {t("teams.edit.personal")}
                    </Badge>
                  )}
                  {ag.Access !== "lead" && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="ml-auto"
                      aria-label={t("teams.edit.edit", { name: ag.Name })}
                      onClick={() => setEditAgent(ag)}
                    >
                      {t("teams.edit.modify")}
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          </section>
        </>
      )}
      {editAgent !== undefined && (
        <AgentEditor
          agent={editAgent}
          onClose={() => setEditAgent(undefined)}
          onSaved={saved}
        />
      )}
      {editTeam !== undefined && (
        <TeamEditor
          team={editTeam}
          agents={catalog.agents}
          onClose={() => setEditTeam(undefined)}
          onSaved={saved}
        />
      )}
      {running.length > 0 && (
        <section>
          <h2 className="mb-2 text-sm font-semibold">{t("teams.running")}</h2>
          <ul className="divide-y rounded-lg border text-sm">
            {running.map((r) => (
              <li key={r.Name} className="flex items-center gap-2 px-3 py-2">
                {TEAM_NAME.test(r.Name) ? (
                  <Link
                    href={href({ kind: "team", name: r.Name })}
                    className="font-medium hover:underline"
                  >
                    {r.Name}
                  </Link>
                ) : (
                  <span className="font-medium">{r.Name}</span>
                )}
                <span className="text-muted-foreground">
                  {t("teams.members", { count: r.Members })}
                </span>
                {r.Live && <Badge variant="secondary">{t("teams.live")}</Badge>}
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
