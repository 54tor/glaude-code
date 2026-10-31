import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "wouter"
import { href, TEAM_NAME } from "@/lib/route"
import { useActions } from "@/components/actions"
import { Badge } from "@/components/ui/badge"
import { errMsg } from "@/lib/apiError"
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
import { sameLabel } from "./editorBits"

const RUNNING_REFRESH_MS = 5000

export function TeamsPage({ home }: { home: string }) {
  const { t } = useTranslation()
  const actions = useActions()
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
      void teamsCatalog().then(setCatalog, (catalogError) => setErr(errMsg(catalogError))),
    []
  )
  const accessLabel = (agent: Agent) => {
    if (agent.Access === "lead") return t("teams.lead")
    const accessKey = agent.Access === "read" ? "read" : "write"
    return t(`teams.edit.${accessKey}`)
  }
  const saved = () => {
    setEditAgent(undefined)
    setEditTeam(undefined)
    load()
  }

  const refresh = useCallback(() => {
    if (document.visibilityState !== "visible") return
    teamsList().then((teamList) => setRunning(teamList.teams), () => {})
  }, [])
  useEffect(() => {
    load()
    refresh()
    const refreshInterval = setInterval(refresh, RUNNING_REFRESH_MS)
    return () => clearInterval(refreshInterval)
  }, [refresh, load])

  if (err) return <p className="p-4 text-sm text-destructive">{err}</p>
  if (!catalog) return null
  return (
    <div className="mx-auto max-w-page space-y-8 p-4">
      <TeamPicker
        catalog={catalog}
        home={home}
        onEdit={canEdit ? setEditTeam : undefined}
        pickDir={actions.pickDir}
        onLaunch={async (ids, goal, dir) => {
          try {
            await teamsLaunch(ids, goal, dir)
          } finally {
            refresh()
          }
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
              {catalog.agents.map((agent) => (
                <li key={agent.ID} className="flex items-center gap-2 px-3 py-2">
                  <span className="font-medium">{agent.Name}</span>
                  <span className="text-muted-foreground">
                    {!sameLabel(agent.Name, agent.Role) && `${agent.Role} · `}
                    {accessLabel(agent)}
                  </span>
                  {!agent.Builtin && (
                    <Badge variant="secondary">
                      {t("teams.edit.personal")}
                    </Badge>
                  )}
                  {agent.Access !== "lead" && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="ml-auto"
                      aria-label={t("teams.edit.edit", { name: agent.Name })}
                      onClick={() => setEditAgent(agent)}
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
            {running.map((runningTeam) => (
              <li key={runningTeam.Name} className="flex items-center gap-2 px-3 py-2">
                {TEAM_NAME.test(runningTeam.Name) ? (
                  <Link
                    href={href({ kind: "team", name: runningTeam.Name })}
                    className="font-medium hover:underline pointer-coarse:inline-flex pointer-coarse:min-h-8 pointer-coarse:items-center"
                  >
                    {runningTeam.Name}
                  </Link>
                ) : (
                  <span className="font-medium">{runningTeam.Name}</span>
                )}
                <span className="text-muted-foreground">
                  {t("teams.members", { count: runningTeam.Members })}
                </span>
                {runningTeam.Live && <Badge variant="secondary">{t("teams.live")}</Badge>}
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
