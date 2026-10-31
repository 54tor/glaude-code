import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { errMsg } from "@/lib/apiError"
import { sameLabel } from "./editorBits"
import {
  LaunchError,
  teamsCompose,
  type Catalog,
  type Composition,
  type Team,
} from "@/lib/teams"

const MAX_MATES = 5 // même plafond que maxMates côté Go

export function TeamPicker({
  catalog,
  onLaunch,
  pickDir,
  home = "",
  onEdit,
}: {
  catalog: Catalog
  onLaunch: (ids: string[], goal: string, dir: string) => Promise<void>
  pickDir: (title: string, start: string) => Promise<string | null>
  home?: string
  onEdit?: (tm: Team) => void
}) {
  const { t } = useTranslation()
  const [sel, setSel] = useState<string[]>([])
  const [compo, setCompo] = useState<Composition | null>(null)
  const [goal, setGoal] = useState("")
  const [dir, setDir] = useState("")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState("")
  const [conflicts, setConflicts] = useState<string[]>([])
  const seq = useRef(0)

  const agentNamesById = new Map(catalog.agents.map((agent) => [agent.ID, agent.Name]))
  const toggle = (teamId: string) =>
    setSel((selectedTeamIds) => {
      if (selectedTeamIds.includes(teamId)) {
        return selectedTeamIds.filter((selectedTeamId) => selectedTeamId !== teamId)
      }
      return [...selectedTeamIds, teamId]
    })

  // une team supprimée ou renommée ailleurs ne reste pas sélectionnée
  useEffect(
    () =>
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setSel((selectedTeamIds) => {
        const isKnownTeam = (teamId: string) =>
          catalog.teams.some((team) => team.ID === teamId)
        if (selectedTeamIds.every(isKnownTeam)) return selectedTeamIds
        return selectedTeamIds.filter(isKnownTeam)
      }),
    [catalog.teams]
  )
  useEffect(() => {
    const requestSequence = ++seq.current // une réponse lente d'une sélection précédente est ignorée
    if (!sel.length) return void setCompo(null) // eslint-disable-line react-hooks/set-state-in-effect
    const isLatestRequest = () => requestSequence === seq.current
    teamsCompose(sel).then(
      (composition) => {
        if (isLatestRequest()) setCompo(composition)
      },
      () => {
        if (isLatestRequest()) setCompo(null)
      }
    )
  }, [sel])

  const launch = async () => {
    setBusy(true)
    setErr("")
    setConflicts([])
    try {
      await onLaunch(sel, goal.trim(), dir)
    } catch (launchError) {
      setErr(errMsg(launchError))
      const launchConflicts =
        launchError instanceof LaunchError ? (launchError.run?.Conflicts ?? []) : []
      setConflicts(launchConflicts)
    } finally {
      setBusy(false)
    }
  }
  const ready = sel.length > 0 && goal.trim() !== "" && dir !== "" && !busy

  return (
    <div className="space-y-6">
      <section>
        <h2 className="mb-2 text-sm font-semibold">{t("teams.title")}</h2>
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {catalog.teams.map((team) => {
            const isSelected = sel.includes(team.ID)
            return (
              <div key={team.ID} className="relative">
                <div
                  role="checkbox"
                  aria-checked={isSelected}
                  tabIndex={0}
                  onClick={() => toggle(team.ID)}
                  onKeyDown={(event) => {
                    const isToggleKey = event.key === " " || event.key === "Enter"
                    if (!isToggleKey) return
                    event.preventDefault()
                    toggle(team.ID)
                  }}
                  className={`cursor-pointer rounded-lg border p-3 pr-20 outline-none focus-visible:ring-3 focus-visible:ring-ring/80 ${isSelected ? "border-primary bg-primary/10" : "border-border hover:bg-muted"}`}
                >
                  <div className="text-sm font-medium">{team.Name}</div>
                  <div className="text-xs text-muted-foreground">
                    {team.Members.map((memberId) => agentNamesById.get(memberId) ?? memberId).join(" · ")}
                  </div>
                </div>
                {onEdit && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="absolute top-2 right-2"
                    aria-label={t("teams.edit.edit", { name: team.Name })}
                    onClick={() => onEdit(team)}
                  >
                    {t("teams.edit.modify")}
                  </Button>
                )}
              </div>
            )
          })}
        </div>
      </section>

      <section className="rounded-lg border p-3">
        <h2 className="mb-2 text-sm font-semibold">{t("teams.composition")}</h2>
        <p className="mb-2 text-sm text-muted-foreground">
          {t("teams.count", {
            teams: sel.length,
            mates: compo?.mates.length ?? 0,
          })}
        </p>
        {compo && (
          <ul className="space-y-1 text-sm">
            <li>
              <b>{compo.lead.Name}</b>{" "}
              <span className="text-muted-foreground">
                · {t("teams.lead")}
                {!sameLabel(compo.lead.Name, compo.lead.Role) &&
                  ` · ${compo.lead.Role}`}
              </span>
            </li>
            {compo.mates.map((mate) => (
              <li key={mate.ID}>
                <b>{mate.Name}</b>{" "}
                {!sameLabel(mate.Name, mate.Role) && (
                  <span className="text-muted-foreground">· {mate.Role}</span>
                )}
              </li>
            ))}
          </ul>
        )}
        {compo?.tooMany && (
          <p className="mt-2 text-sm text-destructive">
            {t("teams.tooMany", { max: MAX_MATES })}
          </p>
        )}
      </section>

      <section className="space-y-3">
        <textarea
          value={goal}
          onChange={(event) => setGoal(event.target.value)}
          placeholder={t("teams.goal")}
          aria-label={t("teams.goal")}
          rows={4}
          className="w-full rounded-lg border border-input bg-transparent p-2 text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/80"
        />
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            onClick={async () => {
              const pickedDir = await pickDir(t("teams.pickDir"), dir || home)
              setDir(pickedDir ?? dir)
            }}
          >
            {t("teams.choose")}
          </Button>
          <span className="truncate text-sm text-muted-foreground" title={dir}>
            {dir || t("teams.dir")}
          </span>
        </div>
        <Button disabled={!ready} onClick={launch}>
          {t("teams.launch")}
        </Button>
        {err && <p className="text-sm text-destructive">{err}</p>}
        {conflicts.length > 0 && (
          <div className="text-sm">
            <p className="font-medium">{t("teams.conflicts")}</p>
            <ul className="text-muted-foreground">
              {conflicts.map((conflictPath) => (
                <li key={conflictPath}>{conflictPath}</li>
              ))}
            </ul>
          </div>
        )}
      </section>
    </div>
  )
}
