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
  const [containerWidth, setContainerWidth] = useState(0)
  useEffect(() => {
    if (typeof ResizeObserver === "undefined" || !ref.current) return
    const resizeObserver = new ResizeObserver((entries) => {
      const latestEntry = entries[entries.length - 1]
      setContainerWidth(latestEntry.contentRect.width)
    })
    resizeObserver.observe(ref.current)
    return () => resizeObserver.disconnect()
  }, [ref])
  return containerWidth
}

function Messages({ view, who }: { view: TeamView; who?: string }) {
  const { t } = useTranslation()
  const displayName = (agentName: string) =>
    view.Agents.find((agent) => agent.Name === agentName)?.Display ?? agentName
  const teamMessages = view.Messages.map((message, messageIndex) => ({
    key: "m" + (message.ID || messageIndex),
    from: message.From,
    to: message.To,
    text: message.Text || message.Summary,
    at: message.At,
  }))
  const directMessages = view.Direct.map((message, messageIndex) => ({
    key: "d" + messageIndex,
    from: message.Author,
    to: message.To,
    text: message.Text,
    at: message.At,
  }))
  const involvesFilteredAgent = (message: { from: string; to: string }) =>
    !who || message.from === who || message.to === who
  const visibleMessages = [...teamMessages, ...directMessages]
    .filter(involvesFilteredAgent)
    .sort((earlierMessage, laterMessage) => earlierMessage.at - laterMessage.at)
  if (!visibleMessages.length) return <p className="p-3 text-sm text-muted-foreground">{t("teams.none")}</p>
  return (
    <ul className="divide-y text-sm">
      {visibleMessages.map((message) => (
        <li key={message.key} className="space-y-0.5 px-3 py-2">
          <div className="text-xs text-muted-foreground">
            <b>{displayName(message.from)}</b> → {displayName(message.to)}
          </div>
          <div className="whitespace-pre-wrap break-words">{message.text}</div>
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
      {view.Tasks.map((task) => (
        <li key={task.ID} className="flex gap-2 px-3 py-2">
          <span className="flex-1">{task.Title}</span>
          <span className="text-xs text-muted-foreground">{task.Status}</span>
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
    const pollControl = { alive: true, inflight: false }
    setView(null) // eslint-disable-line react-hooks/set-state-in-effect
    setErr("")
    setActErr("")
    setTo("")
    setSel("")
    setMode("mine")
    setScreen("")
    setStale(false)
    const poll = async () => {
      if (pollControl.inflight || document.visibilityState !== "visible") return
      pollControl.inflight = true
      const generationAtStart = gen.current
      let polledView: TeamView | undefined
      let pollErrorMessage = ""
      try {
        polledView = await teamGet(name)
      } catch (pollError) {
        pollErrorMessage = errMsg(pollError)
      }
      pollControl.inflight = false
      if (!pollControl.alive) return
      if (!polledView) {
        setErr(pollErrorMessage)
        return
      }
      setView(polledView)
      setErr("")
      const startedAfterLastKey = generationAtStart === gen.current && !keyBusy.current
      if (startedAfterLastKey) setStale(false)
    }
    pollRef.current = poll
    void poll()
    const pollInterval = setInterval(() => void poll(), POLL_MS)
    const onVisibilityChange = () => void poll()
    document.addEventListener("visibilitychange", onVisibilityChange)
    return () => {
      pollControl.alive = false
      clearInterval(pollInterval)
      document.removeEventListener("visibilitychange", onVisibilityChange)
    }
  }, [name])

  const leadAgent = view?.Agents.find((agent) => agent.Lead)
  const lead = leadAgent?.Name ?? view?.Agents[0]?.Name ?? ""
  const isKnownRecipient = view?.Agents.some((agent) => agent.Name === to)
  const recipient = isKnownRecipient ? to : lead
  const exec = can("exec")
  const live = !!view?.Live
  const act = exec && live

  const sendPermissionKey = async (keyName: string) => {
    if (keyBusy.current || !view?.Permission) return
    keyBusy.current = true
    gen.current++
    setStale(true)
    setActErr("")
    let keyErrorMessage = ""
    try {
      await teamKey(name, lead, keyName, view.Permission)
    } catch (keyError) {
      keyErrorMessage = errMsg(keyError)
    }
    gen.current++
    keyBusy.current = false
    if (keyErrorMessage) setActErr(keyErrorMessage)
    void pollRef.current()
  }
  const pickAgent = (agentName: string) => {
    setSel(agentName)
    setMode("mine")
  }
  const showScreen = async (agent: string) => {
    if (screenBusy.current) return
    screenBusy.current = true
    setMode("screen")
    setScreen("")
    let screenText: string
    try {
      const screenResponse = await teamScreen(name, agent)
      screenText = screenResponse.text
    } catch (screenError) {
      screenText = errMsg(screenError)
    }
    screenBusy.current = false
    setScreen(screenText)
  }

  const leadState = leadAgent?.State ?? ""
  const isLeadWaitingInTerminal = live && ["waiting", "unknown"].includes(leadState)
  const renderPermission = () => {
    if (!view?.Permission) {
      if (!isLeadWaitingInTerminal) return null
      return <p className="text-sm text-muted-foreground">{t("teams.permTerminal")}</p>
    }
    return (
      <div className="space-y-2 rounded-lg border border-amber-500/60 p-3">
        <div className="text-xs font-semibold uppercase text-muted-foreground">{t("teams.permission")}</div>
        <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words text-xs">{view.Permission}</pre>
        {view.PermissionTruncated && <p className="text-xs text-destructive">{t("teams.permTruncated")}</p>}
        {exec && (
          <div className="flex gap-2">
            <Button size="sm" disabled={!live || stale || view.PermissionTruncated} onClick={() => void sendPermissionKey("1")}>
              {t("teams.yes")}
            </Button>
            <Button size="sm" variant="outline" disabled={!live || stale} onClick={() => void sendPermissionKey("Escape")}>
              {t("teams.no")}
            </Button>
          </div>
        )}
      </div>
    )
  }
  const perm = renderPermission()

  if (!view) return <div ref={root}>{err && <p className="p-4 text-sm text-destructive">{err}</p>}</div>
  const wide = width >= WIDE
  const mate = view.Agents.find((agent) => agent.Name === sel)
  const box = exec && (
    <MessageBox name={name} agents={view.Agents} to={recipient} setTo={setTo} queued={view.Queued} disabled={!act} />
  )
  const askCloseTeam = () =>
    setAsk({
      title: t("teams.close"),
      body: view.Name,
      confirm: t("teams.close"),
      run: () =>
        void teamClose(name).then(
          () => void pollRef.current(),
          (closeError) => setActErr(errMsg(closeError))
        ),
    })
  const renderMateDetail = (mateName: string) => {
    if (mode === "tasks") return <Tasks view={view} />
    if (mode === "screen") return <pre className="max-h-64 overflow-auto whitespace-pre-wrap p-3 text-xs">{screen}</pre>
    const messageFilter = mode === "all" ? undefined : mateName
    return <Messages view={view} who={messageFilter} />
  }

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
            onClick={askCloseTeam}
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
            <AgentList agents={view.Agents} sel={sel} onPick={pickAgent} />
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
              {renderMateDetail(mate.Name)}
            </div>
          )}
        </div>
      )}
      {box}
      <Confirm ask={ask} onClose={() => setAsk(null)} />
    </div>
  )
}
