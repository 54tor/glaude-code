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

// 401/403 ou réponse non JSON : le serveur traite la requête comme celle d'un hôte, donc le partage n'existe plus.
const over = (e: unknown) => (e instanceof ApiError && (e.status === 401 || e.status === 403 || e.code === "bad_response")) || e instanceof SyntaxError

const HANDLE = /^\/guest\/([0-9a-f]{16})$/

// Partages de ce navigateur (/v1/guest/sessions), sans en-tête de partage : relu toutes les 30 s, sans bruit si ça échoue.
function useShares(on: boolean): [GuestShare[] | null, () => Promise<void>, boolean] {
  const [list, setList] = useState<GuestShare[] | null>(null)
  const [failed, setFailed] = useState(false)
  const reload = useCallback(
    () =>
      api.guestList().then(
        (l) => (setList(l), setFailed(false)),
        (e) => (over(e) ? (setList([]), setFailed(false)) : setFailed(true))
      ),
    []
  )
  useEffect(() => {
    if (!on) return
    void reload()
    const i = setInterval(() => document.visibilityState === "visible" && void reload(), 30000)
    return () => clearInterval(i)
  }, [on, reload])
  return [list, reload, failed]
}

const OtherLinks = ({ list }: { list: GuestShare[] }) => {
  const { t } = useTranslation()
  return (
    <ul className="space-y-2">
      {list.map((s) => (
        <li key={s.handle}>
          <a href={`/guest/${s.handle}`} className="flex items-center gap-2 rounded-md border px-3 py-2 hover:bg-muted">
            <span className="min-w-0 flex-1 truncate">{s.title || t("conversation.guest.title")}</span>
            <span className="shrink-0 rounded bg-muted px-1 text-2xs uppercase">{s.mode === "rw" ? t("conversation.guest.modeRw") : t("conversation.guest.modeRo")}</span>
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
    const h = HANDLE.exec(location.pathname)?.[1]
    setAuth({ share: h })
    return h
  })
  return handle ? <GuestSession handle={handle} /> : <Chooser />
}

function GuestSession({ handle }: { handle: string }) {
  const { t } = useTranslation()
  const [phase, setPhase] = useState<Phase>("loading")
  const [me, setMe] = useState<Me | null>(null)
  const [shares, reloadShares] = useShares(phase !== "ended")
  const others = (shares ?? []).filter((s) => s.handle !== handle)
  const [h, setH] = useState<Hist>({ msgs: [], start: 0, more: false })
  const [tools, setTools] = useState(false)
  const [files, setFiles] = useState(false)
  const [loadingOlder, setLoadingOlder] = useState(false)
  const hRef = useRef(h)
  const seq = useRef(0)
  const sending = useRef(false)
  const [st, setSt] = useState<LiveState | null>(null)
  const [queue, setQueue] = useState<QItem[]>([])
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const end = useRef<HTMLDivElement>(null)
  // Suit la fin tant que l'invité y est ; dès qu'il remonte lire, la vue ne bouge plus jusqu'à ce qu'il redescende ou envoie un message.
  const stuck = useRef(true)
  useEffect(() => {
    hRef.current = h
  })

  const readMe = useCallback(async (): Promise<boolean> => {
    try {
      const r = await api.guestMe()
      setAuth({ state: "open", csrf: r.csrf })
      setMe((p) => (p && p.id === r.id && p.mode === r.mode && p.title === r.title && p.invite === r.invite ? p : { id: r.id, dir: r.dir, mode: r.mode, title: r.title, invite: r.invite }))
      setPhase((p) => (p === "loading" ? "on" : p))
      return false
    } catch (e) {
      if (!over(e)) return false
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
    const first = setTimeout(() => void readMe(), 0)
    const timer = setInterval(() => void readMe(), 5000)
    return () => {
      clearTimeout(first)
      clearInterval(timer)
    }
  }, [active, readMe])

  const id = me?.id ?? ""
  const dir = me?.dir ?? ""
  useEffect(() => {
    if (!active || !id) return
    let alive = true
    const load = () => {
      const gen = seq.current
      return Promise.all([api.conv({ dir, id }, tools, { limit: Math.max(PAGE, hRef.current.msgs.length + 50) }), api.sessionLive(dir, id), api.queue(id)]).then(
        ([c, l, q]) => {
          if (!alive) return
          if (gen === seq.current) {
            const next = { msgs: c.msgs, start: c.start ?? 0, more: !!c.more }
            setH((p) => (JSON.stringify(p) === JSON.stringify(next) ? p : next))
          }
          setSt((p) => (JSON.stringify(p) === JSON.stringify(l) ? p : l))
          setQueue(q.items ?? [])
        },
        (e) => alive && over(e) && setPhase("ended")
      )
    }
    void load()
    const timer = setInterval(() => document.visibilityState === "visible" && void load(), 2000)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [active, id, dir, tools])

  const older = async () => {
    if (loadingOlder || !h.more) return
    setLoadingOlder(true)
    try {
      const r = await api.conv({ dir, id }, tools, { limit: PAGE, before: h.start })
      seq.current++
      setH((p) => ({ msgs: [...r.msgs, ...p.msgs], start: r.start ?? 0, more: !!r.more }))
    } catch (e) {
      if (over(e)) setPhase("ended")
    }
    setLoadingOlder(false)
  }

  // Corps en bloc : dans Chrome, scrollIntoView renvoie une Promise que React prendrait pour un nettoyage et appellerait au message suivant.
  useEffect(() => {
    if (stuck.current) end.current?.scrollIntoView?.({ block: "end" })
  }, [h.msgs.length])
  // La bande d'état (outil, sous-agents, file d'attente) change de hauteur sans nouveau message : la vue reste collée à la fin.
  useEffect(() => {
    const vp = end.current?.closest<HTMLElement>('[data-slot="scroll-area-viewport"]')
    const body = vp?.firstElementChild
    if (!vp || !body) return
    const ro = new ResizeObserver(() => {
      if (stuck.current) vp.scrollTop = vp.scrollHeight
    })
    ro.observe(body)
    return () => ro.disconnect()
  }, [phase])

  const rw = me?.mode === "rw"
  const send = async () => {
    const v = text.trim()
    if (!v || sending.current || !rw) return
    sending.current = true
    stuck.current = true
    setBusy(true)
    let fail: string | null = null
    try {
      await api.queueAdd(id, v)
    } catch (e) {
      fail = errMsg(e)
    } finally {
      sending.current = false
      setBusy(false)
    }
    if (fail !== null) return void ((await readMe()) || toast.error(fail))
    setText("")
    api.queue(id).then((q) => setQueue(q.items ?? []), () => {})
  }
  const remove = async (q: QItem) => {
    try {
      await api.queueRm(id, q.id)
    } catch (e) {
      if (!(await readMe())) toast.error(errMsg(e))
    }
    api.queue(id).then((x) => setQueue(x.items ?? []), () => {})
  }
  const leave = async () => {
    try {
      await api.guestLeave()
      await reloadShares()
      setPhase("bye")
    } catch (e) {
      if (!(await readMe())) toast.error(errMsg(e))
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
      <DialogContent
        showCloseButton={false}
        onOpenAutoFocus={(e) => e.preventDefault()}
        onInteractOutside={(e) => e.preventDefault()}
        onEscapeKeyDown={(e) => e.preventDefault()}
        className="safe-t safe-b flex h-[calc(var(--vvh,100dvh)*0.94)] w-[96vw] max-w-none flex-col gap-0 overflow-hidden p-0 max-md:top-[var(--vvt,0px)] max-md:left-0 max-md:h-[var(--vvh,100dvh)] max-md:w-screen max-md:translate-x-0 max-md:translate-y-0 max-md:rounded-none max-md:border-0 sm:max-w-none"
      >
        <DialogHeader className="gap-1 border-b px-4 pt-2.5 pb-3 md:px-6 md:pb-4">
          <div className="flex items-start gap-4">
            <div className="min-w-0 flex-1 space-y-1">
              <DialogTitle className="truncate text-lg tracking-tight">{me.title || t("conversation.guest.title")}</DialogTitle>
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
                  {others.map((s) => (
                    <DropdownMenuItem key={s.handle} onSelect={() => location.assign(`/guest/${s.handle}`)}>
                      {s.title || t("conversation.guest.title")}
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
          onScrollCapture={(e) => {
            const v = e.target as HTMLElement
            stuck.current = v.scrollHeight - v.scrollTop - v.clientHeight < 80
          }}
        >
          <div className="session-text mx-auto max-w-5xl space-y-6 px-3 py-4 md:px-8 md:py-6">
            {h.more && (
              <div className="text-center">
                <Button variant="outline" size="sm" disabled={loadingOlder} onClick={() => void older()}>
                  {loadingOlder ? <Fan className="animate-spin" /> : <ArrowUpToLine />} {t("conversation.view.loadOlder")}
                </Button>
              </div>
            )}
            {h.msgs.map((m, i) => (
              <Message key={`${m.o ?? i}-${i}`} m={m} conv={{ dir, id, tools, guest: true }} />
            ))}
            <LiveBar live={{ st, open: true }} id={id} />
            <div ref={end} />
          </div>
        </ScrollArea>
        <div className="border-t bg-muted/20 px-4 py-3">
          <section aria-label={t("conversation.guest.queue")} className="mx-auto max-w-7xl space-y-2">
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
              maxLength={32000}
              aria-label={t("conversation.composer.messageAria")}
              placeholder={t("conversation.guest.placeholder")}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                  e.preventDefault()
                  void send()
                }
              }}
              className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border border-input bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/80 disabled:opacity-60"
            />
            {queue.length > 0 && (
              <ul className="space-y-1 text-xs" aria-label={t("conversation.composer.queueAria")}>
                {queue.map((q, i) => (
                  <li key={q.id} className="flex items-center gap-2 rounded-md border border-dashed px-2 py-1 text-muted-foreground">
                    <span className="shrink-0 tabular-nums">{i + 1}.</span>
                    <span className="shrink-0 rounded bg-muted px-1 text-2xs uppercase">{q.author === "guest" ? q.label || t("conversation.composer.guest") : t("conversation.guest.host")}</span>
                    <span className="min-w-0 flex-1 truncate" title={q.text}>
                      {q.text}
                    </span>
                    {q.state === "uncertain" && <span className="shrink-0 text-amber-600 dark:text-amber-400">{t("conversation.composer.sendUncertain")}</span>}
                    {q.state === "sending" && <span className="shrink-0">{t("conversation.composer.sending")}</span>}
                    {q.author === "guest" && q.guest === me.invite && q.state !== "sending" && (
                      <Button size="icon-xs" variant="ghost" aria-label={t("conversation.composer.dequeue")} onClick={() => void remove(q)}>
                        <X />
                      </Button>
                    )}
                  </li>
                ))}
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
