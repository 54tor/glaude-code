import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Confirm } from "@/components/Confirm"
import { Input } from "@/components/ui/input"
import { errMsg } from "@/lib/apiError"
import { teamsTeamRm, teamsTeamSave, type Agent, type Team } from "@/lib/teams"
import { EditorDialog, Field } from "./editorBits"

// Le chef (Access "lead") est implicite : affiché, jamais sélectionnable.
export function TeamEditor({
  team,
  agents,
  onClose,
  onSaved,
}: {
  team: Team | null
  agents: Agent[]
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const copy = !!team?.Builtin
  const initialName = () => {
    if (!team) return ""
    if (copy) return t("teams.edit.copyName", { name: team.Name })
    return team.Name
  }
  const [name, setName] = useState(initialName)
  const [members, setMembers] = useState<string[]>(team?.Members ?? [])
  const [err, setErr] = useState("")
  const [busy, setBusy] = useState(false)
  const [ask, setAsk] = useState(false)
  const lead = agents.find((agent) => agent.Access === "lead")

  const dialogTitle = () => {
    if (!team) return t("teams.edit.newTeam")
    if (copy) return t("teams.edit.copyOf", { name: team.Name })
    return t("teams.edit.edit", { name: team.Name })
  }

  const run = async (
    pendingRequest: ReturnType<typeof teamsTeamSave> | ReturnType<typeof teamsTeamRm>
  ) => {
    setBusy(true)
    setErr("")
    try {
      await pendingRequest
      onSaved()
    } catch (requestError) {
      setErr(errMsg(requestError))
    } finally {
      setBusy(false)
    }
  }
  const toggle = (agentId: string) =>
    setMembers((currentMembers) => {
      if (currentMembers.includes(agentId)) {
        return currentMembers.filter((memberId) => memberId !== agentId)
      }
      return [...currentMembers, agentId]
    })
  return (
    <>
      <EditorDialog
        title={dialogTitle()}
        err={err}
        busy={busy}
        canSave={name.trim() !== "" && members.length > 0}
        onSave={() =>
          void run(
            teamsTeamSave({
              ID: team?.ID ?? "",
              Name: name.trim(),
              Members: members,
            })
          )
        }
        onDelete={team && !copy ? () => setAsk(true) : undefined}
        onClose={onClose}
      >
        <Field label={t("teams.edit.name")}>
          <Input value={name} onChange={(event) => setName(event.target.value)} />
        </Field>
        <fieldset className="space-y-1 text-sm">
          <legend className="mb-1 font-medium">
            {t("teams.edit.members")}
          </legend>
          {lead && (
            <p className="text-muted-foreground">
              {lead.Name} · {t("teams.lead")}
            </p>
          )}
          {agents
            .filter((agent) => agent.Access !== "lead")
            .map((agent) => (
              <label key={agent.ID} className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={members.includes(agent.ID)}
                  onChange={() => toggle(agent.ID)}
                />
                <span>{agent.Name}</span>
                <span className="text-muted-foreground">· {agent.Role}</span>
              </label>
            ))}
        </fieldset>
      </EditorDialog>
      <Confirm
        ask={
          ask && team
            ? {
                title: t("teams.edit.deleteTitle", { name: team.Name }),
                body: "",
                confirm: t("common.delete"),
                run: () => void run(teamsTeamRm(team.ID)),
              }
            : null
        }
        onClose={() => setAsk(false)}
      />
    </>
  )
}
