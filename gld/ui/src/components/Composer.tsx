import { useEffect, useRef, useState, type ReactNode } from "react"
import { usePending, refreshPending } from "@/hooks/usePending"
import { Trans, useTranslation } from "react-i18next"
import { i18n } from "@/i18n"
import { cn, copyText, isSend, keyHint } from "@/lib/utils"
import { modeOfScreen, parseBtw, parsePrompt, type Opt } from "@/lib/prompt"
import { type Live } from "./LiveStatus"
import { toast } from "sonner"
import { Bug, Copy, GitFork, Minimize2, Power, Snowflake, Send, SendHorizontal, Square, SquareCheck, X } from "lucide-react"
import { useLocal } from "@/hooks/useLocal"
import { draftOf, setDraft, useDraft } from "@/hooks/useDraft"
import { efforts, models, withChoice, type Choice } from "@/lib/models"
import { ModelPicker } from "./ModelPicker"
import { useMention } from "./Mention"
import { Choose } from "./Choose"
import { api, type Msg, type QItem } from "@/lib/api"
import { errMsg, qs, readResponse, seg, v1 } from "@/lib/apiError"
import { useActions } from "./actions"
import { AttachButton, ImageChips, useImages } from "@/hooks/useImages"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { readNdjson } from "@/lib/ndjson"
import { authFetch, useCan } from "@/lib/auth"

const MAX_MESSAGE_LENGTH = 32000
const KEEP_FOCUS_WINDOW_MS = 8000
const OWN_TURN_GRACE_MS = 30_000
const LIVE_SEND_TIMEOUT_MS = 20_000
const INTERRUPT_SETTLE_MS = 400
const LIVE_MODE_POLL_MS = 3000
const LIVE_MODE_REREAD_DELAY_MS = 400
const SCREEN_POLL_MS = 1500
const KEY_PRESS_INTERVAL_MS = 120
const SCREEN_CHANGE_POLL_MS = 400
const SCREEN_CHANGE_MAX_POLLS = 10
const QUESTION_CLOSE_DELAY_MS = 800

const wait = (delayMs: number) => new Promise((resolve) => setTimeout(resolve, delayMs))
const appendToDraft = (draftText: string, addition: string) => (draftText.trim() ? `${draftText}\n${addition}` : addition)

// File serveur : un refus (pleine, texte refusé) s'affiche et laisse le brouillon intact.
async function addToQueue(id: string, text: string): Promise<boolean> {
  try {
    await api.queueAdd(id, text)
    return true
  } catch (error) {
    toast.error(errMsg(error))
    return false
  } finally {
    refreshPending()
  }
}
// Retirer un message le remet dans la zone de saisie, à la suite de ce qui s'y trouve déjà.
async function removeFromQueue(id: string, item: QItem) {
  try {
    const removed = await api.queueRm(id, item.id)
    const returnedText = removed.text ?? item.text
    setDraft(id, { text: appendToDraft(draftOf(id).text, returnedText) })
  } catch (error) {
    toast.error(errMsg(error))
  } finally {
    refreshPending()
  }
}
async function retryItem(id: string, item: QItem) {
  try {
    await api.queueRetry(id, item.id)
  } catch (error) {
    toast.error(errMsg(error))
  } finally {
    refreshPending()
  }
}
// Une ligne de la file. Un envoi incertain se tranche (renvoyer ou retirer) ; un envoi en cours ne se retire plus.
function QueueRow({ id, item, index, label, onSendNow }: { id: string; item: QItem; index: number; label?: ReactNode; onSendNow?: (item: QItem) => void }) {
  const { t } = useTranslation()
  let status: ReactNode
  if (item.state === "uncertain") {
    status = (
      <>
        <span className="shrink-0 text-amber-600 dark:text-amber-400">{t("conversation.composer.sendUncertain")}</span>
        <Button size="xs" variant="outline" onClick={() => void retryItem(id, item)}>
          {t("conversation.composer.resend")}
        </Button>
      </>
    )
  } else if (item.state === "sending") {
    status = <span className="shrink-0">{t("conversation.composer.sending")}</span>
  } else {
    status = label && <span className="shrink-0">{label}</span>
  }
  return (
    <li className="flex items-center gap-2 rounded-md border border-dashed px-2 py-1 text-muted-foreground">
      <span className="shrink-0 tabular-nums">{index + 1}.</span>
      <Author item={item} />
      <span className="min-w-0 flex-1 truncate" title={item.text}>
        {item.text}
      </span>
      {status}
      {onSendNow && item.state === "queued" && (
        <Button size="xs" variant="outline" title={t("conversation.composer.sendNowTitle")} onClick={() => onSendNow(item)}>
          <SendHorizontal /> {t("conversation.composer.sendNow")}
        </Button>
      )}
      {item.state !== "sending" && (
        <Button size="icon-xs" variant="ghost" aria-label={t("conversation.composer.dequeue")} onClick={() => void removeFromQueue(id, item)}>
          <X />
        </Button>
      )}
    </li>
  )
}
function Author({ item }: { item: QItem }) {
  const { t } = useTranslation()
  if (item.author !== "guest") return null
  return <span className="shrink-0 rounded bg-muted px-1 text-2xs uppercase">{item.label || t("conversation.composer.guest")}</span>
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
const fieldRef = (element: HTMLTextAreaElement | null) => {
  field = element
  if (element && Date.now() < focusUntil) element.focus()
}
export const focusComposer = () => {
  if (window.matchMedia("(pointer: coarse)").matches) return // pas de clavier virtuel qui jaillit à l'ouverture
  keepFocus()
}
const keepFocus = () => {
  focusUntil = Date.now() + KEEP_FOCUS_WINDOW_MS
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
  setTurn: (update: (previousTurn: Turn | null) => Turn | null) => void
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
  const setText = (newText: string) => setDraft(id, { text: newText })
  const mention = useMention(text, setText, id)
  const [mode, setMode] = useLocal("ccChatMode", "")
  const [choice, setChoice] = useState(defaultChoice)
  const running = !!turn?.running
  // Le claude -p lancé par gld pour ce tour apparaît lui-même comme « session ouverte » (claude agents) pendant qu'il répond, et
  // jusqu'au prochain relevé : tant que le tour est le nôtre, on ne bascule pas vers l'envoi à une session ouverte dans un terminal.
  const [own, setOwn] = useState(false)
  const pictures = useImages()
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
    const renameMatch = /^\/rename\s+(\S[\s\S]*)$/.exec(text.trim())
    if (renameMatch) {
      setText("")
      api.rename({ dir, id }, { title: renameMatch[1] }).then(
        (response) => toast.success(response.message),
        (error) => toast.error(errMsg(error)),
      )
      void onDone()
      return
    }
    const prompt = [text.trim(), pictures.suffix()].filter(Boolean).join("\n\n")
    if (!prompt) return
    keepFocus()
    if (running) {
      if (adding.current) return
      const queued = await enqueue(prompt)
      if (!queued) return // refusé : le brouillon reste
      if (draftOf(id).text === text) setText("")
      pictures.clear()
      return
    }
    setText("")
    pictures.clear()
    onWrite?.()
    const delivered = await submit(prompt)
    if (delivered) return
    // non délivré : jamais perdu, il revient dans le brouillon
    const currentDraft = draftOf(id).text
    if (!currentDraft.includes(prompt)) setText(appendToDraft(currentDraft, prompt))
  }
  // Envoyer la tête de la file suspendue : son texte passe d'abord dans le brouillon (enregistré), puis la file le lâche, puis
  // l'envoi ; le brouillon d'origine n'est rendu qu'une fois l'envoi réussi, donc fermer l'onglet n'efface jamais le texte.
  const sendHead = async (head: QItem) => {
    if (head.state !== "queued") return
    const before = draftOf(id).text
    const merged = appendToDraft(before, head.text)
    setText(merged)
    let returnedText: string
    try {
      returnedText = (await api.queueRm(id, head.id)).text ?? head.text
    } catch (error) {
      toast.error(errMsg(error))
      if (draftOf(id).text === merged) setText(before)
      return
    } finally {
      refreshPending()
    }
    onWrite?.()
    const delivered = await submit(returnedText)
    if (delivered && draftOf(id).text === merged) setText(before)
  }
  const submit = async (prompt: string): Promise<boolean> => {
    setOwn(true)
    setTurn(() => ({ id, msgs: [{ r: "user", t: prompt }], running: true }))
    const fail = (error: string) => {
      setOwn(false)
      setTurn((previousTurn) => previousTurn && { ...previousTurn, running: false, error })
      return false
    }
    stopped.current = false
    let failed = false
    let response: Response
    try {
      response = await authFetch(v1(`/sessions/${seg(id)}/chat${qs({ dir })}`), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ prompt, args: withChoice(args, choice), mode }),
      })
      if (!response.ok) await readResponse(response)
    } catch (error) {
      return fail(errMsg(error))
    }
    if (!response.body) return fail(`HTTP ${response.status}`)
    try {
      for await (const event of readNdjson<{ msgs?: Msg[]; done?: boolean; ok?: boolean; error?: string; cost?: number; note?: string }>(response.body)) {
        if (event.note) toast.info(event.note)
        const turnFailed = !!event.done && !event.ok
        if (turnFailed) failed = true
        setTurn(
          (previousTurn) =>
            previousTurn && {
              ...previousTurn,
              msgs: event.msgs ? [...previousTurn.msgs, ...event.msgs] : previousTurn.msgs,
              running: event.done ? false : previousTurn.running,
              error: turnFailed ? event.error || i18n.t("conversation.composer.failed") : previousTurn.error,
              cost: event.cost ?? previousTurn.cost,
            }
        )
      }
    } catch (error) {
      return fail(error instanceof Error ? error.message : String(error)) // connexion coupée en cours de réponse
    }
    const delivered = !failed || stopped.current // échec de claude (pas un arrêt demandé) : on ne sait pas si le message est parti
    setTurn((previousTurn) => previousTurn && { ...previousTurn, running: false })
    await onDone() // le transcript est relu avant le tour suivant, qui efface sinon son propre affichage
    if (delivered) api.rename({ dir, id }, { auto: true, keep: true }).then(() => onDone(), () => {}) // échec (session ouverte, IA indisponible) : le titre par défaut reste
    setTimeout(() => setOwn(false), OWN_TURN_GRACE_MS)
    return delivered
  }
  const stop = () => {
    stopped.current = true
    void api.chatStop(id).catch(() => {})
  }

  if (live && !running && !own) return <LiveSend id={id} onSent={onSent} liveState={liveState} onAsk={onAsk} bar={extra} compactHint={compactHint} coldHint={coldHint} />
  return (
    <div className="border-t bg-muted/20 px-4 py-3">
      <div className="mx-auto max-w-page space-y-2">
        {mention.list}
        {coldHint && <ColdHint {...coldHint} />}
        {extra && <div className="flex justify-end">{extra}</div>}
        <div className="relative">
          <textarea
            ref={fieldRef}
            value={text}
            onChange={(changeEvent) => {
              setText(changeEvent.target.value)
              mention.track(changeEvent.target)
            }}
            onKeyDown={(keyEvent) => {
              if (mention.onKey(keyEvent)) return
              if (!isSend(keyEvent)) return
              keyEvent.preventDefault()
              void send()
            }}
            onPaste={pictures.onPaste}
            onDrop={pictures.onDrop}
            placeholder={keyHint(running ? t("conversation.composer.placeholderRunning") : t("conversation.composer.placeholder"))}
            rows={2}
            maxLength={MAX_MESSAGE_LENGTH}
            className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border border-input bg-background py-2 pr-10 pl-3 outline-none focus-visible:ring-3 focus-visible:ring-ring/80 disabled:opacity-60"
            aria-label={t("conversation.composer.messageAria")}
          />
          <div className="absolute right-1.5 bottom-1.5">
            <AttachButton onPick={pictures.pick} disabled={running} />
          </div>
        </div>
        <ImageChips images={pictures.images} busy={pictures.busy} onRemove={pictures.remove} />
        {queue.length > 0 && (
          <ul className="space-y-1 text-xs" aria-label={t("conversation.composer.queueAria")}>
            {queue.map((queueItem, queueIndex) => (
              <QueueRow key={queueItem.id} id={id} item={queueItem} index={queueIndex} />
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
          <Choose value={mode} onChange={setMode} options={modes()} label={t("conversation.composer.permissions")} disabled={running} size="sm" className="max-w-full min-w-0" />
          <ModelPicker value={choice} onChange={setChoice} disabled={running} size="sm" />
          {turn?.cost ? <span className="text-muted-foreground">{t("conversation.composer.cost", { cost: turn.cost.toFixed(2) })}</span> : null}
          <span className="ml-auto max-md:hidden" />
          {running ? (
            <Button size="sm" variant="outline" onClick={stop}>
              <Square /> {t("conversation.composer.stop")}
            </Button>
          ) : (
            <Button size="sm" onClick={send} disabled={(!text.trim() && !pictures.images.length) || !exec}>
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
  const actions = useActions()
  const exec = useCan()("exec")
  const [info, setInfo] = useState<{ ids: string[]; why: string } | null>(null)
  const { text } = useDraft(id) // gardé par session : fermer la fenêtre ou un échec d'envoi ne le perd pas
  const queue = usePending(id)
  const setText = (newText: string) => setDraft(id, { text: newText })
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
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [extra, setExtra] = useState<(readonly [string, string])[]>([])
  const pictures = useImages(id)
  useEffect(() => {
    let isMounted = true
    api.sendInfo().then((sendInfo) => isMounted && setInfo(sendInfo), () => {})
    api.slashCommands().then(
      (response) => isMounted && setExtra(response.commands.map((command) => [command.cmd, command.desc] as const)),
      () => {},
    )
    return () => {
      isMounted = false
    }
  }, [id])
  const canSendToTerminal = info?.ids.includes(id)
  const isTypingCommand = /^\/\S*$/.test(text)
  let suggestions: (readonly [string, string])[] = []
  if (isTypingCommand) {
    const knownCommands = [
      ...slash().map(([command, label]) => [command, label] as const),
      ["/btw", i18n.t("conversation.slash.btw.hint")] as const,
      ...extra,
    ]
    suggestions = knownCommands.filter(([command]) => command.startsWith(text) && command !== text)
  }
  const highlightedIndex = suggestions.length ? selectedIndex % suggestions.length : 0
  // Un message écrit pendant que Claude travaille, ou qu'une question attend, est mis en file : il part dès que la session attend
  // (jamais dans une question, où il répondrait à sa place).
  const post = async (full: string) => {
    setBusy(true)
    try {
      await api.sessionSend(id, full, LIVE_SEND_TIMEOUT_MS)
      return true
    } catch (error) {
      toast.error(errMsg(error)) // le succès se voit dans la session : pas de notification
      return false
    } finally {
      setBusy(false)
    }
  }
  const send = async () => {
    const full = [text.trim(), pictures.suffix()].filter(Boolean).join("\n")
    if (!full || busy) return
    keepFocus()
    // /btw se pose pendant que Claude travaille, mais jamais pendant une question.
    const isSideQuestion = /^\/btw\s/.test(full) && !asking
    const mustQueue = (working || asking) && !isSideQuestion
    if (mustQueue) {
      if (adding.current) return
      const queued = await enqueue(full)
      if (!queued) return
      if (draftOf(id).text === text) setText("")
      pictures.clear()
      return
    }
    const posted = await post(full)
    if (!posted) return
    if (draftOf(id).text === text) setText("") // pas un texte tapé entre-temps
    pictures.clear()
    onSent()
    keepFocus()
  }
  // Envoi avec interruption : le message sort de la file, Échap coupe la réponse en cours, puis il part.
  const sendNow = async (item: QItem) => {
    let queuedText: string
    try {
      queuedText = (await api.queueRm(id, item.id)).text ?? item.text
    } catch (error) {
      toast.error(errMsg(error))
      return
    } finally {
      refreshPending()
    }
    await api.sessionKey(id, "Escape").catch((error) => toast.error(errMsg(error)))
    await new Promise((resolve) => setTimeout(resolve, INTERRUPT_SETTLE_MS))
    if (!(await post(queuedText))) setText(appendToDraft(draftOf(id).text, queuedText))
    else onSent()
  }
  let headOfQueueLabel: string | undefined
  if (asking) headOfQueueLabel = t("conversation.composer.afterQuestion")
  else if (working) headOfQueueLabel = t("conversation.composer.whenIdle")
  const settingsDisabled = working || asking || busy
  const sessionModel = models().find(([modelKey]) => modelKey && liveState.st?.model?.toLowerCase().includes(modelKey))?.[0] ?? ""
  const postSetting = async (command: string, value: string) => {
    if (!value) return
    const posted = await post(`${command} ${value}`)
    if (posted) onSent()
  }
  if (!canSendToTerminal)
    return (
      <div className="flex items-center gap-3 border-t px-6 py-3 text-xs text-muted-foreground">
        <p className="min-w-0 flex-1 text-center">
          {actions.background.has(id) ? (
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
      <div className="mx-auto max-w-page space-y-2">
        <TerminalPanel
          id={id}
          onAsk={(isAsking) => {
            setAsking(isAsking)
            onAsk?.(isAsking)
          }}
          onSent={onSent}
          extra={bar}
        />
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
        {suggestions.length > 0 && (
          <ul className="max-h-48 overflow-y-auto rounded-lg border bg-popover p-1 text-sm shadow-md" role="listbox" aria-label={t("conversation.composer.commandsAria")}>
            {suggestions.map(([cmd, label], suggestionIndex) => (
              <li
                key={cmd}
                role="option"
                ref={(element) => {
                  if (element && suggestionIndex === highlightedIndex) element.scrollIntoView({ block: "nearest" })
                }}
                aria-selected={suggestionIndex === highlightedIndex}
                onMouseDown={(mouseEvent) => {
                  mouseEvent.preventDefault()
                  setText(cmd + " ")
                }}
                className={cn("flex cursor-pointer items-baseline gap-2 rounded-md px-2 py-1", suggestionIndex === highlightedIndex && "bg-accent")}
              >
                <code className="text-xs">{cmd}</code>
                <span className="text-muted-foreground">{label}</span>
              </li>
            ))}
          </ul>
        )}
        {mention.list}
        <div className="relative">
          <textarea
            ref={fieldRef}
            value={text}
            onChange={(changeEvent) => {
              setText(changeEvent.target.value)
              setSelectedIndex(0)
              mention.track(changeEvent.target)
            }}
            onKeyDown={(keyEvent) => {
              if (mention.onKey(keyEvent)) return
              if (!suggestions.length) {
                if (!isSend(keyEvent)) return
                keyEvent.preventDefault()
                void send()
                return
              }
              if (keyEvent.key === "ArrowDown" || keyEvent.key === "ArrowUp") {
                keyEvent.preventDefault()
                const step = keyEvent.key === "ArrowDown" ? 1 : suggestions.length - 1
                setSelectedIndex((previousIndex) => (previousIndex + step) % suggestions.length)
                return
              }
              if (keyEvent.key === "Tab" || keyEvent.key === "Enter") {
                keyEvent.preventDefault()
                setText(suggestions[highlightedIndex][0] + " ")
              }
            }}
            onPaste={pictures.onPaste}
            onDrop={pictures.onDrop}
            placeholder={keyHint(t("conversation.composer.sessionPlaceholder"))}
            rows={2}
            maxLength={MAX_MESSAGE_LENGTH}
            className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border border-input bg-background py-2 pr-10 pl-3 outline-none focus-visible:ring-3 focus-visible:ring-ring/80"
            aria-label={t("conversation.composer.sessionAria")}
          />
          <div className="absolute right-1.5 bottom-1.5">
            <AttachButton onPick={pictures.pick} />
          </div>
        </div>
        <ImageChips images={pictures.images} busy={pictures.busy} onRemove={pictures.remove} />
        {queue.length > 0 && (
          <ul className="space-y-1 text-xs" aria-label={t("conversation.composer.queueAria")}>
            {queue.map((queueItem, queueIndex) => (
              <QueueRow key={queueItem.id} id={id} item={queueItem} index={queueIndex} label={queueIndex === 0 ? headOfQueueLabel : undefined} onSendNow={working && !asking ? sendNow : undefined} />
            ))}
          </ul>
        )}
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {/* Téléphone : réglages sur une ligne à parts égales, actions sur la suivante avec l'envoi en largeur restante. */}
          <div className="flex min-w-0 items-center gap-2 max-md:w-full max-md:[&>*]:min-w-0 max-md:[&>*]:flex-1">
            <LiveModeButton id={id} disabled={settingsDisabled} />
            <Choose
              value={sessionModel}
              onChange={(selectedModel) => postSetting("/model", selectedModel)}
              options={[["", t("conversation.composer.modelEllipsis")], ...models().slice(1)]}
              label={t("conversation.composer.sessionModel")}
              size="sm"
              disabled={settingsDisabled}
            />
            <Choose
              value=""
              onChange={(selectedEffort) => postSetting("/effort", selectedEffort)}
              options={[["", t("conversation.composer.effortEllipsis")], ...efforts().slice(1)]}
              label={t("conversation.composer.sessionEffort")}
              size="sm"
              disabled={settingsDisabled}
            />
          </div>
          <span className="ml-auto max-md:hidden" />
          <div className="flex items-center gap-2 max-md:w-full">
            <SlashMenu id={id} text={text} onSent={onSent} />
            {liveState.st?.thinking && (
              <Button
                size="sm"
                variant="outline"
                title={t("conversation.composer.interruptTitle")}
                onClick={async () => {
                  await api.sessionKey(id, "Escape").catch((error) => toast.error(errMsg(error)))
                }}
              >
                <Square className="max-md:hidden" /> {t("conversation.composer.interrupt")}
              </Button>
            )}
            <Button size="sm" className="max-md:flex-1" onClick={send} disabled={(!text.trim() && !pictures.images.length) || busy || !exec}>
              <Send /> {t("conversation.composer.sendToSession")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

// Mode de permission de la session ouverte : lu sur l'écran tmux, un clic envoie Maj+Tab (mode suivant).
function LiveModeButton({ id, disabled }: { id: string; disabled: boolean }) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<ReturnType<typeof modeOfScreen>>(null)
  const read = () => api.screen(id).then((terminalScreen) => setMode(modeOfScreen(terminalScreen.text)), () => {})
  useEffect(() => {
    read()
    const pollTimer = setInterval(() => {
      if (document.visibilityState === "visible") read()
    }, LIVE_MODE_POLL_MS)
    return () => clearInterval(pollTimer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])
  const next = async () => {
    try {
      await api.sessionKey(id, "ShiftTab")
    } catch (error) {
      toast.error(errMsg(error))
      return
    }
    setTimeout(read, LIVE_MODE_REREAD_DELAY_MS)
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
  const actions = useActions()
  const close = () => actions.stop([id], onClosed) // confirme dans la fenêtre commune, puis met la liste à jour
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
    let isMounted = true
    const load = () => {
      if (busy.current) return
      api.screen(id).then(
        (terminalScreen) => {
          if (isMounted && !busy.current) setScreen(terminalScreen.text)
        },
        () => {},
      )
    }
    load()
    const pollTimer = setInterval(() => {
      if (document.visibilityState === "visible") load()
    }, SCREEN_POLL_MS)
    return () => {
      isMounted = false
      clearInterval(pollTimer)
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
        await api.sessionKey(id, key)
        await wait(KEY_PRESS_INTERVAL_MS)
      }
      for (let pollIndex = 0; pollIndex < SCREEN_CHANGE_MAX_POLLS; pollIndex++) {
        await wait(SCREEN_CHANGE_POLL_MS)
        const terminalScreen = await api.screen(id).catch(() => null)
        if (!terminalScreen) continue
        setScreen(terminalScreen.text)
        if (terminalScreen.text === before) continue
        if (!parsePrompt(terminalScreen.text)) onSent() // dialogue terminé : la suite arrive, on la suit tout de suite
        break
      }
    } catch (error) {
      toast.error(errMsg(error))
    } finally {
      busy.current = false
      setPressing(false)
    }
  }
  const prompt = parsePrompt(screen)
  const btw = parseBtw(screen)
  const [typing, setTyping] = useState<string | null>(null) // option à réponse libre en cours de saisie
  const [answer, setAnswer] = useState("")
  const editLen = useRef(0) // réponse libre déjà saisie dans le terminal : à effacer avant de renvoyer le texte modifié
  const choose = async (option: Opt) => {
    if (!option.text && !option.chat) return press(...option.keys)
    setTyping(option.label)
    setAnswer(option.edit ?? "")
    editLen.current = option.edit?.length ?? 0
    if (option.chat) return press(...option.keys) // quitte la question : le texte part ensuite dans la zone de saisie normale
    // Champ libre : le curseur doit être sur la ligne sans la valider (case à cocher : chiffre = coche ; liste simple : chiffre = saut).
    const endsWithEnter = option.keys[option.keys.length - 1] === "Enter"
    const keysWithoutValidation = endsWithEnter ? option.keys.slice(0, -1) : option.keys
    await press(...(option.reach ?? keysWithoutValidation))
  }
  const reply = async () => {
    const text = answer.trim()
    if (!text) return
    const typedOption = prompt?.options.find((option) => option.label === typing)
    if (typedOption?.chat) await wait(QUESTION_CLOSE_DELAY_MS) // laisse la question se fermer
    try {
      for (let erasedCount = 0; erasedCount < editLen.current; erasedCount++) await api.sessionKey(id, "Backspace")
      editLen.current = 0
      await api.sessionSend(id, text)
    } catch (error) {
      toast.error(errMsg(error))
      return
    }
    setAnswer("")
    setTyping(null)
    onSent()
  }
  const copyWithToast = (copiedText: string) =>
    copyText(copiedText).then(
      () => toast.success(t("conversation.composer.screenCopied")),
      () => toast.error(t("conversation.composer.copyFailed")),
    )
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
            {prompt.options.map((option) => (
              <div key={option.label} className="contents">
                <Button size="sm" disabled={pressing} variant={typing === option.label ? "secondary" : "outline"} className="h-auto min-h-8 justify-start gap-2 py-1.5 text-left whitespace-normal" onClick={() => choose(option)} title={t("conversation.composer.keys", { keys: option.keys.join(", ") })}>
                  {option.checked !== undefined && (option.checked ? <SquareCheck className="text-success" /> : <Square />)}
                  <span className="min-w-0">
                    {option.label}
                    {option.detail && <span className="block text-xs font-normal text-muted-foreground">{option.detail}</span>}
                  </span>
                </Button>
                {typing === option.label && (
                  <div className="flex items-end gap-1.5">
                    <textarea
                      autoFocus
                      value={answer}
                      onChange={(changeEvent) => setAnswer(changeEvent.target.value)}
                      onKeyDown={(keyEvent) => {
                        if (!isSend(keyEvent)) return
                        keyEvent.preventDefault()
                        void reply()
                      }}
                      rows={2}
                      maxLength={MAX_MESSAGE_LENGTH}
                      placeholder={keyHint(t("conversation.composer.answerPlaceholder"))}
                      className="field-sizing-content max-h-40 min-h-12 flex-1 resize-none rounded-lg border border-input bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/80"
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
      {btw && (
        <div className="space-y-1.5 rounded-lg border bg-background p-3 text-sm">
          <p className="text-xs font-medium text-muted-foreground">/btw {btw.question}</p>
          <pre className="max-h-72 overflow-auto font-sans break-words whitespace-pre-wrap">{btw.answer}</pre>
          <div className="flex flex-wrap gap-1.5">
            <Button size="xs" variant="outline" onClick={() => copyWithToast(btw.answer)}>
              <Copy /> {t("conversation.composer.btwCopy")}
            </Button>
            <Button size="xs" variant="outline" disabled={pressing} onClick={() => press("f")} title={t("conversation.composer.btwForkTitle")}>
              <GitFork /> {t("conversation.composer.btwFork")}
            </Button>
            <Button size="xs" variant="outline" disabled={pressing} onClick={() => press("Escape")}>
              {t("conversation.composer.btwClose")}
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
          <Button size="xs" variant="outline" onClick={() => copyWithToast(screen)}>
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
  const labelAndHint = (name: string) => [i18n.t(`conversation.slash.${name}.label`), i18n.t(`conversation.slash.${name}.hint`)] as const
  const commandNames = ["compact", "context", "cost", "usage", "clear", "model", "mcp", "status", "memory", "permissions", "agents", "rewind", "export", "doctor", "help"] as const
  return commandNames.map((commandName) => [`/${commandName}`, ...labelAndHint(commandName)] as const)
}

async function sendSlash(id: string, cmd: string, text: string, onSent: () => void) {
  const full = cmd === "/compact" && text.trim() ? `${cmd} ${text.trim().replace(/\s+/g, " ")}` : cmd
  try {
    await api.sessionSend(id, full)
    onSent()
  } catch (error) {
    toast.error(errMsg(error))
  }
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
