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
  const [name, setName] = useState(
    team
      ? copy
        ? t("teams.edit.copyName", { name: team.Name })
        : team.Name
      : ""
  )
  const [members, setMembers] = useState<string[]>(team?.Members ?? [])
  const [err, setErr] = useState("")
  const [busy, setBusy] = useState(false)
  const [ask, setAsk] = useState(false)
  const lead = agents.find((a) => a.Access === "lead")

  const run = async (
    p: ReturnType<typeof teamsTeamSave> | ReturnType<typeof teamsTeamRm>
  ) => {
    setBusy(true)
    setErr("")
    try {
      await p
      onSaved()
    } catch (e) {
      setErr(errMsg(e))
    } finally {
      setBusy(false)
    }
  }
  const toggle = (id: string) =>
    setMembers((m) => (m.includes(id) ? m.filter((x) => x !== id) : [...m, id]))
  return (
    <>
      <EditorDialog
        title={
          !team
            ? t("teams.edit.newTeam")
            : copy
              ? t("teams.edit.copyOf", { name: team.Name })
              : t("teams.edit.edit", { name: team.Name })
        }
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
          <Input value={name} onChange={(e) => setName(e.target.value)} />
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
            .filter((a) => a.Access !== "lead")
            .map((a) => (
              <label key={a.ID} className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={members.includes(a.ID)}
                  onChange={() => toggle(a.ID)}
                />
                <span>{a.Name}</span>
                <span className="text-muted-foreground">· {a.Role}</span>
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
