import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Confirm } from "@/components/Confirm"
import { Input } from "@/components/ui/input"
import { models } from "@/lib/models"
import { teamsAgentRm, teamsAgentSave, type Agent } from "@/lib/teams"
import { bytes, EditorDialog, Field, fieldCls, Select } from "./editorBits"

const MAX_PROMPT = 8000 // même plafond que le serveur (octets)

// agent null : nouvel agent. Fourni (Builtin) : le serveur crée la copie perso, le nom proposé diffère donc de l'original.
export function AgentEditor({
  agent,
  onClose,
  onSaved,
}: {
  agent: Agent | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const copy = !!agent?.Builtin
  const [name, setName] = useState(
    agent
      ? copy
        ? t("teams.edit.copyName", { name: agent.Name })
        : agent.Name
      : ""
  )
  const [role, setRole] = useState(agent?.Role ?? "")
  const [access, setAccess] = useState(
    agent?.Access === "read" ? "read" : "write"
  )
  const [model, setModel] = useState(agent?.Model ?? "")
  const [prompt, setPrompt] = useState(agent?.Prompt ?? "")
  const [err, setErr] = useState("")
  const [busy, setBusy] = useState(false)
  const [ask, setAsk] = useState(false)
  const n = bytes(prompt)

  const run = async (
    p: ReturnType<typeof teamsAgentSave> | ReturnType<typeof teamsAgentRm>
  ) => {
    setBusy(true)
    setErr("")
    const r = await p
    setBusy(false)
    if (r.ok) onSaved()
    else setErr(r.out)
  }
  return (
    <>
      <EditorDialog
        title={
          !agent
            ? t("teams.edit.newAgent")
            : copy
              ? t("teams.edit.copyOf", { name: agent.Name })
              : t("teams.edit.edit", { name: agent.Name })
        }
        err={err}
        busy={busy}
        canSave={name.trim() !== "" && role.trim() !== "" && n <= MAX_PROMPT}
        onSave={() =>
          void run(
            teamsAgentSave({
              ID: agent?.ID ?? "",
              Name: name.trim(),
              Role: role.trim(),
              Access: access,
              Model: model,
              Prompt: prompt,
            })
          )
        }
        onDelete={agent && !copy ? () => setAsk(true) : undefined}
        onClose={onClose}
      >
        <Field label={t("teams.edit.name")}>
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t("teams.edit.role")}>
          <Input value={role} onChange={(e) => setRole(e.target.value)} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label={t("teams.edit.access")}>
            <Select value={access} onChange={(e) => setAccess(e.target.value)}>
              <option value="read">{t("teams.edit.read")}</option>
              <option value="write">{t("teams.edit.write")}</option>
            </Select>
          </Field>
          <Field label={t("teams.edit.model")}>
            <Select value={model} onChange={(e) => setModel(e.target.value)}>
              {models().map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </Select>
          </Field>
        </div>
        <Field label={t("teams.edit.prompt")}>
          <textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            rows={8}
            className={`${fieldCls} font-mono`}
          />
        </Field>
        <p
          className={`text-right text-xs ${n > MAX_PROMPT ? "text-destructive" : "text-muted-foreground"}`}
        >
          {n} / {MAX_PROMPT}
        </p>
      </EditorDialog>
      <Confirm
        ask={
          ask && agent
            ? {
                title: t("teams.edit.deleteTitle", { name: agent.Name }),
                body: "",
                confirm: t("common.delete"),
                run: () => void run(teamsAgentRm(agent.ID)),
              }
            : null
        }
        onClose={() => setAsk(false)}
      />
    </>
  )
}
