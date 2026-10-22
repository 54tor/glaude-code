import { useEffect, useState } from "react"
import { cn } from "@/lib/utils"
import { LiveAgents, type Live } from "./LiveStatus"
import { toast } from "sonner"
import { Minimize2, Power, Send, Square } from "lucide-react"
import { useLocal } from "@/hooks/useLocal"
import { withChoice, type Choice } from "@/lib/models"
import { ModelPicker } from "./ModelPicker"
import { Choose } from "./Choose"
import { api, type Msg } from "@/lib/api"
import { useActions } from "./actions"
import { ImageChips, useImages } from "@/hooks/useImages"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

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
  liveState,
  onSent,
  turn,
  setTurn,
  onDone,
  onAsk,
}: {
  dir: string
  id: string
  args: string
  defaultChoice: Choice
  live: boolean
  liveState: Live
  onSent: () => void // message envoyé à la session ouverte
  turn: Turn | null
  setTurn: (f: (t: Turn | null) => Turn | null) => void
  onDone: () => void
  onAsk?: (asking: boolean) => void // Claude attend une réponse dans le terminal
}) {
  const [text, setText] = useState("")
  const [mode, setMode] = useLocal("ccChatMode", "")
  const [choice, setChoice] = useState(defaultChoice)
  const running = !!turn?.running
  const pics = useImages()
  const send = async () => {
    const prompt = [text.trim(), pics.suffix()].filter(Boolean).join("\n\n")
    if (!prompt || running) return
    setText("")
    pics.clear()
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

  if (live) return <LiveSend id={id} onSent={onSent} liveState={liveState} onAsk={onAsk} />
  return (
    <div className="border-t bg-muted/20 px-4 py-3">
      <div className="mx-auto max-w-7xl space-y-2">
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && (e.preventDefault(), send())}
          onPaste={pics.onPaste}
          onDrop={pics.onDrop}
          placeholder={running ? "Claude répond…" : "Écrire à Claude dans cette session… (Ctrl+Entrée pour envoyer ; une image se colle ou se dépose ici)"}
          disabled={running}
          rows={2}
          maxLength={32000}
          className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-60"
          aria-label="Message à Claude"
        />
        <ImageChips images={pics.images} busy={pics.busy} onRemove={pics.remove} />
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Choose value={mode} onChange={setMode} options={MODES} label="Autorisations accordées à Claude" disabled={running} className="max-w-full min-w-0" />
          <ModelPicker value={choice} onChange={setChoice} disabled={running} />
          {turn?.cost ? <span className="text-muted-foreground">coût indiqué par claude : {turn.cost.toFixed(2)} $</span> : null}
          <span className="ml-auto" />
          {running ? (
            <Button size="sm" variant="outline" onClick={stop}>
              <Square /> Arrêter
            </Button>
          ) : (
            <Button size="sm" onClick={send} disabled={!text.trim() && !pics.images.length}>
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
function LiveSend({ id, onSent, liveState, onAsk }: { id: string; onSent: () => void; liveState: Live; onAsk?: (asking: boolean) => void }) {
  const [info, setInfo] = useState<{ ids: string[]; why: string } | null>(null)
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const [sel, setSel] = useState(0)
  const [extra, setExtra] = useState<(readonly [string, string])[]>([])
  const pics = useImages()
  useEffect(() => {
    let live = true
    api.sendInfo().then((r) => live && setInfo(r), () => {})
    api.slashCommands().then((r) => live && setExtra(r.commands.map((c) => [c.cmd, c.desc] as const)), () => {})
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
  const sugg = /^\/\S*$/.test(text) ? [...SLASH.map(([c, l]) => [c, l] as const), ...extra].filter(([c]) => c.startsWith(text) && c !== text) : []
  const send = async () => {
    const full = [text.trim(), pics.suffix()].filter(Boolean).join("\n")
    if (!full || busy) return
    setBusy(true)
    const r = await api.post("/session-send", { id, text: full })
    setBusy(false)
    if (!r.ok) toast.error(r.out) // le succès se voit dans la session : pas de notification
    if (r.ok) {
      setText("")
      pics.clear()
      onSent()
    }
  }
  return (
    <div className="border-t bg-muted/20 px-4 py-3">
      <div className="mx-auto max-w-7xl space-y-2">
        <TerminalPanel id={id} onAsk={onAsk} />
        {sugg.length > 0 && (
          <ul className="max-h-48 overflow-y-auto rounded-lg border bg-popover p-1 text-sm shadow-md" role="listbox" aria-label="Commandes">
            {sugg.map(([cmd, label], i) => (
              <li
                key={cmd}
                role="option"
                aria-selected={i === sel % sugg.length}
                onMouseDown={(e) => (e.preventDefault(), setText(cmd + " "))}
                className={cn("flex cursor-pointer items-baseline gap-2 rounded px-2 py-1", i === sel % sugg.length && "bg-accent")}
              >
                <code className="text-xs">{cmd}</code>
                <span className="text-muted-foreground">{label}</span>
              </li>
            ))}
          </ul>
        )}
        <textarea
          value={text}
          onChange={(e) => (setText(e.target.value), setSel(0))}
          onKeyDown={(e) => {
            if ((e.ctrlKey || e.metaKey) && e.key === "Enter") return e.preventDefault(), send()
            if (!sugg.length) return
            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault()
              setSel((i) => (i + (e.key === "ArrowDown" ? 1 : sugg.length - 1)) % sugg.length)
            } else if (e.key === "Tab" || e.key === "Enter") {
              e.preventDefault()
              setText(sugg[sel % sugg.length][0] + " ")
            }
          }}
          onPaste={pics.onPaste}
          onDrop={pics.onDrop}
          placeholder="Écrire dans la session ouverte (tmux)… (Ctrl+Entrée pour envoyer ; une image se colle ou se dépose ici)"
          rows={2}
          maxLength={32000}
          disabled={busy}
          className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-60"
          aria-label="Message à la session ouverte"
        />
        <ImageChips images={pics.images} busy={pics.busy} onRemove={pics.remove} />
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          Le texte est collé dans le terminal de la session, puis validé : si Claude répond déjà, il le prend à la suite.
          <span className="ml-auto" />
          <LiveAgents live={liveState} />
          <SlashMenu id={id} text={text} onSent={onSent} />
          <CloseSession id={id} />
          <Button size="sm" onClick={send} disabled={(!text.trim() && !pics.images.length) || busy}>
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

// Questions du terminal. Deux formes : une liste numérotée sous une phrase (autorisation d'un outil, choix d'un plan) : le bouton envoie
// le chiffre ; une liste à sélection (« ❯ option » suivie des autres, sans numéro) : le bouton déplace la sélection puis valide.
type Opt = { label: string; keys: string[]; text?: boolean }
// Options qui attendent une réponse libre (« Type something. », « Chat about this », « No, and tell Claude what to do differently »…).
const FREE = /type something|chat about this|tell claude|something else|^\d+\.\s*other\b/i
function parsePrompt(screen: string): { question: string; options: Opt[] } | null {
  // Un vrai dialogue du terminal porte une ligne d'aide en bas (« Esc to cancel », « Enter to confirm »…) ; le texte de Claude, lui, peut
  // contenir des listes numérotées ou un « ❯ » sans que rien n'attende de réponse. On ne regarde que le bas de l'écran.
  // Les dialogues d'autorisation peuvent être encadrés (« │ ❯ 1. Yes │ ») : on retire le cadre avant d'analyser.
  const all = screen
    .split("\n")
    .map((l) => l.replace(/^(\s*)[│┃]\s?/, "$1").replace(/\s*[│┃]\s*$/, ""))
    .filter((l) => l.trim())
  const tail = all.slice(-22)
  // « esc to interrupt » est la ligne d'état de Claude en pleine réflexion : rien n'attend alors de réponse.
  if (/esc to interrupt/i.test(tail.slice(-6).join("\n"))) return null
  // Les confirmations d'opération risquée n'ont pas toujours cette ligne d'aide tout en bas : on la cherche sur les dernières lignes,
  // ou on reconnaît la question elle-même (« Do you want to proceed? »).
  const foot = tail.slice(-8).join("\n")
  if (!/(esc|escape) to (cancel|exit|go back)|enter to (select|confirm)|to navigate|tab to amend|ready to submit|submit your answers|do you want to|would you like to|proceed\?/i.test(foot)) return null
  const lines = tail
  const nums: { n: string; label: string; at: number }[] = []
  lines.forEach((l, i) => {
    const m = /^\s*(?:[❯>›]\s*)?([1-9])[.)]\s+(\S.*)$/.exec(l)
    if (m) nums.push({ n: m[1], label: m[2].trim(), at: i })
  })
  const above = (at: number) => {
    let q = ""
    for (let i = at - 1; i >= 0 && i >= at - 6; i--) {
      const t = lines[i].trim()
      if (t) {
        q = t
        if (/[?:]$/.test(t)) break
      }
    }
    return q
  }
  if (nums.length >= 2 && nums[0].n === "1") return { question: above(nums[0].at), options: nums.map((o) => ({ label: `${o.n}. ${o.label}`, keys: [o.n], text: FREE.test(o.label) })) }
  const sel = lines.findIndex((l) => /^\s{0,3}❯ \S/.test(l))
  if (sel < 0) return null
  // Le texte des options est aligné sur celui de la ligne sélectionnée ; les autres lignes du dialogue ne le sont pas.
  const col = lines[sel].indexOf("❯") + 2
  const item = (l: string) => l.length > col && l.slice(0, col).trim() === "" && l[col] !== " " && !/^[─━│┌└╭╰]/.test(l[col])
  let from = sel
  while (from > 0 && item(lines[from - 1])) from--
  let to = sel
  while (to + 1 < lines.length && item(lines[to + 1])) to++
  if (to - from < 1 || to - from > 8) return null
  const options: Opt[] = []
  for (let i = from; i <= to; i++) {
    const d = i - sel
    const label = lines[i].slice(col).trim()
    options.push({ label, keys: [...Array(Math.abs(d)).fill(d < 0 ? "Up" : "Down"), "Enter"], text: FREE.test(label) })
  }
  return { question: above(from), options }
}

// L'écran du terminal de la session (tmux) relu toutes les 1,5 s, pour proposer en boutons les options détectées.
function TerminalPanel({ id, onAsk }: { id: string; onAsk?: (asking: boolean) => void }) {
  const [screen, setScreen] = useState("")
  useEffect(() => {
    let live = true
    const load = () => api.screen(id).then((r) => live && r.ok && setScreen(r.text ?? ""), () => {})
    load()
    const t = setInterval(() => document.visibilityState === "visible" && load(), 1500)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [id])
  // Une ou plusieurs touches, l'une après l'autre (déplacer la sélection puis valider).
  const press = async (...keys: string[]) => {
    for (const key of keys) {
      const r = await api.post("/session-key", { id, key })
      if (!r.ok) return void toast.error(r.out)
      await new Promise((x) => setTimeout(x, 80))
    }
    api.screen(id).then((s) => s.ok && setScreen(s.text ?? ""))
  }
  const prompt = parsePrompt(screen)
  const [typing, setTyping] = useState<string | null>(null) // option à réponse libre en cours de saisie
  const [answer, setAnswer] = useState("")
  const choose = async (o: Opt) => {
    if (!o.text) return press(...o.keys)
    setTyping(o.label)
    await press(...(o.keys[o.keys.length - 1] === "Enter" ? o.keys.slice(0, -1) : o.keys)) // sélectionne l'option sans valider
  }
  const reply = async () => {
    const t = answer.trim()
    if (!t) return
    const r = await api.post("/session-send", { id, text: t })
    if (!r.ok) return void toast.error(r.out)
    setAnswer("")
    setTyping(null)
  }
  const asking = !!prompt
  useEffect(() => {
    onAsk?.(asking)
    return () => onAsk?.(false)
  }, [asking, onAsk])
  return (
    <div className="space-y-2">
      {prompt && (
        <div className="rounded-lg border border-amber-500/50 bg-amber-500/10 px-3 py-2 text-sm">
          <p className="mb-1.5 font-medium">{prompt.question || "Réponse attendue"}</p>
          <div className="flex flex-col items-stretch gap-1.5">
            {prompt.options.map((o) => (
              <div key={o.label} className="contents">
                <Button size="sm" variant={typing === o.label ? "secondary" : "outline"} className="h-auto min-h-8 justify-start py-1.5 text-left whitespace-normal" onClick={() => choose(o)} title={`Touches : ${o.keys.join(", ")}`}>
                  {o.label}
                </Button>
                {typing === o.label && (
                  <div className="flex items-end gap-1.5">
                    <textarea
                      autoFocus
                      value={answer}
                      onChange={(e) => setAnswer(e.target.value)}
                      onKeyDown={(e) => (e.ctrlKey || e.metaKey || !e.shiftKey) && e.key === "Enter" && (e.preventDefault(), reply())}
                      rows={2}
                      maxLength={32000}
                      placeholder="Ta réponse (Entrée pour envoyer, Maj+Entrée pour un saut de ligne)"
                      className="field-sizing-content max-h-40 min-h-12 flex-1 resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
                      aria-label="Réponse à la question du terminal"
                    />
                    <Button size="sm" onClick={reply} disabled={!answer.trim()}>
                      <Send /> Envoyer
                    </Button>
                  </div>
                )}
              </div>
            ))}
            <Button size="sm" variant="ghost" className="justify-start" onClick={() => press("Escape")}>
              Échap
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

// Commandes de Claude Code envoyées telles quelles à la session (elles s'exécutent dans son terminal). /compact accepte des consignes :
// ce qui est écrit dans le champ s'y ajoute (« /compact garde les décisions d'architecture »).

const SLASH = [
  ["/compact", "Compacter le contexte", "Résume la conversation pour libérer du contexte ; le texte du champ sert de consigne au résumé"],
  ["/context", "Voir le contexte", "Répartition du contexte (instructions, mémoire, outils, messages)"],
  ["/cost", "Voir le coût", "Coût et durée de la session"],
  ["/usage", "Voir les limites d'usage", "Met à jour le relevé des limites du compte"],
  ["/clear", "Effacer la conversation", "Vide le contexte (la conversation reste dans le transcript)"],
  ["/model", "Changer de modèle", "Ouvre le choix du modèle : réponds avec les boutons qui apparaissent au-dessus du champ"],
  ["/mcp", "Serveurs MCP", "État des serveurs MCP"],
  ["/status", "État de Claude Code", "Version, compte, modèle, connexion"],
  ["/memory", "Mémoire", "Fichiers CLAUDE.md chargés"],
  ["/permissions", "Permissions", "Règles d'autorisation en vigueur"],
  ["/agents", "Sous-agents", "Liste des agents disponibles"],
  ["/rewind", "Revenir en arrière", "Rétablit la conversation ou le code à un point précédent"],
  ["/export", "Exporter", "Exporte la conversation"],
  ["/doctor", "Diagnostic", "Vérifie l'installation"],
  ["/help", "Aide", "Liste des commandes"],
] as const

function SlashMenu({ id, text, onSent }: { id: string; text: string; onSent: () => void }) {
  const run = async (cmd: string) => {
    const full = cmd === "/compact" && text.trim() ? `${cmd} ${text.trim().replace(/\s+/g, " ")}` : cmd
    const r = await api.post("/session-send", { id, text: full })
    if (!r.ok) toast.error(r.out)
    else onSent()
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm" variant="ghost" title="Commandes de Claude Code">
          <Minimize2 /> Commandes
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        {SLASH.map(([cmd, label, hint]) => (
          <DropdownMenuItem key={cmd} onSelect={() => run(cmd)} className="flex-col items-start gap-0">
            <span>
              {label} <code className="text-xs text-muted-foreground">{cmd}</code>
            </span>
            <span className="text-xs text-muted-foreground">{hint}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
