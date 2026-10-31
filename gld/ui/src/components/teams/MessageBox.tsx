import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { agentLabel } from "./AgentList"
import { teamSend, type AgentView } from "@/lib/teams"
import { errMsg } from "@/lib/apiError"
import { isSend } from "@/lib/utils"

export function MessageBox({ name, agents, to, setTo, queued, disabled }: { name: string; agents: AgentView[]; to: string; setTo: (n: string) => void; queued: number; disabled: boolean }) {
  const { t } = useTranslation()
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState("")
  const [wasQueued, setWasQueued] = useState(false)
  const send = async () => {
    if (busy || disabled || !text.trim()) return
    setBusy(true)
    try {
      const sendResult = await teamSend(name, to, text)
      setErr("")
      setWasQueued(sendResult.queued)
      setText("")
    } catch (sendError) {
      setErr(errMsg(sendError))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="space-y-2 border-t p-3">
      <div className="flex items-center gap-2">
        <select aria-label={t("teams.to")} value={to} onChange={(event) => setTo(event.target.value)} className="rounded-md border bg-background px-2 py-1 text-sm" disabled={disabled}>
          {agents.map((agent) => (
            <option key={agent.Name} value={agent.Name}>
              {agentLabel(agent)}
            </option>
          ))}
        </select>
        {queued > 0 ? <span className="text-xs text-muted-foreground">{t("teams.queuedN", { count: queued })}</span> : wasQueued && <span className="text-xs text-muted-foreground">{t("teams.queued")}</span>}
      </div>
      <div className="flex gap-2">
        <textarea
          value={text}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={(event) => {
            if (!isSend(event)) return
            event.preventDefault()
            void send()
          }}
          rows={2}
          disabled={disabled}
          aria-label={t("teams.message")}
          className="field-sizing-content max-h-40 min-h-12 flex-1 resize-none rounded-lg border border-input bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/80 disabled:opacity-60"
        />
        <Button size="sm" onClick={() => void send()} disabled={disabled || busy || !text.trim()}>
          {t("teams.send")}
        </Button>
      </div>
      {err && <p className="text-sm text-destructive">{err}</p>}
    </div>
  )
}
