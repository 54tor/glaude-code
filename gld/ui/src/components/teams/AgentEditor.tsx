import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Confirm } from "@/components/Confirm"
import { Input } from "@/components/ui/input"
import { models } from "@/lib/models"
import { errMsg } from "@/lib/apiError"
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
  const initialName = () => {
    if (!agent) return ""
    if (copy) return t("teams.edit.copyName", { name: agent.Name })
    return agent.Name
  }
  const [name, setName] = useState(initialName)
  const [role, setRole] = useState(agent?.Role ?? "")
  const [access, setAccess] = useState(
    agent?.Access === "read" ? "read" : "write"
  )
  const [model, setModel] = useState(agent?.Model ?? "")
  const [prompt, setPrompt] = useState(agent?.Prompt ?? "")
  const [err, setErr] = useState("")
  const [busy, setBusy] = useState(false)
  const [ask, setAsk] = useState(false)
  const promptByteCount = bytes(prompt)
  const isPromptTooLong = promptByteCount > MAX_PROMPT
  const canSave = name.trim() !== "" && role.trim() !== "" && !isPromptTooLong

  const dialogTitle = () => {
    if (!agent) return t("teams.edit.newAgent")
    if (copy) return t("teams.edit.copyOf", { name: agent.Name })
    return t("teams.edit.edit", { name: agent.Name })
  }

  const run = async (
    pendingRequest: ReturnType<typeof teamsAgentSave> | ReturnType<typeof teamsAgentRm>
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
  return (
    <>
      <EditorDialog
        title={dialogTitle()}
        err={err}
        busy={busy}
        canSave={canSave}
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
          <Input value={name} onChange={(event) => setName(event.target.value)} />
        </Field>
        <Field label={t("teams.edit.role")}>
          <Input value={role} onChange={(event) => setRole(event.target.value)} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label={t("teams.edit.access")}>
            <Select value={access} onChange={(event) => setAccess(event.target.value)}>
              <option value="read">{t("teams.edit.read")}</option>
              <option value="write">{t("teams.edit.write")}</option>
            </Select>
          </Field>
          <Field label={t("teams.edit.model")}>
            <Select value={model} onChange={(event) => setModel(event.target.value)}>
              {models().map(([modelValue, modelLabel]) => (
                <option key={modelValue} value={modelValue}>
                  {modelLabel}
                </option>
              ))}
            </Select>
          </Field>
        </div>
        <Field label={t("teams.edit.prompt")}>
          <textarea
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
            rows={8}
            className={`${fieldCls} font-mono`}
          />
        </Field>
        <p
          className={`text-right text-xs ${isPromptTooLong ? "text-destructive" : "text-muted-foreground"}`}
        >
          {promptByteCount} / {MAX_PROMPT}
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
