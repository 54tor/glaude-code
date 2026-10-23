import { i18n, loc } from "@/i18n"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { BarChart3, Bot, Loader2, Wrench } from "lucide-react"
import { api, type LiveState, type Msg, type SubLive, type Tokens } from "@/lib/api"
import { compact, tokTotal } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Message } from "./Message"
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
function label(st: LiveState, id: string, now: number) {
  if (!st.thinking || !st.generic || !st.verbs?.length) return st.activity || (st.thinking ? i18n.t("conversation.live.working") : i18n.t("conversation.live.open"))
  const seed = [...id].reduce((n, c) => n + c.charCodeAt(0), 0)
  return st.verbs[(Math.floor(now / 6) + seed) % st.verbs.length] + "…"
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
          <div className="msg relative max-w-[85%] rounded-lg bg-muted/30 px-4 py-2.5 break-words whitespace-pre-wrap text-muted-foreground outline-1 -outline-offset-1 outline-dashed outline-border">
            {m.t}
            <span className="absolute top-full right-2 mt-0.5 text-[10px] font-medium tracking-wide whitespace-nowrap uppercase">{t("conversation.live.queued")}</span>
          </div>
        </div>
      ))}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
        {isOpen && (
          <span className={cn("flex min-w-0 items-center gap-1.5 font-medium", st.thinking ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400")}>
            {st.thinking ? <Loader2 className="size-3.5 shrink-0 animate-spin" /> : <span className="size-1.5 shrink-0 rounded-full bg-current" />}
            <span className="truncate" title={st.activity}>
              {st.tool && st.exec ? t("conversation.live.execTool") : label(st, id, now)}
            </span>
            {st.since > 0 && st.thinking && <span className="shrink-0 font-normal text-muted-foreground">{t("conversation.live.since", { time: secs(now - st.since) })}</span>}
          </span>
        )}
        {/* Un seul emplacement d'une ligne : l'outil en cours, ou le dernier message de Claude quand il attend. Sa hauteur est toujours
            réservée (même sans rien à montrer) pour que la page ne saute pas ; un clic le déplie. */}
        <button
          type="button"
          disabled={!detail}
          onClick={() => setExpanded((e) => !e)}
          aria-expanded={detail ? expanded : undefined}
          title={detail}
          className={cn(
            "msg flex min-h-5 min-w-0 basis-full items-start gap-2 border-l-2 py-0.5 pl-3 text-left text-xs leading-4 text-muted-foreground",
            detail ? (isTool ? "" : "border-amber-500/60") : "border-transparent",
            detail && "cursor-pointer hover:text-foreground"
          )}
        >
          {isTool && <Wrench className="mt-px size-3.5 shrink-0" aria-label={st.toolRunning ? t("conversation.live.toolRunning") : t("conversation.live.lastTool")} />}
          <span className={cn("min-w-0 flex-1", expanded ? "max-h-60 overflow-y-auto break-words whitespace-pre-wrap" : "truncate")}>{detail || " "}</span>
        </button>
      </div>
    </div>
  )
}

// Contexte, tokens et sous-agents sur une ligne, le détail dans une fenêtre.
export function LiveStats({ live: { st, open }, tok }: { live: Live; tok: Tokens }) {
  const { t } = useTranslation()
  const [stats, setStats] = useState(false)
  if (!st) return null
  return (
    <>
      <span className="ml-auto flex items-center gap-x-3 px-1 text-xs text-muted-foreground tabular-nums">
        {!(open && st.open) && <span className="font-medium">{t("conversation.live.closed")}</span>}
        {st.context > 0 && <span title={t("conversation.live.contextTitle")}>ctx {compact(st.context)}</span>}
        <span title={t("conversation.live.tokensTitle")}>{compact(tokTotal(tok))} tok</span>
      </span>
      <Button size="sm" variant="ghost" onClick={() => setStats(true)} title={t("conversation.live.statsTitle")}>
        <BarChart3 /> {t("conversation.live.stats")}
      </Button>
      <Dialog open={stats} onOpenChange={setStats}>
        <DialogContent className="flex max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>{t("conversation.live.statsHeading")}</DialogTitle>
            <DialogDescription className="sr-only">{t("conversation.live.statsDescription")}</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 space-y-5 overflow-x-hidden overflow-y-auto px-6 py-5 text-sm">
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
            {(st.context > 0 || st.model) && (
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
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

// Sous-agents de la session : un bouton (nombre et agents en cours) qui ouvre la liste, à côté des commandes de la session.
export function LiveAgents({ live: { st }, dir, id }: { live: Live; dir: string; id: string }) {
  const { t } = useTranslation()
  const [shown, setShown] = useState(false)
  const [sub, setSub] = useState<SubLive | null>(null)
  if (!st || st.subs.length === 0) return null
  const running = st.subs.filter((s) => s.running).length
  return (
    <>
      <Button size="sm" variant="ghost" onClick={() => setShown(true)} title={t("conversation.live.subagentsTitle")}>
        <Bot /> {t("conversation.live.subagents", { n: st.subs.length })}
        {running > 0 && <span className="text-emerald-600 dark:text-emerald-400">{t("conversation.live.runningCount", { running })}</span>}
      </Button>
      <Dialog open={shown} onOpenChange={setShown}>
        <DialogContent className="flex max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>{running > 0 ? t("conversation.live.dialogTitleRunning", { n: st.subs.length, running }) : t("conversation.live.dialogTitle", { n: st.subs.length })}</DialogTitle>
            <DialogDescription className="sr-only">{t("conversation.live.dialogDescription")}</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto px-6 py-5 text-sm">
            <ul className="space-y-1.5">
              {st.subs.map((s) => (
                <li key={s.id}>
                  <button type="button" onClick={() => setSub(s)} title={t("conversation.live.subOpen")} className="flex w-full flex-wrap items-center gap-x-2 rounded-md px-1.5 py-1 text-left hover:bg-muted">
                    {s.running ? <Loader2 className="size-3.5 animate-spin text-emerald-600 dark:text-emerald-400" /> : <span className="size-1.5 rounded-full bg-muted-foreground/50" />}
                    <span className="font-medium">{s.desc || s.type}</span>
                    <span className="text-xs text-muted-foreground">{s.type}</span>
                    <span className="min-w-0 truncate text-xs text-muted-foreground" title={s.activity}>
                      {s.activity}
                    </span>
                    {s.context > 0 && <span className="text-xs text-muted-foreground">{t("conversation.live.subContext", { n: compact(s.context) })}</span>}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
