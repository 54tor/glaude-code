import { useState } from "react"
import { toast } from "sonner"
import { Send, Square } from "lucide-react"
import { useLocal } from "@/hooks/useLocal"
import { withChoice, type Choice } from "@/lib/models"
import { ModelPicker } from "./ModelPicker"
import { Choose } from "./Choose"
import { api, type Msg } from "@/lib/api"
import { Button } from "@/components/ui/button"

// Modes d'autorisation de claude -p : sans terminal, Claude ne peut pas demander la permission.
const MODES = [
  ["", "Lecture seule : écrire et lancer des commandes est refusé"],
  ["plan", "Plan : propose sans rien modifier"],
  ["acceptEdits", "Accepter les modifications de fichiers"],
  ["auto", "Auto : Claude décide de ce qu'il peut faire"],
] as const

export type Turn = { id: string; msgs: Msg[]; running: boolean; error?: string; cost?: number }

// Écrire dans la session depuis le navigateur : claude -p --resume, réponse relayée au fil de l'eau (NDJSON).
export function Composer({
  dir,
  id,
  args,
  defaultChoice,
  live,
  turn,
  setTurn,
  onDone,
}: {
  dir: string
  id: string
  args: string
  defaultChoice: Choice
  live: boolean
  turn: Turn | null
  setTurn: (f: (t: Turn | null) => Turn | null) => void
  onDone: () => void
}) {
  const [text, setText] = useState("")
  const [mode, setMode] = useLocal("ccChatMode", "")
  const [choice, setChoice] = useState(defaultChoice)
  const running = !!turn?.running
  const send = async () => {
    const prompt = text.trim()
    if (!prompt || running) return
    setText("")
    setTurn(() => ({ id, msgs: [{ r: "user", t: prompt }], running: true }))
    const fail = (error: string) => setTurn((t) => t && { ...t, running: false, error })
    let res: Response
    try {
      res = await fetch("/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ dir, id, prompt, args: withChoice(args, choice), mode }),
      })
    } catch (e) {
      return fail(e instanceof Error ? e.message : String(e))
    }
    if (!res.ok || !res.body) return fail(((await res.json().catch(() => ({}))) as { error?: string }).error ?? `HTTP ${res.status}`)
    const reader = res.body.getReader()
    const dec = new TextDecoder()
    let buf = ""
    for (;;) {
      const { value, done } = await reader.read()
      buf += dec.decode(value, { stream: !done })
      const lines = buf.split("\n")
      buf = lines.pop() ?? ""
      for (const l of lines) {
        if (!l.trim()) continue
        const e = JSON.parse(l) as { msgs?: Msg[]; done?: boolean; ok?: boolean; error?: string; cost?: number; note?: string }
        if (e.note) toast.info(e.note)
        setTurn(
          (t) =>
            t && {
              ...t,
              msgs: e.msgs ? [...t.msgs, ...e.msgs] : t.msgs,
              running: e.done ? false : t.running,
              error: e.done && !e.ok ? e.error || "Échec" : t.error,
              cost: e.cost ?? t.cost,
            }
        )
      }
      if (done) break
    }
    setTurn((t) => t && { ...t, running: false })
    onDone()
  }
  const stop = () => api.post("/chat-stop", { id })

  if (live)
    return (
      <p className="border-t px-6 py-3 text-center text-xs text-muted-foreground">
        Session ouverte dans un terminal : écris-y directement (deux processus ne peuvent pas écrire le même transcript).
      </p>
    )
  return (
    <div className="border-t bg-muted/20 px-4 py-3">
      <div className="mx-auto max-w-7xl space-y-2">
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && (e.preventDefault(), send())}
          placeholder={running ? "Claude répond…" : "Écrire à Claude dans cette session… (Ctrl+Entrée pour envoyer)"}
          disabled={running}
          rows={2}
          maxLength={32000}
          className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-60"
          aria-label="Message à Claude"
        />
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Choose value={mode} onChange={setMode} options={MODES} label="Autorisations accordées à Claude" disabled={running} />
          <ModelPicker value={choice} onChange={setChoice} disabled={running} />
          {turn?.cost ? <span className="text-muted-foreground">coût indiqué par claude : {turn.cost.toFixed(2)} $</span> : null}
          <span className="ml-auto" />
          {running ? (
            <Button size="sm" variant="outline" onClick={stop}>
              <Square /> Arrêter
            </Button>
          ) : (
            <Button size="sm" onClick={send} disabled={!text.trim()}>
              <Send /> Envoyer
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
