import { i18n, loc } from "@/i18n"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { BarChart3, Bot, Fan, Square, Wrench } from "lucide-react"
import { api, type Detail, type LiveState, type Msg, type Session, type SubLive, type Tokens } from "@/lib/api"
import { compact, full, tilde, tokTotal } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Markdown, Message } from "./Message"
import { Tagged, isServiceMsg } from "./Tagged"
import { stopTask } from "./LiveTasks"
import { CacheState, KeepWarmButton } from "./KeepWarm"
import { useActions } from "./actions"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

const secs = (n: number) => (n < 60 ? `${Math.max(0, Math.round(n))} s` : n < 3600 ? `${Math.floor(n / 60)} min ${Math.round(n % 60)} s` : `${Math.floor(n / 3600)} h ${Math.floor((n % 3600) / 60)} min`)

// État de la session affichée (relu toutes les 2 s tant qu'elle est ouverte) : useLive le lit, LiveBar montre ce que Claude fait,
// LiveStats (dans la barre d'outils) le contexte, les tokens et le bouton « Stats ». Ancien résumé : : ce que Claude fait en ce moment (réflexion, outil, attente) et un bouton « Stats » qui ouvre
// la consommation de tokens et les sous-agents dans une fenêtre. Relu toutes les 2 s tant que la session est ouverte (fichiers
// locaux seulement), une seule fois sinon.
export type Live = { st: LiveState | null; open: boolean }

export function useLive(dir: string, id: string, open: boolean): Live {
  const [st, setSt] = useState<LiveState | null>(null)
  useEffect(() => {
    if (!id) return
    let live = true
    // Un relevé identique garde l'ancien objet : pas de nouveau rendu de la conversation toutes les 2 s.
    const load = () => api.sessionLive(dir, id).then((r) => live && setSt((p) => (JSON.stringify(p) === JSON.stringify(r) ? p : r)), () => {})
    load()
    if (!open) return () => void (live = false)
    const t = setInterval(() => document.visibilityState === "visible" && load(), 2000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [dir, id, open])
  return { st, open }
}

// Horloge d'une seconde, tenue dans le composant qui l'affiche : la conversation ne se redessine pas à chaque tic.
function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now() / 1000)
  useEffect(() => {
    if (!active) return
    const t = setInterval(() => setNow(Date.now() / 1000), 1000)
    return () => clearInterval(t)
  }, [active])
  return now
}

// Les états génériques (réflexion, travail) changent de verbe toutes les 6 s, l'un après l'autre, décalés d'une session à l'autre ;
// un outil en cours ou une attente se lisent tels quels.
function rotate(list: string[], id: string, now: number) {
  const seed = [...id].reduce((n, c) => n + c.charCodeAt(0), 0)
  return list[(Math.floor(now / 6) + seed) % list.length] + "…"
}

function label(st: LiveState, id: string, now: number) {
  if (!st.thinking || !st.generic || !st.verbs?.length) return st.activity || (st.thinking ? i18n.t("conversation.live.working") : i18n.t("conversation.live.open"))
  return rotate(st.verbs, id, now)
}

// Un outil en cours : une des formules de « conversation.live.execTools », à défaut « exécute un outil… ».
function execLabel(id: string, now: number) {
  const list = i18n.t("conversation.live.execTools", { returnObjects: true })
  return Array.isArray(list) && list.length ? rotate(list as string[], id, now) : i18n.t("conversation.live.execTool")
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
  const detail = isTool ? st.tool : st.thinking ? "" : st.said
  return (
    <div className="space-y-2">
      {st.queue?.map((m) => (
        <div key={m.ts + m.t} className="mb-3 flex justify-end" title={new Date(m.ts).toLocaleString(loc())}>
          <div className={cn("msg relative max-w-[85%] break-words text-muted-foreground", isServiceMsg(m.t) ? "w-full" : "rounded-lg bg-muted/30 px-4 py-2.5 whitespace-pre-wrap outline-1 -outline-offset-1 outline-dashed outline-border")}>
            {isServiceMsg(m.t) ? <Tagged t={m.t} md={(x) => <Markdown t={x} />} /> : m.t}
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
            {st.since > 0 && st.thinking && <span className="shrink-0 font-normal text-muted-foreground">{t("conversation.live.since", { time: secs(now - st.since) })}</span>}
          </span>
        )}
        {/* Un seul emplacement d'une ligne : l'outil en cours, ou le dernier message de Claude quand il attend. Sa hauteur est toujours
            réservée (même sans rien à montrer) pour que la page ne saute pas ; un clic le déplie. */}
        {detail && <button
          type="button"
          onClick={() => setExpanded((e) => !e)}
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
          .filter((s) => s.running)
          .map((s) => (
            <div key={s.id} className="flex min-w-0 basis-full items-center gap-2 pl-3 text-xs text-muted-foreground" title={s.activity}>
              <Fan className="size-3.5 shrink-0 animate-spin text-success" />
              <span className="shrink-0 font-medium text-foreground">{s.desc || s.type}</span>
              <span className="min-w-0 truncate">{s.activity}</span>
            </div>
          ))}
        {st.tasks
          ?.filter((x) => x.status === "running")
          .map((x) => (
            <div key={x.id} className="flex min-w-0 basis-full items-center gap-2 pl-3 text-xs text-muted-foreground" title={x.desc}>
              <Fan className="size-3.5 shrink-0 animate-spin text-success" />
              <span className="min-w-0 truncate font-medium text-foreground">{x.desc || x.id}</span>
            </div>
          ))}
      </div>
    </div>
  )
}

// Bouton de l'en-tête : contexte et tokens, ouvre la fenêtre « Stats ».
export function StatsButton({ live: { st }, s, onClick }: { live: Live; s: Session; onClick: () => void }) {
  const { t } = useTranslation()
  return (
    <Button size="xs" variant="ghost" className="tabular-nums max-md:hidden" onClick={onClick} title={t("conversation.live.statsTitle")}>
      <BarChart3 />
      {t("conversation.live.stats")} :{st && st.context > 0 && <> ctx {compact(st.context)}</>} {compact(tokTotal(s.tok as Tokens))} tok
    </Button>
  )
}

// Fenêtre « Stats » : faits de la session, cache et maintien au chaud, consommation, dernier appel, sous-agents.
// shown/onShown : ouverte depuis l'en-tête du lecteur ou le menu « … » (petit écran).
export function LiveStats({ live: { st }, s, project, detail, shown, onShown }: { live: Live; s: Session; project: string; detail: Detail | null; shown: boolean; onShown: (v: boolean) => void }) {
  const { t } = useTranslation()
  const a = useActions()
  const [sub, setSub] = useState<SubLive | null>(null)
  const tok = s.tok as Tokens
  const d = detail
  const facts = [
    [t("shell.info.id"), <code key="i" className="text-xs break-all">{s.id}</code>],
    [t("shell.info.cwd"), tilde(s.cwd, a.home)],
    [t("shell.info.project"), tilde(project, a.home)],
    [t("shell.info.modified"), full(s.mtime)],
    [t("shell.info.state"), a.live.has(s.id) ? t("shell.info.open") : t("shell.info.closed")],
    d ? [t("shell.info.prompts"), String(d.n)] : null,
    d?.subagents ? [t("shell.info.subagents"), String(d.subagents)] : null,
    d?.branch ? [t("shell.info.branch"), d.branch] : null,
    d?.models.length ? [t("shell.info.models"), d.models.filter((m) => !m.startsWith("<")).join(", ")] : null,
    d?.t0 ? [t("shell.info.started"), full(d.t0)] : null,
    d?.t1 ? [t("shell.info.last"), full(d.t1)] : null,
  ].filter(Boolean) as [string, React.ReactNode][]
  return (
    <>
      <Dialog open={shown} onOpenChange={onShown}>
        <DialogContent className="flex max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle className="truncate pr-8">{s.title}</DialogTitle>
            <DialogDescription className="sr-only">{t("conversation.live.statsDescription")}</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 space-y-5 overflow-x-hidden overflow-y-auto px-6 py-5 text-sm">
            <section>
              <h3 className="mb-1 font-medium">{t("conversation.live.sessionHeading")}</h3>
              <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5">
                {facts.map(([k, v]) => (
                  <div key={k} className="contents">
                    <dt className="text-muted-foreground">{k}</dt>
                    <dd className="min-w-0 break-words">{v}</dd>
                  </div>
                ))}
              </dl>
            </section>
            <section className="space-y-2">
              <h3 className="font-medium">{t("conversation.live.cacheHeading")}</h3>
              <div className="flex flex-wrap items-center gap-3">
                <CacheState s={s} />
                <KeepWarmButton s={s} />
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
                <SubList subs={st.subs} onPick={setSub} session={s.id} />
              </section>
            )}
          </div>
        </DialogContent>
      </Dialog>
      <SubView dir={s.dir} id={s.id} sub={sub} running={!!st?.subs.find((x) => x.id === sub?.id)?.running} onClose={() => setSub(null)} />
    </>
  )
}

// Liste des sous-agents ; un clic en ouvre la conversation.
function SubList({ subs, onPick, session }: { subs: SubLive[]; onPick: (s: SubLive) => void; session: string }) {
  const { t } = useTranslation()
  return (
    <ul className="space-y-1.5">
      {subs.map((s) => (
        <li key={s.id} className="flex items-center gap-1">
          <button type="button" onClick={() => onPick(s)} title={t("conversation.live.subOpen")} className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 rounded-md px-1.5 py-1 text-left hover:bg-muted">
            {s.running ? <Fan className="size-3.5 animate-spin text-success" /> : <span className="size-1.5 rounded-full bg-muted-foreground/50" />}
            <span className="font-medium">{s.desc || s.type}</span>
            <span className="text-xs text-muted-foreground">{s.type}</span>
            <span className="min-w-0 truncate text-xs text-muted-foreground" title={s.activity}>
              {s.activity}
            </span>
            {s.context > 0 && <span className="text-xs text-muted-foreground">{t("conversation.live.subContext", { n: compact(s.context) })}</span>}
          </button>
          {s.running && (
            <Button size="icon-xs" variant="ghost" title={t("conversation.live.subStop")} aria-label={t("conversation.live.subStop")} onClick={() => void stopTask(session, s.id, s.desc || s.type, t("conversation.live.subStopSent"))}>
              <Square />
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
  const running = subs.filter((s) => s.running).length
  if (!st || subs.length === 0) return null
  return (
    <>
      <Button size="xs" variant="ghost" className="max-md:[&>svg]:hidden" onClick={() => setShown(true)} title={t("conversation.live.subagentsTitle")}>
        <Bot /> {t("conversation.live.subagents", { n: st.subs.length })}
        {running > 0 && <span className="text-success">{t("conversation.live.runningCount", { running })}</span>}
      </Button>
      <Dialog open={shown} onOpenChange={setShown}>
        <DialogContent className="flex max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>{running > 0 ? t("conversation.live.dialogTitleRunning", { n: st.subs.length, running }) : t("conversation.live.dialogTitle", { n: st.subs.length })}</DialogTitle>
            <DialogDescription className="sr-only">{t("conversation.live.dialogDescription")}</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto px-6 py-5 text-sm">
            <SubList subs={st.subs} onPick={setSub} session={id} />
          </div>
        </DialogContent>
      </Dialog>
      <SubView dir={dir} id={id} sub={sub} running={!!st.subs.find((x) => x.id === sub?.id)?.running} onClose={() => setSub(null)} />
    </>
  )
}

// Conversation d'un sous-agent, en fenêtre au-dessus de la liste ; relue toutes les 3 s tant qu'il travaille.
function SubView({ dir, id, sub, running, onClose }: { dir: string; id: string; sub: SubLive | null; running: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const [tools, setTools] = useState(false)
  const [got, setGot] = useState<{ key: string; msgs: Msg[] } | null>(null)
  const sid = sub?.id
  const key = `${sid}/${tools}`
  const vp = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!sid) return
    let live = true
    const load = () => api.conv({ dir, id, sub: sid }, tools, { limit: 300 }).then((r) => live && setGot({ key, msgs: r.msgs }), () => {})
    load()
    if (!running) return () => void (live = false)
    const iv = setInterval(() => document.visibilityState === "visible" && load(), 3000)
    return () => {
      live = false
      clearInterval(iv)
    }
  }, [dir, id, sid, tools, running, key])
  const msgs = got && sid && got.key.startsWith(`${sid}/`) ? got.msgs : null
  // Collée à la fin tant qu'on y est, comme la conversation principale.
  useEffect(() => {
    const el = vp.current?.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]')
    if (el && el.scrollHeight - el.scrollTop - el.clientHeight < 120) el.scrollTop = el.scrollHeight
  }, [msgs])
  return (
    <Dialog open={!!sub} onOpenChange={(o) => !o && onClose()}>
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
        <div ref={vp} className="min-h-0 flex-1">
          <ScrollArea className="h-full">
            <div className="space-y-3 px-6 py-4 text-sm">
              {msgs?.map((m, i) => (
                <Message key={m.o === undefined ? i : `${m.o}.${m.n ?? 0}`} m={m} />
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
