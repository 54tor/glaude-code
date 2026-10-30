import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "wouter"
import { Confirm, type Ask } from "@/components/Confirm"
import { Button } from "@/components/ui/button"
import { useCan } from "@/lib/auth"
import { errMsg } from "@/lib/apiError"
import { teamClose, teamGet, teamKey, teamScreen, type TeamView } from "@/lib/teams"
import { AgentList, agentLabel } from "./AgentList"
import { MessageBox } from "./MessageBox"

const WIDE = 1024
const POLL_MS = 2000

// Largeur du conteneur (pas de la fenêtre) ; sans ResizeObserver (jsdom), 0 : disposition B.
function useWidth(ref: React.RefObject<HTMLElement | null>) {
  const [w, setW] = useState(0)
  useEffect(() => {
    if (typeof ResizeObserver === "undefined" || !ref.current) return
    const ro = new ResizeObserver((es) => setW(es[es.length - 1].contentRect.width))
    ro.observe(ref.current)
    return () => ro.disconnect()
  }, [ref])
  return w
}

function Messages({ view, who }: { view: TeamView; who?: string }) {
  const { t } = useTranslation()
  const dn = (n: string) => view.Agents.find((a) => a.Name === n)?.Display ?? n
  const all = [
    ...view.Messages.map((m, i) => ({ k: "m" + (m.ID || i), from: m.From, to: m.To, text: m.Text || m.Summary, at: m.At })),
    ...view.Direct.map((m, i) => ({ k: "d" + i, from: m.Author, to: m.To, text: m.Text, at: m.At })),
  ]
    .filter((m) => !who || m.from === who || m.to === who)
    .sort((a, b) => a.at - b.at)
  if (!all.length) return <p className="p-3 text-sm text-muted-foreground">{t("teams.none")}</p>
  return (
    <ul className="divide-y text-sm">
      {all.map((m) => (
        <li key={m.k} className="space-y-0.5 px-3 py-2">
          <div className="text-xs text-muted-foreground">
            <b>{dn(m.from)}</b> → {dn(m.to)}
          </div>
          <div className="whitespace-pre-wrap break-words">{m.text}</div>
        </li>
      ))}
    </ul>
  )
}

function Tasks({ view }: { view: TeamView }) {
  const { t } = useTranslation()
  if (!view.Tasks.length) return <p className="p-3 text-sm text-muted-foreground">{t("teams.none")}</p>
  return (
    <ul className="divide-y text-sm">
      {view.Tasks.map((k) => (
        <li key={k.ID} className="flex gap-2 px-3 py-2">
          <span className="flex-1">{k.Title}</span>
          <span className="text-xs text-muted-foreground">{k.Status}</span>
        </li>
      ))}
    </ul>
  )
}

const Col = ({ label, children }: { label: string; children: React.ReactNode }) => (
  <section aria-label={label} className="min-w-0 overflow-y-auto rounded-lg border">
    <h3 className="border-b px-3 py-2 text-xs font-semibold uppercase text-muted-foreground">{label}</h3>
    {children}
  </section>
)

export function TeamRun({ name }: { name: string }) {
  const { t } = useTranslation()
  const can = useCan()
  const [view, setView] = useState<TeamView | null>(null)
  const [err, setErr] = useState("")
  const [to, setTo] = useState("")
  const [sel, setSel] = useState("")
  const [mode, setMode] = useState<"mine" | "tasks" | "all" | "screen">("mine")
  const [screen, setScreen] = useState("")
  const [ask, setAsk] = useState<Ask | null>(null)
  const root = useRef<HTMLDivElement>(null)
  const width = useWidth(root)
  const gen = useRef(0) // avance autour de chaque touche : une relève partie avant sa réponse montre l'écran d'avant
  const pollRef = useRef<() => Promise<void>>(async () => {})
  const keyBusy = useRef(false)
  const screenBusy = useRef(false)
  const [stale, setStale] = useState(false) // Oui/Non masqués jusqu'à une relève partie après la réponse à la touche
  const [actErr, setActErr] = useState("")

  useEffect(() => {
    // jeton propre à ce nom : une réponse de l'équipe précédente est jetée, et rien de son état ne survit
    const ctl = { alive: true, inflight: false }
    setView(null) // eslint-disable-line react-hooks/set-state-in-effect
    setErr("")
    setActErr("")
    setTo("")
    setSel("")
    setMode("mine")
    setScreen("")
    setStale(false)
    const poll = async () => {
      if (ctl.inflight || document.visibilityState !== "visible") return
      ctl.inflight = true
      const g = gen.current
      let v: TeamView | undefined
      let msg = ""
      try {
        v = await teamGet(name)
      } catch (e) {
        msg = errMsg(e)
      }
      ctl.inflight = false
      if (!ctl.alive) return
      if (v) {
        setView(v)
        setErr("")
        if (g === gen.current && !keyBusy.current) {
          setStale(false)
        }
      } else setErr(msg)
    }
    pollRef.current = poll
    void poll()
    const iv = setInterval(() => void poll(), POLL_MS)
    const vis = () => void poll()
    document.addEventListener("visibilitychange", vis)
    return () => {
      ctl.alive = false
      clearInterval(iv)
      document.removeEventListener("visibilitychange", vis)
    }
  }, [name])

  const lead = view?.Agents.find((a) => a.Lead)?.Name ?? view?.Agents[0]?.Name ?? ""
  const recipient = view?.Agents.some((a) => a.Name === to) ? to : lead
  const exec = can("exec")
  const live = !!view?.Live
  const act = exec && live

  const key = async (k: string) => {
    if (keyBusy.current || !view?.Permission) return
    keyBusy.current = true
    gen.current++
    setStale(true)
    setActErr("")
    let msg = ""
    try {
      await teamKey(name, lead, k, view.Permission)
    } catch (e) {
      msg = errMsg(e)
    }
    gen.current++
    keyBusy.current = false
    if (msg) setActErr(msg)
    void pollRef.current()
  }
  const pick = (n: string) => {
    setSel(n)
    setMode("mine")
  }
  const showScreen = async (agent: string) => {
    if (screenBusy.current) return
    screenBusy.current = true
    setMode("screen")
    setScreen("")
    let text: string
    try {
      text = (await teamScreen(name, agent)).text
    } catch (e) {
      text = errMsg(e)
    }
    screenBusy.current = false
    setScreen(text)
  }

  const perm = view?.Permission ? (
    <div className="space-y-2 rounded-lg border border-amber-500/60 p-3">
      <div className="text-xs font-semibold uppercase text-muted-foreground">{t("teams.permission")}</div>
      <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words text-xs">{view.Permission}</pre>
      {view.PermissionTruncated && <p className="text-xs text-destructive">{t("teams.permTruncated")}</p>}
      {exec && (
        <div className="flex gap-2">
          <Button size="sm" disabled={!live || stale || view.PermissionTruncated} onClick={() => void key("1")}>
            {t("teams.yes")}
          </Button>
          <Button size="sm" variant="outline" disabled={!live || stale} onClick={() => void key("Escape")}>
            {t("teams.no")}
          </Button>
        </div>
      )}
    </div>
  ) : live && ["waiting", "unknown"].includes(view?.Agents.find((a) => a.Lead)?.State ?? "") ? (
    <p className="text-sm text-muted-foreground">{t("teams.permTerminal")}</p>
  ) : null

  if (!view) return <div ref={root}>{err && <p className="p-4 text-sm text-destructive">{err}</p>}</div>
  const wide = width >= WIDE
  const mate = view.Agents.find((a) => a.Name === sel)
  const box = exec && (
    <MessageBox name={name} agents={view.Agents} to={recipient} setTo={setTo} queued={view.Queued} disabled={!act} />
  )

  return (
    <div ref={root} className="flex h-full flex-col gap-3 p-4">
      <div className="flex items-center gap-3">
        <Link href="/equipes" className="text-sm text-muted-foreground hover:underline">
          ← {t("teams.back")}
        </Link>
        <h2 className="font-semibold">{view.Name}</h2>
        {!live && <span className="text-xs text-muted-foreground">{t("teams.finished")}</span>}
        {view.Unknown.length > 0 && (
          <details className="text-xs">
            <summary className="cursor-pointer">{t("teams.unknownFormat")}</summary>
            <pre className="whitespace-pre-wrap">{view.Unknown.join("\n")}</pre>
          </details>
        )}
        {exec && (
          <Button
            size="sm"
            variant="outline"
            className="ml-auto"
            onClick={() => setAsk({ title: t("teams.close"), body: view.Name, confirm: t("teams.close"), run: () => void teamClose(name).then(() => void pollRef.current(), (e) => setActErr(errMsg(e))) })}
          >
            {t("teams.close")}
          </Button>
        )}
      </div>
      {(actErr || err) && <p className="text-sm text-destructive">{actErr || err}</p>}
      {perm}
      {wide ? (
        <div className="grid min-h-0 flex-1 grid-cols-3 gap-3">
          <Col label={t("teams.agents")}>
            <AgentList agents={view.Agents} />
          </Col>
          <Col label={t("teams.messages")}>
            <Messages view={view} />
          </Col>
          <Col label={t("teams.tasks")}>
            <Tasks view={view} />
          </Col>
        </div>
      ) : (
        <div className="space-y-3">
          <div className="rounded-lg border">
            <AgentList agents={view.Agents} sel={sel} onPick={pick} />
          </div>
          {mate && (
            <div className="rounded-lg border">
              <div className="flex flex-wrap gap-2 border-b p-2">
                <span className="mr-auto px-1 text-sm">
                  {agentLabel(mate)}
                </span>
                {act && (
                  <Button size="sm" variant="outline" onClick={() => setTo(mate.Name)}>
                    {t("teams.writeTo")}
                  </Button>
                )}
                <Button size="sm" variant="outline" onClick={() => setMode("tasks")}>
                  {t("teams.tasks")}
                </Button>
                <Button size="sm" variant="outline" onClick={() => setMode("all")}>
                  {t("teams.allMessages")}
                </Button>
                <Button size="sm" variant="outline" disabled={!live} onClick={() => void showScreen(mate.Name)}>
                  {t("teams.screen")}
                </Button>
              </div>
              {mode === "tasks" ? <Tasks view={view} /> : mode === "screen" ? <pre className="max-h-64 overflow-auto whitespace-pre-wrap p-3 text-xs">{screen}</pre> : <Messages view={view} who={mode === "all" ? undefined : mate.Name} />}
            </div>
          )}
        </div>
      )}
      {box}
      <Confirm ask={ask} onClose={() => setAsk(null)} />
    </div>
  )
}
