import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Power, Send, Square } from "lucide-react"
import { useLocal } from "@/hooks/useLocal"
import { withChoice, type Choice } from "@/lib/models"
import { ModelPicker } from "./ModelPicker"
import { Choose } from "./Choose"
import { api, type Msg } from "@/lib/api"
import { useActions } from "./actions"
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
  onSent,
  turn,
  setTurn,
  onDone,
}: {
  dir: string
  id: string
  args: string
  defaultChoice: Choice
  live: boolean
  onSent: () => void // message envoyé à la session ouverte
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

  if (live) return <LiveSend id={id} onSent={onSent} />
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

// Session ouverte dans un terminal : deux processus ne peuvent pas écrire le même transcript, donc pas de claude -p ici. Si la session
// tourne dans tmux, le message y est collé puis validé ; sinon on l'explique.
function LiveSend({ id, onSent }: { id: string; onSent: () => void }) {
  const [info, setInfo] = useState<{ ids: string[]; why: string } | null>(null)
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    let live = true
    api.sendInfo().then((r) => live && setInfo(r), () => {})
    return () => {
      live = false
    }
  }, [id])
  const ok = info?.ids.includes(id)
  if (!ok)
    return (
      <div className="flex items-center gap-3 border-t px-6 py-3 text-xs text-muted-foreground">
        <p className="min-w-0 flex-1 text-center">
          Session ouverte dans un terminal : écris-y directement (deux processus ne peuvent pas écrire le même transcript).
          {info && " Lancée dans tmux, elle accepterait un message d'ici" + (info.why ? ` (tmux : ${info.why})` : "") + "."}
        </p>
        <CloseSession id={id} />
      </div>
    )
  const send = async () => {
    if (!text.trim() || busy) return
    setBusy(true)
    const r = await api.post("/session-send", { id, text })
    setBusy(false)
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) {
      setText("")
      onSent()
    }
  }
  return (
    <div className="border-t bg-muted/20 px-4 py-3">
      <div className="mx-auto max-w-7xl space-y-2">
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && (e.preventDefault(), send())}
          placeholder="Écrire dans la session ouverte (tmux)… (Ctrl+Entrée pour envoyer)"
          rows={2}
          maxLength={32000}
          disabled={busy}
          className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-60"
          aria-label="Message à la session ouverte"
        />
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          Le texte est collé dans le terminal de la session, puis validé : si Claude répond déjà, il le prend à la suite.
          <span className="ml-auto" />
          <CloseSession id={id} />
          <Button size="sm" onClick={send} disabled={!text.trim() || busy}>
            <Send /> Envoyer à la session
          </Button>
        </div>
      </div>
    </div>
  )
}

// Ferme la session ouverte (le processus claude reçoit SIGTERM), après confirmation.
function CloseSession({ id }: { id: string }) {
  const a = useActions()
  const close = () => a.stop([id]) // confirme dans la fenêtre commune, puis met la liste à jour
  return (
    <Button size="sm" variant="ghost" onClick={close} title="Termine le processus claude de cette session ; la conversation reste reprenable">
      <Power /> Fermer la session
    </Button>
  )
}
