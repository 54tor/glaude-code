import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowUpToLine, Loader2, LogOut, Wrench, X } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { api, HttpError, type LiveState, type Msg, type QItem } from "@/lib/api"
import { setAuth } from "@/lib/auth"
import { LiveBar } from "./LiveStatus"
import { Message } from "./Message"

// Vue de l'invité (/guest) : transcript en lecture, file avec auteurs, composeur. Volontairement mince : aucune des routes
// qu'utilisent la barre latérale, Conversation ou le composeur hôte (api, info, git, docs, envoi direct) n'est refusée sans être appelée.
type Me = { id: string; dir: string; mode: "rw" | "ro" }
type Phase = "loading" | "on" | "ended" | "bye"
type Hist = { msgs: Msg[]; start: number; more: boolean }
const PAGE = 200

// 401/403 ou réponse non JSON : le serveur traite la requête comme celle d'un hôte, donc le partage n'existe plus.
const over = (e: unknown) => (e instanceof HttpError && (e.status === 401 || e.status === 403)) || e instanceof SyntaxError

export function GuestApp() {
  const { t } = useTranslation()
  const [phase, setPhase] = useState<Phase>("loading")
  const [me, setMe] = useState<Me | null>(null)
  const [h, setH] = useState<Hist>({ msgs: [], start: 0, more: false })
  const [tools, setTools] = useState(false)
  const [loadingOlder, setLoadingOlder] = useState(false)
  const hRef = useRef(h)
  const seq = useRef(0)
  const sending = useRef(false)
  const [st, setSt] = useState<LiveState | null>(null)
  const [queue, setQueue] = useState<QItem[]>([])
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const end = useRef<HTMLDivElement>(null)
  useEffect(() => {
    hRef.current = h
  })

  const readMe = useCallback(async (): Promise<boolean> => {
    try {
      const r = await api.guestMe()
      setAuth({ state: "open", csrf: r.csrf })
      setMe((p) => (p && p.id === r.id && p.mode === r.mode ? p : { id: r.id, dir: r.dir, mode: r.mode }))
      setPhase((p) => (p === "loading" ? "on" : p))
      return false
    } catch (e) {
      if (!over(e)) return false
      setPhase("ended")
      return true
    }
  }, [])

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

  useEffect(() => end.current?.scrollIntoView?.({ block: "end" }), [h.msgs.length])

  const rw = me?.mode === "rw"
  const send = async () => {
    const v = text.trim()
    if (!v || sending.current || !rw) return
    sending.current = true
    setBusy(true)
    const r = await api.queueAdd(id, v)
    sending.current = false
    setBusy(false)
    if (!r.ok) return void ((await readMe()) || toast.error(r.out))
    setText("")
    api.queue(id).then((q) => setQueue(q.items ?? []), () => {})
  }
  const remove = async (q: QItem) => {
    const r = await api.queueRm(id, q.id)
    if (!r.ok && !(await readMe())) toast.error(r.out)
    api.queue(id).then((x) => setQueue(x.items ?? []), () => {})
  }
  const leave = async () => {
    const r = await api.guestLeave()
    if (r.ok) setPhase("bye")
    else if (!(await readMe())) toast.error(r.out)
  }

  if (phase === "ended" || phase === "bye")
    return (
      <main className="flex min-h-svh flex-col items-center justify-center gap-2 p-6 text-center">
        <h1 className="text-lg font-semibold">{phase === "ended" ? t("conversation.guest.ended") : t("conversation.guest.bye")}</h1>
        {phase === "ended" && <p className="text-sm text-muted-foreground">{t("conversation.guest.endedHint")}</p>}
      </main>
    )
  if (!me)
    return (
      <main className="flex min-h-svh items-center justify-center gap-2 p-6 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" aria-hidden />
        {t("conversation.guest.loading")}
      </main>
    )

  return (
    <main className="mx-auto flex h-svh max-w-3xl flex-col px-4">
      <header className="flex items-center justify-between gap-2 py-3">
        <h1 className="min-w-0 flex-1 text-sm font-semibold">{t("conversation.guest.title")}</h1>
        <Button size="sm" variant={tools ? "secondary" : "ghost"} aria-pressed={tools} onClick={() => setTools(!tools)}>
          <Wrench /> {t("conversation.view.toolsShown")}
        </Button>
        <Button size="sm" variant="outline" onClick={() => void leave()}>
          <LogOut /> {t("conversation.guest.leave")}
        </Button>
      </header>
      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto pb-3">
        {h.more && (
          <div className="flex justify-center">
            <Button size="sm" variant="outline" disabled={loadingOlder} onClick={() => void older()}>
              {loadingOlder ? <Loader2 className="animate-spin" /> : <ArrowUpToLine />} {t("conversation.view.loadOlder")}
            </Button>
          </div>
        )}
        {h.msgs.map((m, i) => (
          <Message key={`${m.o ?? i}-${i}`} m={m} conv={{ dir, id, tools, guest: true }} />
        ))}
        <LiveBar live={{ st, open: true }} id={id} />
        <div ref={end} />
      </div>
      <section aria-label={t("conversation.guest.queue")} className="space-y-2 pb-3">
        {queue.length > 0 && (
          <ul className="space-y-1 text-sm" aria-label={t("conversation.composer.queueAria")}>
            {queue.map((q, i) => (
              <li key={q.id} className="flex items-center gap-2 rounded-md border border-dashed px-2 py-1 text-muted-foreground">
                <span className="shrink-0 tabular-nums">{i + 1}.</span>
                <span className="shrink-0 rounded bg-muted px-1 text-2xs uppercase">{q.author === "guest" ? t("conversation.composer.guest") : t("conversation.guest.host")}</span>
                <span className="min-w-0 flex-1 truncate" title={q.text}>
                  {q.text}
                </span>
                {q.state === "uncertain" && <span className="shrink-0 text-amber-600 dark:text-amber-400">{t("conversation.composer.sendUncertain")}</span>}
                {q.state === "sending" && <span className="shrink-0">{t("conversation.composer.sending")}</span>}
                {q.author === "guest" && q.state !== "sending" && (
                  <Button size="icon-xs" variant="ghost" aria-label={t("conversation.composer.dequeue")} onClick={() => void remove(q)}>
                    <X />
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        {!rw && <p className="rounded-md border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">{t("conversation.guest.readOnly")}</p>}
        <div className="flex items-end gap-2">
          <textarea
            value={text}
            disabled={!rw}
            rows={2}
            aria-label={t("conversation.composer.messageAria")}
            placeholder={t("conversation.guest.placeholder")}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                void send()
              }
            }}
            className="min-h-10 flex-1 resize-none rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
          />
          <Button disabled={!rw || busy || !text.trim()} onClick={() => void send()}>
            {t("conversation.composer.send")}
          </Button>
        </div>
      </section>
    </main>
  )
}
