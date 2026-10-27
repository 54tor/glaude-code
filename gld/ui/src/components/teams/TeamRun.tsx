import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "wouter"
import { Confirm, type Ask } from "@/components/Confirm"
import { Button } from "@/components/ui/button"
import { useCan } from "@/lib/auth"
import { teamClose, teamGet, teamKey, teamScreen, type TeamView } from "@/lib/teams"
import { AgentList } from "./AgentList"
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
            <b>{m.from}</b> → {m.to}
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
  const [keyBusy, setKeyBusy] = useState(false)
  const [ask, setAsk] = useState<Ask | null>(null)
  const root = useRef<HTMLDivElement>(null)
  const width = useWidth(root)
  const inflight = useRef(false)
  const alive = useRef(true)

  const poll = useCallback(async () => {
    if (inflight.current || document.visibilityState !== "visible") return
    inflight.current = true
    const r = await teamGet(name)
    inflight.current = false
    if (!alive.current) return
    if (r.ok) {
      setView(r.view)
      setErr("")
    } else setErr(r.out)
  }, [name])
  useEffect(() => {
    alive.current = true
    void poll()
    const iv = setInterval(() => void poll(), POLL_MS)
    const vis = () => void poll()
    document.addEventListener("visibilitychange", vis)
    return () => {
      alive.current = false
      clearInterval(iv)
      document.removeEventListener("visibilitychange", vis)
    }
  }, [poll])

  const lead = view?.Agents.find((a) => a.Lead)?.Name ?? view?.Agents[0]?.Name ?? ""
  const recipient = view?.Agents.some((a) => a.Name === to) ? to : lead
  const exec = can("exec")
  const live = !!view?.Live
  const act = exec && live

  const key = async (k: string) => {
    setKeyBusy(true)
    const r = await teamKey(name, lead, k)
    setKeyBusy(false)
    if (!r.ok) setErr(r.out)
    else void poll()
  }
  const pick = (n: string) => {
    setSel(n)
    setMode("mine")
  }
  const showScreen = async (agent: string) => {
    setMode("screen")
    const r = await teamScreen(name, agent)
    setScreen(r.ok ? r.text : r.out)
  }

  const perm = view?.Permission ? (
    <div className="space-y-2 rounded-lg border border-amber-500/60 p-3">
      <div className="text-xs font-semibold uppercase text-muted-foreground">{t("teams.permission")}</div>
      <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words text-xs">{view.Permission}</pre>
      {exec && (
        <div className="flex gap-2">
          <Button size="sm" disabled={!live || keyBusy} onClick={() => void key("1")}>
            {t("teams.yes")}
          </Button>
          <Button size="sm" variant="outline" disabled={!live || keyBusy} onClick={() => void key("Escape")}>
            {t("teams.no")}
          </Button>
        </div>
      )}
    </div>
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
            disabled={!live}
            onClick={() => setAsk({ title: t("teams.close"), body: view.Name, confirm: t("teams.close"), run: () => void teamClose(name).then((r) => (r.ok ? void poll() : setErr(r.out))) })}
          >
            {t("teams.close")}
          </Button>
        )}
      </div>
      {err && <p className="text-sm text-destructive">{err}</p>}
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
                  <b>{mate.Name}</b> · {mate.Role}
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
