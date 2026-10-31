import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowUpToLine, ChevronDown, FolderOpen, Fan, LogOut, MessageSquare, Send, Wrench, X } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import { api, type GuestShare, type LiveState, type Msg, type QItem } from "@/lib/api"
import { ApiError, errMsg } from "@/lib/apiError"
import { setAuth } from "@/lib/auth"
import { GuestFiles } from "./GuestFiles"
import { LiveBar } from "./LiveStatus"
import { Message } from "./Message"

// Vue de l'invité (/guest) : transcript en lecture, file avec auteurs, composeur. Volontairement mince : aucune des routes
// qu'utilisent la barre latérale, Conversation ou le composeur hôte (api, info, git, docs, envoi direct) n'est refusée sans être appelée.
type Me = { id: string; dir: string; mode: "rw" | "ro"; title: string; invite: string }
type Phase = "loading" | "on" | "ended" | "bye"
type Hist = { msgs: Msg[]; start: number; more: boolean }
const PAGE = 200
const EXTRA_MESSAGES_ON_RELOAD = 50
const SHARES_REFRESH_MS = 30000
const ME_REFRESH_MS = 5000
const CONVERSATION_REFRESH_MS = 2000
const STICK_TO_BOTTOM_THRESHOLD_PX = 80
const MAX_MESSAGE_LENGTH = 32000

// 401/403 ou réponse non JSON : le serveur traite la requête comme celle d'un hôte, donc le partage n'existe plus.
const isShareOver = (error: unknown) => {
  if (error instanceof SyntaxError) return true
  if (!(error instanceof ApiError)) return false
  return error.status === 401 || error.status === 403 || error.code === "bad_response"
}

const HANDLE = /^\/guest\/([0-9a-f]{16})$/

// Partages de ce navigateur (/v1/guest/sessions), sans en-tête de partage : relu toutes les 30 s, sans bruit si ça échoue.
function useShares(on: boolean): [GuestShare[] | null, () => Promise<void>, boolean] {
  const [list, setList] = useState<GuestShare[] | null>(null)
  const [failed, setFailed] = useState(false)
  const reload = useCallback(
    () =>
      api.guestList().then(
        (shareList) => {
          setList(shareList)
          setFailed(false)
        },
        (error) => {
          if (!isShareOver(error)) {
            setFailed(true)
            return
          }
          setList([])
          setFailed(false)
        }
      ),
    []
  )
  useEffect(() => {
    if (!on) return
    void reload()
    const refreshInterval = setInterval(() => document.visibilityState === "visible" && void reload(), SHARES_REFRESH_MS)
    return () => clearInterval(refreshInterval)
  }, [on, reload])
  return [list, reload, failed]
}

const OtherLinks = ({ list }: { list: GuestShare[] }) => {
  const { t } = useTranslation()
  return (
    <ul className="space-y-2">
      {list.map((share) => (
        <li key={share.handle}>
          <a href={`/guest/${share.handle}`} className="flex items-center gap-2 rounded-md border px-3 py-2 hover:bg-muted">
            <span className="min-w-0 flex-1 truncate">{share.title || t("conversation.guest.title")}</span>
            <span className="shrink-0 rounded bg-muted px-1 text-2xs uppercase">{share.mode === "rw" ? t("conversation.guest.modeRw") : t("conversation.guest.modeRo")}</span>
          </a>
        </li>
      ))}
    </ul>
  )
}

const Ended = ({ others = [] }: { others?: GuestShare[] }) => {
  const { t } = useTranslation()
  return (
    <main className="flex min-h-svh flex-col items-center justify-center gap-2 p-6 text-center">
      <h1 className="text-lg font-semibold">{t("conversation.guest.ended")}</h1>
      <p className="text-sm text-muted-foreground">{t("conversation.guest.endedHint")}</p>
      {others.length > 0 && (
        <div className="mt-2 w-full max-w-md space-y-2 text-left">
          <p className="text-sm text-muted-foreground">{t("conversation.guest.byeOthers")}</p>
          <OtherLinks list={others} />
        </div>
      )}
    </main>
  )
}

// /guest sans poignée : aucun partage = terminé, un seul = on y va, plusieurs = choix.
function Chooser() {
  const { t } = useTranslation()
  const [list, reload, failed] = useShares(true)
  useEffect(() => {
    if (list?.length === 1) location.replace(`/guest/${list[0].handle}`)
  }, [list])
  if (!list && failed)
    return (
      <main className="flex min-h-svh flex-col items-center justify-center gap-3 p-6 text-center">
        <p className="text-sm text-muted-foreground">{t("conversation.guest.listFailed")}</p>
        <Button variant="outline" size="sm" onClick={() => void reload()}>
          {t("conversation.guest.retry")}
        </Button>
      </main>
    )
  if (!list || list.length === 1)
    return (
      <main className="flex min-h-svh items-center justify-center gap-2 p-6 text-sm text-muted-foreground">
        <Fan className="size-4 animate-spin" aria-hidden />
        {t("conversation.guest.loading")}
      </main>
    )
  if (!list.length) return <Ended />
  return (
    <main className="mx-auto flex min-h-svh max-w-md flex-col justify-center gap-4 p-6">
      <h1 className="text-lg font-semibold">{t("conversation.guest.pick")}</h1>
      <OtherLinks list={list} />
    </main>
  )
}

export function GuestApp() {
  // Posé pendant le premier rendu : aucun effet, donc aucune requête, ne part avant.
  const [handle] = useState(() => {
    const handleFromPath = HANDLE.exec(location.pathname)?.[1]
    setAuth({ share: handleFromPath })
    return handleFromPath
  })
  return handle ? <GuestSession handle={handle} /> : <Chooser />
}

function GuestSession({ handle }: { handle: string }) {
  const { t } = useTranslation()
  const [phase, setPhase] = useState<Phase>("loading")
  const [me, setMe] = useState<Me | null>(null)
  const [shares, reloadShares] = useShares(phase !== "ended")
  const others = (shares ?? []).filter((share) => share.handle !== handle)
  const [history, setHistory] = useState<Hist>({ msgs: [], start: 0, more: false })
  const [tools, setTools] = useState(false)
  const [files, setFiles] = useState(false)
  const [loadingOlder, setLoadingOlder] = useState(false)
  const historyRef = useRef(history)
  const historyGeneration = useRef(0)
  const sending = useRef(false)
  const [st, setSt] = useState<LiveState | null>(null)
  const [queue, setQueue] = useState<QItem[]>([])
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const endRef = useRef<HTMLDivElement>(null)
  // Suit la fin tant que l'invité y est ; dès qu'il remonte lire, la vue ne bouge plus jusqu'à ce qu'il redescende ou envoie un message.
  const stuck = useRef(true)
  useEffect(() => {
    historyRef.current = history
  })

  const readMe = useCallback(async (): Promise<boolean> => {
    try {
      const guestMe = await api.guestMe()
      setAuth({ state: "open", csrf: guestMe.csrf })
      setMe((previousMe) => {
        const isUnchanged =
          previousMe &&
          previousMe.id === guestMe.id &&
          previousMe.mode === guestMe.mode &&
          previousMe.title === guestMe.title &&
          previousMe.invite === guestMe.invite
        if (isUnchanged) return previousMe
        return { id: guestMe.id, dir: guestMe.dir, mode: guestMe.mode, title: guestMe.title, invite: guestMe.invite }
      })
      setPhase((previousPhase) => (previousPhase === "loading" ? "on" : previousPhase))
      return false
    } catch (error) {
      if (!isShareOver(error)) return false
      setPhase("ended")
      return true
    }
  }, [])

  useEffect(() => {
    if (phase === "ended") void reloadShares()
  }, [phase, reloadShares])

  const active = phase === "loading" || phase === "on"
  useEffect(() => {
    if (!active) return
    const firstRead = setTimeout(() => void readMe(), 0)
    const refreshInterval = setInterval(() => void readMe(), ME_REFRESH_MS)
    return () => {
      clearTimeout(firstRead)
      clearInterval(refreshInterval)
    }
  }, [active, readMe])

  const id = me?.id ?? ""
  const dir = me?.dir ?? ""
  useEffect(() => {
    if (!active || !id) return
    let isStillMounted = true
    const load = () => {
      const generationAtStart = historyGeneration.current
      const messageLimit = Math.max(PAGE, historyRef.current.msgs.length + EXTRA_MESSAGES_ON_RELOAD)
      return Promise.all([api.conv({ dir, id }, tools, { limit: messageLimit }), api.sessionLive(dir, id), api.queue(id)]).then(
        ([conversation, liveState, queueResponse]) => {
          if (!isStillMounted) return
          // Une page plus ancienne chargée entre-temps a changé la génération : ce relevé l'écraserait.
          if (generationAtStart === historyGeneration.current) {
            const nextHistory = { msgs: conversation.msgs, start: conversation.start ?? 0, more: !!conversation.more }
            setHistory((previousHistory) => (JSON.stringify(previousHistory) === JSON.stringify(nextHistory) ? previousHistory : nextHistory))
          }
          setSt((previousState) => (JSON.stringify(previousState) === JSON.stringify(liveState) ? previousState : liveState))
          setQueue(queueResponse.items ?? [])
        },
        (error) => isStillMounted && isShareOver(error) && setPhase("ended")
      )
    }
    void load()
    const refreshInterval = setInterval(() => document.visibilityState === "visible" && void load(), CONVERSATION_REFRESH_MS)
    return () => {
      isStillMounted = false
      clearInterval(refreshInterval)
    }
  }, [active, id, dir, tools])

  const loadOlder = async () => {
    if (loadingOlder || !history.more) return
    setLoadingOlder(true)
    try {
      const olderPage = await api.conv({ dir, id }, tools, { limit: PAGE, before: history.start })
      historyGeneration.current++
      setHistory((previousHistory) => ({ msgs: [...olderPage.msgs, ...previousHistory.msgs], start: olderPage.start ?? 0, more: !!olderPage.more }))
    } catch (error) {
      if (isShareOver(error)) setPhase("ended")
    }
    setLoadingOlder(false)
  }

  // Corps en bloc : dans Chrome, scrollIntoView renvoie une Promise que React prendrait pour un nettoyage et appellerait au message suivant.
  useEffect(() => {
    if (stuck.current) endRef.current?.scrollIntoView?.({ block: "end" })
  }, [history.msgs.length])
  // La bande d'état (outil, sous-agents, file d'attente) change de hauteur sans nouveau message : la vue reste collée à la fin.
  useEffect(() => {
    const viewport = endRef.current?.closest<HTMLElement>('[data-slot="scroll-area-viewport"]')
    const body = viewport?.firstElementChild
    if (!viewport || !body) return
    const resizeObserver = new ResizeObserver(() => {
      if (stuck.current) viewport.scrollTop = viewport.scrollHeight
    })
    resizeObserver.observe(body)
    return () => resizeObserver.disconnect()
  }, [phase])

  const rw = me?.mode === "rw"
  const refreshQueue = () => api.queue(id).then((queueResponse) => setQueue(queueResponse.items ?? []), () => {})
  const send = async () => {
    const message = text.trim()
    if (!message || sending.current || !rw) return
    sending.current = true
    stuck.current = true
    setBusy(true)
    let failureMessage: string | null = null
    try {
      await api.queueAdd(id, message)
    } catch (error) {
      failureMessage = errMsg(error)
    } finally {
      sending.current = false
      setBusy(false)
    }
    if (failureMessage !== null) {
      const shareEnded = await readMe()
      if (!shareEnded) toast.error(failureMessage)
      return
    }
    setText("")
    refreshQueue()
  }
  const remove = async (queueItem: QItem) => {
    try {
      await api.queueRm(id, queueItem.id)
    } catch (error) {
      if (!(await readMe())) toast.error(errMsg(error))
    }
    refreshQueue()
  }
  const leave = async () => {
    try {
      await api.guestLeave()
      await reloadShares()
      setPhase("bye")
    } catch (error) {
      if (!(await readMe())) toast.error(errMsg(error))
    }
  }

  if (phase === "ended") return <Ended others={others} />
  if (phase === "bye")
    return (
      <main className="flex min-h-svh flex-col items-center justify-center gap-4 p-6 text-center">
        <h1 className="text-lg font-semibold">{t("conversation.guest.bye")}</h1>
        {others.length > 0 && (
          <div className="w-full max-w-md space-y-2 text-left">
            <p className="text-sm text-muted-foreground">{t("conversation.guest.byeOthers")}</p>
            <OtherLinks list={others} />
          </div>
        )}
      </main>
    )
  if (!me)
    return (
      <main className="flex min-h-svh items-center justify-center gap-2 p-6 text-sm text-muted-foreground">
        <Fan className="size-4 animate-spin" aria-hidden />
        {t("conversation.guest.loading")}
      </main>
    )

  return (
    <Dialog open>
      <DialogContent sheet={false}
        showCloseButton={false}
        onOpenAutoFocus={(focusEvent) => focusEvent.preventDefault()}
        onInteractOutside={(interactEvent) => interactEvent.preventDefault()}
        onEscapeKeyDown={(keyEvent) => keyEvent.preventDefault()}
        className="safe-t safe-b flex h-[calc(var(--vvh,100dvh)*0.94)] w-[96vw] max-w-none flex-col gap-0 overflow-hidden p-0 max-md:top-[var(--vvt,0px)] max-md:left-0 max-md:h-[var(--vvh,100dvh)] max-md:w-screen max-md:translate-x-0 max-md:translate-y-0 max-md:rounded-none max-md:border-0 sm:max-w-none"
      >
        <DialogHeader className="gap-1 border-b px-4 pt-2.5 pb-3 md:px-6 md:pb-4">
          <div className="flex items-start gap-4">
            <div className="min-w-0 flex-1 space-y-1">
              <DialogTitle className="flex items-center gap-2 text-lg tracking-tight">
                <span className="truncate">{me.title || t("conversation.guest.title")}</span>
                {!rw && <span className="shrink-0 rounded bg-muted px-1 text-2xs font-normal uppercase">{t("conversation.guest.modeRo")}</span>}
              </DialogTitle>
              <DialogDescription className="sr-only">{t("conversation.view.description")}</DialogDescription>
            </div>
            {others.length > 0 && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" size="xs" aria-label={t("conversation.guest.others")}>
                    {t("conversation.guest.others")} <ChevronDown />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  {others.map((share) => (
                    <DropdownMenuItem key={share.handle} onSelect={() => location.assign(`/guest/${share.handle}`)}>
                      {share.title || t("conversation.guest.title")}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
            <Button variant="outline" size="xs" aria-pressed={files} onClick={() => setFiles(!files)}>
              {files ? <MessageSquare /> : <FolderOpen />} {files ? t("conversation.guest.filesBack") : t("conversation.guest.files")}
            </Button>
            <Button variant="outline" size="xs" onClick={() => void leave()}>
              <LogOut /> {t("conversation.guest.leave")}
            </Button>
          </div>
        </DialogHeader>
        {files && <GuestFiles dir={dir} />}
        <ScrollArea
          className={files ? "hidden" : "min-h-0 flex-1"}
          onScrollCapture={(scrollEvent) => {
            const scrolledElement = scrollEvent.target as HTMLElement
            const distanceFromBottom = scrolledElement.scrollHeight - scrolledElement.scrollTop - scrolledElement.clientHeight
            stuck.current = distanceFromBottom < STICK_TO_BOTTOM_THRESHOLD_PX
          }}
        >
          <div className="session-text mx-auto max-w-page space-y-6 px-3 py-4 md:px-8 md:py-6">
            {history.more && (
              <div className="text-center">
                <Button variant="outline" size="sm" disabled={loadingOlder} onClick={() => void loadOlder()}>
                  {loadingOlder ? <Fan className="animate-spin" /> : <ArrowUpToLine />} {t("conversation.view.loadOlder")}
                </Button>
              </div>
            )}
            {history.msgs.map((message, messageIndex) => (
              <Message key={`${message.o ?? messageIndex}-${messageIndex}`} m={message} conv={{ dir, id, tools, guest: true }} />
            ))}
            <LiveBar live={{ st, open: true }} id={id} />
            <div ref={endRef} />
          </div>
        </ScrollArea>
        <div className="border-t bg-muted/20 px-4 py-3">
          <section aria-label={t("conversation.guest.queue")} className="mx-auto max-w-page space-y-2">
            <div className="flex justify-end">
              <Button variant={tools ? "secondary" : "ghost"} size="xs" className="text-muted-foreground max-md:hidden" onClick={() => setTools(!tools)} aria-pressed={tools}>
                <Wrench /> {tools ? t("conversation.view.toolsShown") : t("conversation.view.toolsHidden")}
              </Button>
            </div>
            {!rw && <p className="rounded-md border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">{t("conversation.guest.readOnly")}</p>}
            <textarea
              value={text}
              disabled={!rw}
              rows={2}
              maxLength={MAX_MESSAGE_LENGTH}
              aria-label={t("conversation.composer.messageAria")}
              placeholder={t("conversation.guest.placeholder")}
              onChange={(changeEvent) => setText(changeEvent.target.value)}
              onKeyDown={(keyEvent) => {
                const isPlainEnter = keyEvent.key === "Enter" && !keyEvent.shiftKey && !keyEvent.nativeEvent.isComposing
                if (!isPlainEnter) return
                keyEvent.preventDefault()
                void send()
              }}
              className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border border-input bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/80 disabled:opacity-60"
            />
            {queue.length > 0 && (
              <ul className="space-y-1 text-xs" aria-label={t("conversation.composer.queueAria")}>
                {queue.map((queueItem, queuePosition) => {
                  const isGuestItem = queueItem.author === "guest"
                  const authorLabel = isGuestItem ? queueItem.label || t("conversation.composer.guest") : t("conversation.guest.host")
                  const canDequeue = isGuestItem && queueItem.guest === me.invite && queueItem.state !== "sending"
                  return (
                    <li key={queueItem.id} className="flex items-center gap-2 rounded-md border border-dashed px-2 py-1 text-muted-foreground">
                      <span className="shrink-0 tabular-nums">{queuePosition + 1}.</span>
                      <span className="shrink-0 rounded bg-muted px-1 text-2xs uppercase">{authorLabel}</span>
                      <span className="min-w-0 flex-1 truncate" title={queueItem.text}>
                        {queueItem.text}
                      </span>
                      {queueItem.state === "uncertain" && <span className="shrink-0 text-amber-600 dark:text-amber-400">{t("conversation.composer.sendUncertain")}</span>}
                      {queueItem.state === "sending" && <span className="shrink-0">{t("conversation.composer.sending")}</span>}
                      {canDequeue && (
                        <Button size="icon-xs" variant="ghost" aria-label={t("conversation.composer.dequeue")} onClick={() => void remove(queueItem)}>
                          <X />
                        </Button>
                      )}
                    </li>
                  )
                })}
              </ul>
            )}
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <div className="ml-auto flex items-center gap-2">
                <Button size="sm" disabled={!rw || busy || !text.trim()} onClick={() => void send()}>
                  <Send /> <span className="max-md:sr-only">{t("conversation.composer.send")}</span>
                </Button>
              </div>
            </div>
          </section>
        </div>
      </DialogContent>
    </Dialog>
  )
}
