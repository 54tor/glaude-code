import { useEffect, useRef, useState, type ReactNode } from "react"
import { usePending, refreshPending } from "@/hooks/usePending"
import { Trans, useTranslation } from "react-i18next"
import { i18n } from "@/i18n"
import { cn, copyText, isSend, keyHint } from "@/lib/utils"
import { modeOfScreen, parsePrompt, type Opt } from "@/lib/prompt"
import { type Live } from "./LiveStatus"
import { toast } from "sonner"
import { Bug, Copy, Minimize2, Power, Snowflake, Send, Square, SquareCheck, X } from "lucide-react"
import { useLocal } from "@/hooks/useLocal"
import { draftOf, setDraft, useDraft } from "@/hooks/useDraft"
import { efforts, models, withChoice, type Choice } from "@/lib/models"
import { ModelPicker } from "./ModelPicker"
import { useMention } from "./Mention"
import { Choose } from "./Choose"
import { api, type Msg, type QItem } from "@/lib/api"
import { useActions } from "./actions"
import { AttachButton, ImageChips, useImages } from "@/hooks/useImages"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { readNdjson } from "@/lib/ndjson"
import { authFetch, useCan } from "@/lib/auth"

// File serveur : un refus (pleine, texte refusé) s'affiche et laisse le brouillon intact.
async function addToQueue(id: string, text: string): Promise<boolean> {
  const r = await api.queueAdd(id, text)
  if (!r.ok) toast.error(r.out)
  refreshPending()
  return r.ok
}
// Retirer un message le remet dans la zone de saisie, à la suite de ce qui s'y trouve déjà.
async function removeFromQueue(id: string, q: QItem) {
  const r = await api.queueRm(id, q.id)
  refreshPending()
  if (!r.ok) return void toast.error(r.out)
  const d = draftOf(id)
  const back = r.text ?? q.text
  setDraft(id, { text: d.text.trim() ? `${d.text}\n${back}` : back })
}
async function retryItem(id: string, q: QItem) {
  const r = await api.queueRetry(id, q.id)
  refreshPending()
  if (!r.ok) toast.error(r.out)
}
// Une ligne de la file. Un envoi incertain se tranche (renvoyer ou retirer) ; un envoi en cours ne se retire plus.
function QueueRow({ id, q, i, label }: { id: string; q: QItem; i: number; label?: ReactNode }) {
  const { t } = useTranslation()
  return (
    <li className="flex items-center gap-2 rounded-md border border-dashed px-2 py-1 text-muted-foreground">
      <span className="shrink-0 tabular-nums">{i + 1}.</span>
      <Author q={q} />
      <span className="min-w-0 flex-1 truncate" title={q.text}>
        {q.text}
      </span>
      {q.state === "uncertain" ? (
        <>
          <span className="shrink-0 text-amber-600 dark:text-amber-400">{t("conversation.composer.sendUncertain")}</span>
          <Button size="xs" variant="outline" onClick={() => void retryItem(id, q)}>
            {t("conversation.composer.resend")}
          </Button>
        </>
      ) : q.state === "sending" ? (
        <span className="shrink-0">{t("conversation.composer.sending")}</span>
      ) : (
        label && <span className="shrink-0">{label}</span>
      )}
      {q.state !== "sending" && (
        <Button size="icon-xs" variant="ghost" aria-label={t("conversation.composer.dequeue")} onClick={() => void removeFromQueue(id, q)}>
          <X />
        </Button>
      )}
    </li>
  )
}
function Author({ q }: { q: QItem }) {
  const { t } = useTranslation()
  return q.author === "guest" ? <span className="shrink-0 rounded bg-muted px-1 text-2xs uppercase">{t("conversation.composer.guest")}</span> : null
}

// Modes d'autorisation de claude -p : sans terminal, Claude ne peut pas demander la permission.
const modes = () =>
  [
    ["", i18n.t("conversation.composer.modes.readOnly")],
    ["plan", i18n.t("conversation.composer.modes.plan")],
    ["acceptEdits", i18n.t("conversation.composer.modes.acceptEdits")],
    ["auto", i18n.t("conversation.composer.modes.auto")],
  ] as const

// Le champ de saisie garde le focus après chaque envoi, même quand il est remplacé par l'autre (chat de gld ↔ session ouverte) :
// le nouveau champ prend le focus à son montage s'il vient d'être demandé. Un seul composeur est affiché à la fois.
let field: HTMLTextAreaElement | null = null
let focusUntil = 0
const fieldRef = (el: HTMLTextAreaElement | null) => {
  field = el
  if (el && Date.now() < focusUntil) el.focus()
}
const keepFocus = () => {
  focusUntil = Date.now() + 8000
  field?.focus()
  requestAnimationFrame(() => field?.focus()) // après le rendu qui suit l'envoi
}

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
  onWrite,
  extra,
  compactHint,
  coldHint,
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
  onDone: () => void | Promise<void>
  onAsk?: (asking: boolean) => void // Claude attend une réponse dans le terminal
  onWrite?: () => void // l'utilisateur envoie un message : la vue descend
  extra?: ReactNode // commandes de la vue, affichées au-dessus de la zone de saisie
  compactHint?: string | null // conseil de compacter, avec le bouton, au-dessus de la saisie d'une session ouverte
  coldHint?: ColdHintProps | null // cache expiré : conseil de repartir sur une nouvelle session
}) {
  const { t } = useTranslation()
  const exec = useCan()("exec")
  const { text } = useDraft(id)
  const queue = usePending(id)
  const setText = (t: string) => setDraft(id, { text: t })
  const mention = useMention(text, setText, id)
  const [mode, setMode] = useLocal("ccChatMode", "")
  const [choice, setChoice] = useState(defaultChoice)
  const running = !!turn?.running
  // Le claude -p lancé par gld pour ce tour apparaît lui-même comme « session ouverte » (claude agents) pendant qu'il répond, et
  // jusqu'au prochain relevé : tant que le tour est le nôtre, on ne bascule pas vers l'envoi à une session ouverte dans un terminal.
  const [own, setOwn] = useState(false)
  const pics = useImages()
  // File d'attente : les messages écrits pendant que Claude répond partent l'un après l'autre à la fin du tour (sauf échec ou arrêt).
  // Brouillon et file vivent dans un magasin par session (useDraft) : fermer la fenêtre ne les perd pas, et l'envoi des messages
  // en file continue même fenêtre fermée.
  const adding = useRef(false) // un ajout est en vol : deux Entrée rapprochées ne mettent pas le texte deux fois en file
  const enqueue = async (prompt: string) => {
    adding.current = true
    try {
      return await addToQueue(id, prompt)
    } finally {
      adding.current = false
    }
  }
  const stopped = useRef(false) // arrêt demandé par l'utilisateur : le message était bien parti
  const send = async () => {
    const rn = /^\/rename\s+(\S[\s\S]*)$/.exec(text.trim())
    if (rn) {
      setText("")
      api.post("/rename", { dir, id, title: rn[1] }).then((r) => (r.ok ? toast.success(r.out) : toast.error(r.out)), () => toast.error(t("conversation.composer.failed")))
      return void onDone()
    }
    const prompt = [text.trim(), pics.suffix()].filter(Boolean).join("\n\n")
    if (!prompt) return
    keepFocus()
    if (running) {
      if (adding.current) return
      if (!(await enqueue(prompt))) return // refusé : le brouillon reste
      if (draftOf(id).text === text) setText("")
      return void pics.clear()
    }
    setText("")
    pics.clear()
    onWrite?.()
    if (!(await submit(prompt))) {
      // non délivré : jamais perdu, il revient dans le brouillon
      const d = draftOf(id).text
      if (!d.includes(prompt)) setText(d.trim() ? `${d}\n${prompt}` : prompt)
    }
  }
  // Envoyer la tête de la file suspendue : son texte passe d'abord dans le brouillon (enregistré), puis la file le lâche, puis
  // l'envoi ; le brouillon d'origine n'est rendu qu'une fois l'envoi réussi, donc fermer l'onglet n'efface jamais le texte.
  const sendHead = async (head: QItem) => {
    if (head.state !== "queued") return
    const before = draftOf(id).text
    const merged = before.trim() ? `${before}\n${head.text}` : head.text
    setText(merged)
    const r = await api.queueRm(id, head.id)
    refreshPending()
    if (!r.ok) {
      toast.error(r.out)
      if (draftOf(id).text === merged) setText(before)
      return
    }
    onWrite?.()
    if ((await submit(r.text ?? head.text)) && draftOf(id).text === merged) setText(before)
  }
  const submit = async (prompt: string): Promise<boolean> => {
    setOwn(true)
    setTurn(() => ({ id, msgs: [{ r: "user", t: prompt }], running: true }))
    const fail = (error: string) => (setOwn(false), setTurn((t) => t && { ...t, running: false, error }), false)
    stopped.current = false
    let failed = false
    let res: Response
    try {
      res = await authFetch("/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ dir, id, prompt, args: withChoice(args, choice), mode }),
      })
    } catch (e) {
      return fail(e instanceof Error ? e.message : String(e))
    }
    if (!res.ok || !res.body) return fail(((await res.json().catch(() => ({}))) as { error?: string }).error ?? `HTTP ${res.status}`)
    try {
      for await (const e of readNdjson<{ msgs?: Msg[]; done?: boolean; ok?: boolean; error?: string; cost?: number; note?: string }>(res.body)) {
        if (e.note) toast.info(e.note)
        if (e.done && !e.ok) failed = true
        setTurn(
          (t) =>
            t && {
              ...t,
              msgs: e.msgs ? [...t.msgs, ...e.msgs] : t.msgs,
              running: e.done ? false : t.running,
              error: e.done && !e.ok ? e.error || i18n.t("conversation.composer.failed") : t.error,
              cost: e.cost ?? t.cost,
            }
        )
      }
    } catch (e) {
      return fail(e instanceof Error ? e.message : String(e)) // connexion coupée en cours de réponse
    }
    const delivered = !failed || stopped.current // échec de claude (pas un arrêt demandé) : on ne sait pas si le message est parti
    setTurn((t) => t && { ...t, running: false })
    await onDone() // le transcript est relu avant le tour suivant, qui efface sinon son propre affichage
    setTimeout(() => setOwn(false), 30_000)
    return delivered
  }
  const stop = () => ((stopped.current = true), api.post("/chat-stop", { id }))

  if (live && !running && !own) return <LiveSend id={id} onSent={onSent} liveState={liveState} onAsk={onAsk} bar={extra} compactHint={compactHint} coldHint={coldHint} />
  return (
    <div className="border-t bg-muted/20 px-4 py-3">
      <div className="mx-auto max-w-7xl space-y-2">
        {mention.list}
        {coldHint && <ColdHint {...coldHint} />}
        {extra && <div className="flex justify-end">{extra}</div>}
        <textarea
          ref={fieldRef}
          value={text}
          onChange={(e) => (setText(e.target.value), mention.track(e.target))}
          onKeyDown={(e) => !mention.onKey(e) && isSend(e) && (e.preventDefault(), send())}
          onPaste={pics.onPaste}
          onDrop={pics.onDrop}
          placeholder={keyHint(running ? t("conversation.composer.placeholderRunning") : t("conversation.composer.placeholder"))}
          rows={2}
          maxLength={32000}
          className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-60"
          aria-label={t("conversation.composer.messageAria")}
        />
        <ImageChips images={pics.images} busy={pics.busy} onRemove={pics.remove} />
        {queue.length > 0 && (
          <ul className="space-y-1 text-xs" aria-label={t("conversation.composer.queueAria")}>
            {queue.map((q, i) => (
              <QueueRow key={q.id} id={id} q={q} i={i} />
            ))}
            {!running && queue[0].state === "queued" && (
              <li className="flex items-center gap-2 text-muted-foreground">
                {t("conversation.composer.queueSuspended")}
                <Button
                  size="xs"
                  variant="outline"
                  onClick={() => void sendHead(queue[0])}
                >
                  {t("conversation.composer.sendNext")}
                </Button>
              </li>
            )}
          </ul>
        )}
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Choose value={mode} onChange={setMode} options={modes()} label={t("conversation.composer.permissions")} disabled={running} className="max-w-full min-w-0" />
          <ModelPicker value={choice} onChange={setChoice} disabled={running} />
          {turn?.cost ? <span className="text-muted-foreground">{t("conversation.composer.cost", { cost: turn.cost.toFixed(2) })}</span> : null}
          <span className="ml-auto max-md:hidden" />
          <AttachButton onPick={pics.pick} disabled={running} />
          {running ? (
            <Button size="sm" variant="outline" onClick={stop}>
              <Square /> {t("conversation.composer.stop")}
            </Button>
          ) : (
            <Button size="sm" onClick={send} disabled={(!text.trim() && !pics.images.length) || !exec}>
              <Send /> <span className="max-md:sr-only">{t("conversation.composer.send")}</span>
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

export type ColdHintProps = { text: string; onNew: () => void }

// Cache des prompts expiré : reprendre la session réécrit tout son contexte au tarif plein.
function ColdHint({ text, onNew }: ColdHintProps) {
  const { t } = useTranslation()
  return (
    <p role="status" className="flex items-center gap-2 rounded-md bg-info/10 px-3 py-1 text-xs text-info">
      <Snowflake className="size-3.5 shrink-0" aria-hidden />
      <span className="min-w-0 flex-1">{text}</span>
      <Button size="xs" variant="outline" title={t("conversation.composer.coldNewTitle")} onClick={onNew}>
        {t("conversation.composer.coldNew")}
      </Button>
    </p>
  )
}

// Session ouverte dans un terminal : deux processus ne peuvent pas écrire le même transcript, donc pas de claude -p ici. Si la session
// tourne dans tmux, le message y est collé puis validé ; sinon on l'explique.
function LiveSend({ id, onSent, liveState, onAsk, bar, compactHint, coldHint }: { id: string; onSent: () => void; liveState: Live; onAsk?: (asking: boolean) => void; bar?: ReactNode; compactHint?: string | null; coldHint?: ColdHintProps | null }) {
  const { t } = useTranslation()
  const a = useActions()
  const exec = useCan()("exec")
  const [info, setInfo] = useState<{ ids: string[]; why: string } | null>(null)
  const { text } = useDraft(id) // gardé par session : fermer la fenêtre ou un échec d'envoi ne le perd pas
  const queue = usePending(id)
  const setText = (t: string) => setDraft(id, { text: t })
  const mention = useMention(text, setText, id)
  const adding = useRef(false)
  const enqueue = async (prompt: string) => {
    adding.current = true
    try {
      return await addToQueue(id, prompt)
    } finally {
      adding.current = false
    }
  }
  const [busy, setBusy] = useState(false)
  const [asking, setAsking] = useState(false) // une question attend une réponse dans le terminal : rien ne s'y écrit tant qu'elle est ouverte
  const working = !!liveState.st?.thinking
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
  const sugg =/^\/\S*$/.test(text) ? [...slash().map(([c, l]) => [c, l] as const), ...extra].filter(([c]) => c.startsWith(text) && c !== text) : []
  // Un message écrit pendant que Claude travaille, ou qu'une question attend, est mis en file : il part dès que la session attend
  // (jamais dans une question, où il répondrait à sa place).
  const post = async (full: string) => {
    setBusy(true)
    const r = await api.post("/session-send", { id, text: full }, 20_000)
    setBusy(false)
    if (!r.ok) toast.error(r.out) // le succès se voit dans la session : pas de notification
    return r.ok
  }
  const send = async () => {
    const full = [text.trim(), pics.suffix()].filter(Boolean).join("\n")
    if (!full || busy) return
    keepFocus()
    if (working || asking) {
      if (adding.current) return
      if (!(await enqueue(full))) return
      if (draftOf(id).text === text) setText("")
      pics.clear()
      return
    }
    if (await post(full)) {
      if (draftOf(id).text === text) setText("") // pas un texte tapé entre-temps
      pics.clear()
      onSent()
      keepFocus()
    }
  }
  if (!ok)
    return (
      <div className="flex items-center gap-3 border-t px-6 py-3 text-xs text-muted-foreground">
        <p className="min-w-0 flex-1 text-center">
          {a.background.has(id) ? (
            t("conversation.composer.backgroundOnly")
          ) : (
            <>
              <Trans i18nKey="conversation.composer.noTmux" components={[<code key="0" />]} />
              {info && " " + t("conversation.composer.relaunch", { why: info.why ? ` (${info.why})` : "" })}
            </>
          )}
        </p>
        {bar}
      </div>
    )
  return (
    <div className="border-t bg-muted/20 px-4 py-3">
      <div className="mx-auto max-w-7xl space-y-2">
        {compactHint && (
          <p role="status" className="flex items-center gap-2 rounded-md bg-warning/10 px-3 py-1 text-xs text-warning">
            <Minimize2 className="size-3.5 shrink-0" aria-hidden />
            <span className="min-w-0 flex-1">{compactHint}</span>
            <Button size="xs" variant="outline" disabled={!exec} title={t("conversation.composer.compactTitle")} onClick={() => sendSlash(id, "/compact", text, onSent)}>
              {t("conversation.composer.compact")}
            </Button>
          </p>
        )}
        {coldHint && <ColdHint {...coldHint} />}
        <TerminalPanel id={id} onAsk={(a) => (setAsking(a), onAsk?.(a))} onSent={onSent} extra={bar} />
        {sugg.length > 0 && (
          <ul className="max-h-48 overflow-y-auto rounded-lg border bg-popover p-1 text-sm shadow-md" role="listbox" aria-label={t("conversation.composer.commandsAria")}>
            {sugg.map(([cmd, label], i) => (
              <li
                key={cmd}
                role="option"
                ref={(el) => {
                  if (el && i === sel % sugg.length) el.scrollIntoView({ block: "nearest" })
                }}
                aria-selected={i === sel % sugg.length}
                onMouseDown={(e) => (e.preventDefault(), setText(cmd + " "))}
                className={cn("flex cursor-pointer items-baseline gap-2 rounded-md px-2 py-1", i === sel % sugg.length && "bg-accent")}
              >
                <code className="text-xs">{cmd}</code>
                <span className="text-muted-foreground">{label}</span>
              </li>
            ))}
          </ul>
        )}
        {mention.list}
        <textarea
          ref={fieldRef}
          value={text}
          onChange={(e) => (setText(e.target.value), setSel(0), mention.track(e.target))}
          onKeyDown={(e) => {
            if (mention.onKey(e)) return
            if (!sugg.length) return isSend(e) ? (e.preventDefault(), void send()) : undefined
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
          placeholder={keyHint(t("conversation.composer.sessionPlaceholder"))}
          rows={2}
          maxLength={32000}
          className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          aria-label={t("conversation.composer.sessionAria")}
        />
        <ImageChips images={pics.images} busy={pics.busy} onRemove={pics.remove} />
        {queue.length > 0 && (
          <ul className="space-y-1 text-xs" aria-label={t("conversation.composer.queueAria")}>
            {queue.map((q, i) => (
              <QueueRow key={q.id} id={id} q={q} i={i} label={i === 0 ? (asking ? t("conversation.composer.afterQuestion") : working ? t("conversation.composer.whenIdle") : undefined) : undefined} />
            ))}
          </ul>
        )}
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <LiveModeButton id={id} disabled={working || asking || busy} />
          <Choose
            value={models().find(([a]) => a && liveState.st?.model?.toLowerCase().includes(a))?.[0] ?? ""}
            onChange={async (m) => m && (await post(`/model ${m}`)) && onSent()}
            options={[["", t("conversation.composer.modelEllipsis")], ...models().slice(1)]}
            label={t("conversation.composer.sessionModel")}
            disabled={working || asking || busy}
          />
          <Choose
            value=""
            onChange={async (e) => e && (await post(`/effort ${e}`)) && onSent()}
            options={[["", t("conversation.composer.effortEllipsis")], ...efforts().slice(1)]}
            label={t("conversation.composer.sessionEffort")}
            disabled={working || asking || busy}
          />
          <span className="ml-auto" />
          <SlashMenu id={id} text={text} onSent={onSent} />
          <AttachButton onPick={pics.pick} />
          {liveState.st?.thinking && (
            <Button
              size="sm"
              variant="outline"
              title={t("conversation.composer.interruptTitle")}
              onClick={async () => {
                const r = await api.post("/session-key", { id, key: "Escape" })
                if (!r.ok) toast.error(r.out)
              }}
            >
              <Square className="max-md:hidden" /> {t("conversation.composer.interrupt")}
            </Button>
          )}
          <Button size="sm" onClick={send} disabled={(!text.trim() && !pics.images.length) || busy || !exec}>
            <Send /> {t("conversation.composer.sendToSession")}
          </Button>
        </div>
      </div>
    </div>
  )
}

// Mode de permission de la session ouverte : lu sur l'écran tmux, un clic envoie Maj+Tab (mode suivant).
function LiveModeButton({ id, disabled }: { id: string; disabled: boolean }) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<ReturnType<typeof modeOfScreen>>(null)
  const read = () => api.screen(id).then((r) => r.ok && setMode(modeOfScreen(r.text ?? "")), () => {})
  useEffect(() => {
    read()
    const iv = setInterval(() => document.visibilityState === "visible" && read(), 3000)
    return () => clearInterval(iv)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])
  const next = async () => {
    const r = await api.post("/session-key", { id, key: "ShiftTab" })
    if (!r.ok) return void toast.error(r.out)
    setTimeout(read, 400)
  }
  return (
    <Button size="sm" variant="outline" disabled={disabled} onClick={next} title={t("conversation.composer.liveModeTitle")}>
      {t("conversation.composer.liveMode", { mode: t(`conversation.composer.liveModes.${mode ?? "unknown"}`) })}
    </Button>
  )
}

// Ferme la session ouverte (le processus claude reçoit SIGTERM), après confirmation.
export function CloseSession({ id, onClosed }: { id: string; onClosed?: () => void }) {
  const { t } = useTranslation()
  const a = useActions()
  const close = () => a.stop([id], onClosed) // confirme dans la fenêtre commune, puis met la liste à jour
  return (
    <Button size="sm" variant="ghost" onClick={close} title={t("conversation.composer.closeTitle")}>
      <Power /> {t("conversation.composer.close")}
    </Button>
  )
}

// L'écran du terminal de la session (tmux) relu toutes les 1,5 s, pour proposer en boutons les options détectées.
function TerminalPanel({ id, onAsk, onSent, extra }: { id: string; onAsk?: (asking: boolean) => void; onSent: () => void; extra?: ReactNode }) {
  const { t } = useTranslation()
  const [screen, setScreen] = useState("")
  const busy = useRef(false) // une touche vient d'être envoyée : l'écran affiché peut être périmé
  useEffect(() => {
    let live = true
    const load = () => !busy.current && api.screen(id).then((r) => live && !busy.current && r.ok && setScreen(r.text ?? ""), () => {})
    load()
    const t = setInterval(() => document.visibilityState === "visible" && load(), 1500)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [id])
  // Une ou plusieurs touches, l'une après l'autre (déplacer la sélection puis valider). Les boutons restent bloqués tant que l'écran
  // n'a pas changé (ou 4 s) : cliquer sur une question déjà quittée désynchroniserait la sélection.
  const [pressing, setPressing] = useState(false)
  const [debug, setDebug] = useState(false)
  const press = async (...keys: string[]) => {
    if (busy.current) return
    busy.current = true
    setPressing(true)
    const before = screen
    try {
      for (const key of keys) {
        const r = await api.post("/session-key", { id, key })
        if (!r.ok) return void toast.error(r.out)
        await new Promise((x) => setTimeout(x, 120))
      }
      for (let i = 0; i < 10; i++) {
        await new Promise((x) => setTimeout(x, 400))
        const s = await api.screen(id)
        if (s.ok) {
          setScreen(s.text ?? "")
          if ((s.text ?? "") !== before) {
            if (!parsePrompt(s.text ?? "")) onSent() // dialogue terminé : la suite arrive, on la suit tout de suite
            break
          }
        }
      }
    } finally {
      busy.current = false
      setPressing(false)
    }
  }
  const prompt = parsePrompt(screen)
  const [typing, setTyping] = useState<string | null>(null) // option à réponse libre en cours de saisie
  const [answer, setAnswer] = useState("")
  const choose = async (o: Opt) => {
    if (!o.text && !o.chat) return press(...o.keys)
    setTyping(o.label)
    if (o.chat) return press(...o.keys) // quitte la question : le texte part ensuite dans la zone de saisie normale
    // Champ libre : le curseur doit être sur la ligne sans la valider (case à cocher : chiffre = coche ; liste simple : chiffre = saut).
    await press(...(o.reach ?? (o.keys[o.keys.length - 1] === "Enter" ? o.keys.slice(0, -1) : o.keys)))
  }
  const reply = async () => {
    const text = answer.trim()
    if (!text) return
    if (prompt?.options.find((o) => o.label === typing)?.chat) await new Promise((x) => setTimeout(x, 800)) // laisse la question se fermer
    const r = await api.post("/session-send", { id, text })
    if (!r.ok) return void toast.error(r.out)
    setAnswer("")
    setTyping(null)
    onSent()
  }
  const asking = !!prompt
  const ask = useRef(onAsk) // le parent passe une flèche neuve à chaque rendu : l'effet ne doit suivre que asking
  useEffect(() => {
    ask.current = onAsk
  })
  useEffect(() => {
    ask.current?.(asking)
    return () => ask.current?.(false)
  }, [asking])
  return (
    <div className="space-y-2">
      {prompt && (
        <div className="rounded-lg border border-warning/50 bg-warning/10 px-3 py-2 text-sm">
          {prompt.tabs && prompt.tabs.length > 1 && (
            <p className="mb-1.5 flex flex-wrap items-center gap-1 text-xs" aria-label={t("conversation.composer.tabsAria")}>
              <Button size="xs" variant="ghost" disabled={pressing} onClick={() => press("Left")} aria-label={t("conversation.composer.prevTab")} title={t("conversation.composer.prevTab")}>
                ←
              </Button>
              {prompt.tabs.map((tab) => (
                <span key={tab.label} className={tab.done ? "rounded-sm border border-success/40 px-1.5 text-success" : "rounded-sm border px-1.5 font-medium"}>
                  {tab.done ? "✔" : "•"} {tab.label}
                </span>
              ))}
              <Button size="xs" variant="ghost" disabled={pressing} onClick={() => press("Right")} aria-label={t("conversation.composer.nextTab")} title={t("conversation.composer.nextTab")}>
                →
              </Button>
            </p>
          )}
          {prompt.context && <pre className="mb-2 max-h-60 overflow-auto rounded-md border bg-background/70 p-2 font-mono text-xs break-words whitespace-pre-wrap">{prompt.context}</pre>}
          {prompt.preview && <pre className="mb-2 max-h-60 overflow-auto rounded-md border bg-background/70 p-2 font-mono text-xs leading-tight whitespace-pre">{prompt.preview}</pre>}
          <p className="mb-1.5 font-medium">{prompt.question || t("conversation.composer.answerExpected")}</p>
          <div className="flex flex-col items-stretch gap-1.5">
            {prompt.options.map((o) => (
              <div key={o.label} className="contents">
                <Button size="sm" disabled={pressing} variant={typing === o.label ? "secondary" : "outline"} className="h-auto min-h-8 justify-start gap-2 py-1.5 text-left whitespace-normal" onClick={() => choose(o)} title={t("conversation.composer.keys", { keys: o.keys.join(", ") })}>
                  {o.checked !== undefined && (o.checked ? <SquareCheck className="text-success" /> : <Square />)}
                  <span className="min-w-0">
                    {o.label}
                    {o.detail && <span className="block text-xs font-normal text-muted-foreground">{o.detail}</span>}
                  </span>
                </Button>
                {typing === o.label && (
                  <div className="flex items-end gap-1.5">
                    <textarea
                      autoFocus
                      value={answer}
                      onChange={(e) => setAnswer(e.target.value)}
                      onKeyDown={(e) => isSend(e) && (e.preventDefault(), reply())}
                      rows={2}
                      maxLength={32000}
                      placeholder={keyHint(t("conversation.composer.answerPlaceholder"))}
                      className="field-sizing-content max-h-40 min-h-12 flex-1 resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
                      aria-label={t("conversation.composer.answerAria")}
                    />
                    <Button size="sm" onClick={reply} disabled={!answer.trim()}>
                      <Send /> {t("conversation.composer.send")}
                    </Button>
                  </div>
                )}
              </div>
            ))}
            {prompt.submit && (
              <Button size="sm" disabled={pressing} onClick={() => press(...prompt.submit!)} title={t("conversation.composer.validateTitle", { keys: prompt.submit.join(", ") })}>
                <Send /> {t("conversation.composer.validate")}
              </Button>
            )}
            <Button size="sm" disabled={pressing} variant="ghost" className="justify-start" onClick={() => press("Escape")}>
              {t("conversation.composer.escape")}
            </Button>
          </div>
        </div>
      )}
      {debug && (
        <div className="space-y-1.5 text-2xs">
          <pre className="max-h-72 overflow-auto rounded-lg border bg-background px-3 py-2 font-mono leading-snug whitespace-pre" aria-label={t("conversation.composer.screenAria")}>
            {screen || t("conversation.composer.emptyScreen")}
          </pre>
          <pre className="max-h-40 overflow-auto rounded-lg border bg-muted/40 px-3 py-2 font-mono leading-snug whitespace-pre-wrap" aria-label={t("conversation.composer.detectorAria")}>
            {prompt ? JSON.stringify(prompt, null, 1) : t("conversation.composer.noPrompt")}
          </pre>
          <Button size="xs" variant="outline" onClick={() => copyText(screen).then(() => toast.success(t("conversation.composer.screenCopied")), () => toast.error(t("conversation.composer.copyFailed")))}>
            <Copy /> {t("conversation.composer.copyScreen")}
          </Button>
        </div>
      )}
      <div className="flex items-center justify-end gap-1">
        {extra}
        <Button size="xs" variant="ghost" className="text-muted-foreground max-md:[&>svg]:hidden" onClick={() => setDebug(!debug)} aria-pressed={debug} title={t("conversation.composer.rawScreenTitle")}>
          <Bug /> {t("conversation.composer.rawScreen")}
        </Button>
      </div>
    </div>
  )
}

// Commandes de Claude Code envoyées telles quelles à la session (elles s'exécutent dans son terminal). /compact accepte des consignes :
// ce qui est écrit dans le champ s'y ajoute (« /compact garde les décisions d'architecture »).
const slash = () => {
  const s = (name: string) => [i18n.t(`conversation.slash.${name}.label`), i18n.t(`conversation.slash.${name}.hint`)] as const
  return (["compact", "context", "cost", "usage", "clear", "model", "mcp", "status", "memory", "permissions", "agents", "rewind", "export", "doctor", "help"] as const).map((n) => [`/${n}`, ...s(n)] as const)
}

async function sendSlash(id: string, cmd: string, text: string, onSent: () => void) {
  const full = cmd === "/compact" && text.trim() ? `${cmd} ${text.trim().replace(/\s+/g, " ")}` : cmd
  const r = await api.post("/session-send", { id, text: full })
  if (!r.ok) toast.error(r.out)
  else onSent()
}

function SlashMenu({ id, text, onSent }: { id: string; text: string; onSent: () => void }) {
  const { t } = useTranslation()
  const run = (cmd: string) => sendSlash(id, cmd, text, onSent)
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm" variant="ghost" className="max-md:[&>svg]:hidden" title={t("conversation.composer.commandsTitle")}>
          <Minimize2 /> {t("conversation.composer.commands")}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        {slash().map(([cmd, label, hint]) => (
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
