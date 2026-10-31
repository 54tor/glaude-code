import { i18n, loc } from "@/i18n"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { BarChart3, Bot, Fan, OctagonAlert, Wrench } from "lucide-react"
import { api, type Detail, type LiveState, type Msg, type Session, type SubLive, type Tokens } from "@/lib/api"
import { compact, full, tilde, tokTotal } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Markdown, Message } from "./Message"
import { Tagged, isServiceMsg } from "./Tagged"
import { stopTask } from "./LiveTasks"
import { CacheState } from "./KeepWarm"
import { useActions } from "./actions"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

const SECONDS_PER_MINUTE = 60
const SECONDS_PER_HOUR = 3600
const LIVE_REFRESH_MS = 2000
const SUBAGENT_REFRESH_MS = 3000
const ROTATION_PERIOD_SECONDS = 6
const HASH_MULTIPLIER = 0x45d9f3b
const SUBAGENT_MESSAGE_LIMIT = 300
const STICK_TO_BOTTOM_THRESHOLD_PX = 120

const formatSeconds = (seconds: number) => {
  if (seconds < SECONDS_PER_MINUTE) return `${Math.max(0, Math.round(seconds))} s`
  if (seconds < SECONDS_PER_HOUR) return `${Math.floor(seconds / SECONDS_PER_MINUTE)} min ${Math.round(seconds % SECONDS_PER_MINUTE)} s`
  const hours = Math.floor(seconds / SECONDS_PER_HOUR)
  const minutes = Math.floor((seconds % SECONDS_PER_HOUR) / SECONDS_PER_MINUTE)
  return `${hours} h ${minutes} min`
}

// État de la session affichée (relu toutes les 2 s tant qu'elle est ouverte) : useLive le lit, LiveBar montre ce que Claude fait,
// LiveStats (dans la barre d'outils) le contexte, les tokens et le bouton « Stats ». Ancien résumé : : ce que Claude fait en ce moment (réflexion, outil, attente) et un bouton « Stats » qui ouvre
// la consommation de tokens et les sous-agents dans une fenêtre. Relu toutes les 2 s tant que la session est ouverte (fichiers
// locaux seulement), une seule fois sinon.
export type Live = { st: LiveState | null; open: boolean }

export function useLive(dir: string, id: string, open: boolean): Live {
  const [st, setSt] = useState<LiveState | null>(null)
  useEffect(() => {
    if (!id) return
    let isStillMounted = true
    // Un relevé identique garde l'ancien objet : pas de nouveau rendu de la conversation toutes les 2 s.
    const keepPreviousIfIdentical = (nextState: LiveState) => (previousState: LiveState | null) =>
      JSON.stringify(previousState) === JSON.stringify(nextState) ? previousState : nextState
    const load = () => api.sessionLive(dir, id).then((liveState) => isStillMounted && setSt(keepPreviousIfIdentical(liveState)), () => {})
    load()
    if (!open) return () => void (isStillMounted = false)
    const refreshInterval = setInterval(() => document.visibilityState === "visible" && load(), LIVE_REFRESH_MS)
    return () => {
      isStillMounted = false
      clearInterval(refreshInterval)
    }
  }, [dir, id, open])
  return { st, open }
}

// Horloge d'une seconde, tenue dans le composant qui l'affiche : la conversation ne se redessine pas à chaque tic.
function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now() / 1000)
  useEffect(() => {
    if (!active) return
    const tickInterval = setInterval(() => setNow(Date.now() / 1000), 1000)
    return () => clearInterval(tickInterval)
  }, [active])
  return now
}

// Les états génériques (réflexion, travail) et les outils en cours tirent une formule au hasard toutes les 6 s : le tirage dépend
// de la tranche de 6 s et de la session, donc il est stable entre deux rendus et diffère d'une session à l'autre.
function rotate(phrases: string[], id: string, now: number) {
  const timeSlice = Math.floor(now / ROTATION_PERIOD_SECONDS) >>> 0
  let hash = [...id].reduce((hashSoFar, character) => (hashSoFar * 31 + character.charCodeAt(0)) >>> 0, timeSlice)
  hash = Math.imul(hash ^ (hash >>> 16), HASH_MULTIPLIER)
  hash = Math.imul(hash ^ (hash >>> 16), HASH_MULTIPLIER)
  const phraseIndex = ((hash ^ (hash >>> 16)) >>> 0) % phrases.length
  return phrases[phraseIndex] + "…"
}

function label(liveState: LiveState, id: string, now: number) {
  const usesRotatingVerbs = liveState.thinking && liveState.generic && liveState.verbs?.length
  if (usesRotatingVerbs) return rotate(liveState.verbs!, id, now)
  if (liveState.activity) return liveState.activity
  return liveState.thinking ? i18n.t("conversation.live.working") : i18n.t("conversation.live.open")
}

// Un outil en cours : une des formules de « conversation.live.execTools », à défaut « exécute un outil… ».
function execLabel(id: string, now: number) {
  const phrases = i18n.t("conversation.live.execTools", { returnObjects: true })
  const hasPhrases = Array.isArray(phrases) && phrases.length > 0
  return hasPhrases ? rotate(phrases as string[], id, now) : i18n.t("conversation.live.execTool")
}

// Ce que Claude fait en ce moment, en dernière ligne de la transcription. `open` (la liste des sessions ouvertes, mise à jour dès la fermeture) prime sur le relevé :
// le processus met un instant à finir après SIGTERM.
export function LiveBar({ live: { st, open }, id }: { live: Live; id: string }) {
  const { t } = useTranslation()
  const now = useNow(open)
  const [expanded, setExpanded] = useState(false)
  if (!st) return null
  if (!(open && st.open)) return null // fermée : « Session fermée » est dans la barre d'outils (LiveStats), sans bande
  const isOpen = true
  const isTool = st.thinking && !!st.tool
  let detail = st.said
  if (isTool) detail = st.tool
  else if (st.thinking) detail = ""
  return (
    <div className="space-y-2">
      {st.queue?.map((queuedMessage) => (
        <div key={queuedMessage.ts + queuedMessage.t} className="mb-3 flex justify-end" title={new Date(queuedMessage.ts).toLocaleString(loc())}>
          <div className={cn("msg relative max-w-[85%] break-words text-muted-foreground", isServiceMsg(queuedMessage.t) ? "w-full" : "rounded-lg bg-muted/30 px-4 py-2.5 whitespace-pre-wrap outline-1 -outline-offset-1 outline-dashed outline-border")}>
            {isServiceMsg(queuedMessage.t) ? <Tagged t={queuedMessage.t} md={(markdownText) => <Markdown t={markdownText} />} /> : queuedMessage.t}
            <span className="absolute top-full right-2 mt-0.5 text-2xs font-medium tracking-wide whitespace-nowrap uppercase">{t("conversation.live.queued")}</span>
          </div>
        </div>
      ))}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 py-1 text-sm">
        {isOpen && (
          <span className={cn("flex min-w-0 items-center gap-1.5 font-medium", st.thinking ? "text-success" : "text-warning")}>
            {st.thinking ? <Fan className="size-3.5 shrink-0 animate-spin" /> : <span className="size-1.5 shrink-0 rounded-full bg-current" />}
            <span className="truncate" title={st.activity}>
              {st.tool && st.exec ? execLabel(id, now) : label(st, id, now)}
            </span>
            {st.since > 0 && st.thinking && <span className="shrink-0 font-normal text-muted-foreground">{t("conversation.live.since", { time: formatSeconds(now - st.since) })}</span>}
          </span>
        )}
        {/* Un seul emplacement d'une ligne : l'outil en cours, ou le dernier message de Claude quand il attend. Sa hauteur est toujours
            réservée (même sans rien à montrer) pour que la page ne saute pas ; un clic le déplie. */}
        {detail && <button
          type="button"
          onClick={() => setExpanded((wasExpanded) => !wasExpanded)}
          aria-expanded={expanded}
          title={detail}
          className={cn(
            "msg flex min-w-0 basis-full items-start gap-2 border-l-2 py-0.5 pl-3 text-left text-xs leading-4 text-muted-foreground",
            isTool ? "" : "border-warning/60",
            isTool && st.toolRunning && "animate-pulse rounded-r-md border-success bg-success/10 text-foreground",
            "cursor-pointer hover:text-foreground"
          )}
        >
          {isTool && st.toolRunning && <Fan className="mt-px size-3.5 shrink-0 animate-spin text-success" aria-hidden />}
          {isTool && <Wrench className="mt-px size-3.5 shrink-0" aria-label={st.toolRunning ? t("conversation.live.toolRunning") : t("conversation.live.lastTool")} />}
          <span className={cn("min-w-0 flex-1", expanded ? "max-h-60 overflow-y-auto break-words whitespace-pre-wrap" : "truncate")}>{detail}</span>
        </button>}
        {st.subs
          .filter((subagent) => subagent.running)
          .map((subagent) => (
            <div key={subagent.id} className="flex min-w-0 basis-full items-center gap-2 pl-3 text-xs text-muted-foreground" title={subagent.activity}>
              <Fan className="size-3.5 shrink-0 animate-spin text-success" />
              <span className="shrink-0 font-medium text-foreground">{subagent.desc || subagent.type}</span>
              <span className="min-w-0 truncate">{subagent.activity}</span>
            </div>
          ))}
        {st.tasks
          ?.filter((task) => task.status === "running")
          .map((task) => (
            <div key={task.id} className="flex min-w-0 basis-full items-center gap-2 pl-3 text-xs text-muted-foreground" title={task.desc}>
              <Fan className="size-3.5 shrink-0 animate-spin text-success" />
              <span className="min-w-0 truncate font-medium text-foreground">{task.desc || task.id}</span>
            </div>
          ))}
      </div>
    </div>
  )
}

// Bouton de l'en-tête : contexte et tokens, ouvre la fenêtre « Stats ».
export function StatsButton({ live: { st }, s: session, onClick }: { live: Live; s: Session; onClick: () => void }) {
  const { t } = useTranslation()
  return (
    <Button size="xs" variant="ghost" className="tabular-nums max-md:hidden" onClick={onClick} title={t("conversation.live.statsTitle")}>
      <BarChart3 />
      {t("conversation.live.stats")} :{st && st.context > 0 && <> ctx {compact(st.context)}</>} {compact(tokTotal(session.tok as Tokens))} tok
    </Button>
  )
}

// Fenêtre « Stats » : faits de la session, cache et maintien au chaud, consommation, dernier appel, sous-agents.
// shown/onShown : ouverte depuis l'en-tête du lecteur ou le menu « … » (petit écran).
export function LiveStats({ live: { st }, s: session, project, detail, shown, onShown }: { live: Live; s: Session; project: string; detail: Detail | null; shown: boolean; onShown: (flag: boolean) => void }) {
  const { t } = useTranslation()
  const actions = useActions()
  const [sub, setSub] = useState<SubLive | null>(null)
  const tok = session.tok as Tokens
  const isSessionOpen = actions.live.has(session.id)
  const facts = [
    [t("shell.info.id"), <code key="i" className="text-xs break-all">{session.id}</code>],
    [t("shell.info.cwd"), tilde(session.cwd, actions.home)],
    [t("shell.info.project"), tilde(project, actions.home)],
    [t("shell.info.modified"), full(session.mtime)],
    [t("shell.info.state"), isSessionOpen ? t("shell.info.open") : t("shell.info.closed")],
    isSessionOpen ? [t("shell.info.remote"), actions.rc.has(session.id) ? t("shell.info.remoteOn") : t("shell.info.remoteOff")] : null,
    detail ? [t("shell.info.prompts"), String(detail.n)] : null,
    detail?.subagents ? [t("shell.info.subagents"), String(detail.subagents)] : null,
    detail?.branch ? [t("shell.info.branch"), detail.branch] : null,
    detail?.models.length ? [t("shell.info.models"), detail.models.filter((model) => !model.startsWith("<")).join(", ")] : null,
    detail?.t0 ? [t("shell.info.started"), full(detail.t0)] : null,
    detail?.t1 ? [t("shell.info.last"), full(detail.t1)] : null,
  ].filter(Boolean) as [string, React.ReactNode][]
  const isSubRunning = !!st?.subs.find((subagent) => subagent.id === sub?.id)?.running
  return (
    <>
      <Dialog open={shown} onOpenChange={onShown}>
        <DialogContent className="flex max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle className="truncate pr-8">{session.title}</DialogTitle>
            <DialogDescription className="sr-only">{t("conversation.live.statsDescription")}</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 space-y-5 overflow-x-hidden overflow-y-auto px-6 py-5 text-sm">
            <section>
              <h3 className="mb-1 font-medium">{t("conversation.live.sessionHeading")}</h3>
              <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5">
                {facts.map(([factLabel, factValue]) => (
                  <div key={factLabel} className="contents">
                    <dt className="text-muted-foreground">{factLabel}</dt>
                    <dd className="min-w-0 break-words">{factValue}</dd>
                  </div>
                ))}
              </dl>
            </section>
            <section className="space-y-2">
              <h3 className="font-medium">{t("conversation.live.cacheHeading")}</h3>
              <div className="flex flex-wrap items-center gap-3">
                <CacheState s={session} />
              </div>
            </section>
            <section>
              <h3 className="mb-1 font-medium">{t("conversation.live.usage")}</h3>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-[auto_1fr_auto_1fr]">
                <dt className="text-muted-foreground">{t("conversation.live.input")}</dt>
                <dd className="tabular-nums">{compact(tok.in)}</dd>
                <dt className="text-muted-foreground">{t("conversation.live.output")}</dt>
                <dd className="tabular-nums">{compact(tok.out)}</dd>
                <dt className="text-muted-foreground">{t("conversation.live.cacheRead")}</dt>
                <dd className="tabular-nums">{compact(tok.cr)}</dd>
                <dt className="text-muted-foreground">{t("conversation.live.cacheWrite")}</dt>
                <dd className="tabular-nums">{compact(tok.cw)}</dd>
              </dl>
              <p className="mt-1 text-xs text-muted-foreground" title={t("conversation.live.processedTitle")}>
                {t("conversation.live.processed", { n: compact(tokTotal(tok)) })}
              </p>
            </section>
            {st && (st.context > 0 || st.model) && (
              <section>
                <h3 className="mb-1 font-medium">{t("conversation.live.lastCall")}</h3>
                {st.model && (
                  <p>
                    {t("conversation.live.model")} <code className="text-xs">{st.model}</code>
                  </p>
                )}
                {st.context > 0 && <p>{t("conversation.live.contextSent", { n: compact(st.context) })}</p>}
                {st.out > 0 && <p>{t("conversation.live.tokensOut", { n: compact(st.out) })}</p>}
              </section>
            )}
            {st && st.subs.length > 0 && (
              <section>
                <h3 className="mb-1 font-medium">{t("conversation.live.subagents", { n: st.subs.length })}</h3>
                <SubList subs={st.subs} onPick={setSub} session={session.id} />
              </section>
            )}
          </div>
        </DialogContent>
      </Dialog>
      <SubView dir={session.dir} id={session.id} sub={sub} running={isSubRunning} onClose={() => setSub(null)} />
    </>
  )
}

// Liste des sous-agents ; un clic en ouvre la conversation.
function SubList({ subs, onPick, session }: { subs: SubLive[]; onPick: (subagent: SubLive) => void; session: string }) {
  const { t } = useTranslation()
  return (
    <ul className="space-y-1.5">
      {subs.map((subagent) => (
        <li key={subagent.id} className="flex items-center gap-1">
          <button type="button" onClick={() => onPick(subagent)} title={t("conversation.live.subOpen")} className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 rounded-md px-1.5 py-1 text-left hover:bg-muted">
            {subagent.running ? <Fan className="size-3.5 animate-spin text-success" /> : <span className="size-1.5 rounded-full bg-muted-foreground/50" />}
            <span className="font-medium">{subagent.desc || subagent.type}</span>
            <span className="text-xs text-muted-foreground">{subagent.type}</span>
            <span className="min-w-0 truncate text-xs text-muted-foreground" title={subagent.activity}>
              {subagent.activity}
            </span>
            {subagent.context > 0 && <span className="text-xs text-muted-foreground">{t("conversation.live.subContext", { n: compact(subagent.context) })}</span>}
          </button>
          {subagent.running && (
            <Button size="icon-xs" variant="ghost" title={t("conversation.live.subStop")} aria-label={t("conversation.live.subStop")} onClick={() => void stopTask(session, subagent.id, subagent.desc || subagent.type, t("conversation.live.subStopSent"))}>
              <OctagonAlert />
            </Button>
          )}
        </li>
      ))}
    </ul>
  )
}

// Sous-agents de la session : un bouton (nombre et agents en cours) qui ouvre la liste, à côté des commandes de la session.
export function LiveAgents({ live: { st }, dir, id }: { live: Live; dir: string; id: string }) {
  const { t } = useTranslation()
  const [shown, setShown] = useState(false)
  const [sub, setSub] = useState<SubLive | null>(null)
  const subs = st?.subs ?? []
  const running = subs.filter((subagent) => subagent.running).length
  if (!st || subs.length === 0) return null
  const dialogTitle = running > 0
    ? t("conversation.live.dialogTitleRunning", { n: st.subs.length, running })
    : t("conversation.live.dialogTitle", { n: st.subs.length })
  const isSubRunning = !!st.subs.find((subagent) => subagent.id === sub?.id)?.running
  return (
    <>
      <Button size="xs" variant="ghost" className="max-md:[&>svg]:hidden" onClick={() => setShown(true)} title={t("conversation.live.subagentsTitle")}>
        <Bot /> {t("conversation.live.subagents", { n: st.subs.length })}
        {running > 0 && <span className="text-success">{t("conversation.live.runningCount", { running })}</span>}
      </Button>
      <Dialog open={shown} onOpenChange={setShown}>
        <DialogContent className="flex max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>{dialogTitle}</DialogTitle>
            <DialogDescription className="sr-only">{t("conversation.live.dialogDescription")}</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto px-6 py-5 text-sm">
            <SubList subs={st.subs} onPick={setSub} session={id} />
          </div>
        </DialogContent>
      </Dialog>
      <SubView dir={dir} id={id} sub={sub} running={isSubRunning} onClose={() => setSub(null)} />
    </>
  )
}

// Conversation d'un sous-agent, en fenêtre au-dessus de la liste ; relue toutes les 3 s tant qu'il travaille.
function SubView({ dir, id, sub, running, onClose }: { dir: string; id: string; sub: SubLive | null; running: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const [tools, setTools] = useState(false)
  const [loaded, setLoaded] = useState<{ key: string; msgs: Msg[] } | null>(null)
  const subId = sub?.id
  const key = `${subId}/${tools}`
  const viewportContainerRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!subId) return
    let isStillMounted = true
    const load = () =>
      api.conv({ dir, id, sub: subId }, tools, { limit: SUBAGENT_MESSAGE_LIMIT }).then(
        (conversation) => isStillMounted && setLoaded({ key, msgs: conversation.msgs }),
        () => {}
      )
    load()
    if (!running) return () => void (isStillMounted = false)
    const refreshInterval = setInterval(() => document.visibilityState === "visible" && load(), SUBAGENT_REFRESH_MS)
    return () => {
      isStillMounted = false
      clearInterval(refreshInterval)
    }
  }, [dir, id, subId, tools, running, key])
  const isLoadedForThisSub = loaded && subId && loaded.key.startsWith(`${subId}/`)
  const msgs = isLoadedForThisSub ? loaded.msgs : null
  // Collée à la fin tant qu'on y est, comme la conversation principale.
  useEffect(() => {
    const viewport = viewportContainerRef.current?.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]')
    if (!viewport) return
    const distanceFromBottom = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight
    if (distanceFromBottom < STICK_TO_BOTTOM_THRESHOLD_PX) viewport.scrollTop = viewport.scrollHeight
  }, [msgs])
  return (
    <Dialog open={!!sub} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="flex h-[85vh] max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
        <DialogHeader className="border-b px-6 py-4">
          <DialogTitle className="truncate pr-6">{sub?.desc || sub?.type}</DialogTitle>
          <DialogDescription className="sr-only">{t("conversation.live.subNote")}</DialogDescription>
          <div>
            <Button size="sm" variant={tools ? "secondary" : "ghost"} onClick={() => setTools(!tools)} aria-pressed={tools}>
              <Wrench /> {tools ? t("conversation.view.toolsShown") : t("conversation.view.toolsHidden")}
            </Button>
          </div>
        </DialogHeader>
        <div ref={viewportContainerRef} className="min-h-0 flex-1">
          <ScrollArea className="h-full">
            <div className="space-y-3 px-6 py-4 text-sm">
              {msgs?.map((message, messageIndex) => (
                <Message key={message.o === undefined ? messageIndex : `${message.o}.${message.n ?? 0}`} m={message} />
              ))}
              {msgs?.length === 0 && <p className="text-center text-muted-foreground">{t("conversation.live.subEmpty")}</p>}
            </div>
          </ScrollArea>
        </div>
        <p className="border-t px-6 py-2 text-xs text-muted-foreground">{t("conversation.live.subNote")}</p>
      </DialogContent>
    </Dialog>
  )
}
