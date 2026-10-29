import { useTranslation } from "react-i18next"
import { useEffect, useState } from "react"
import { CircleAlert, CircleCheck, CircleHelp, ListChecks, Fan, Square } from "lucide-react"
import { toast } from "sonner"
import { api, type BgTask, type Workflow } from "@/lib/api"
import { ApiError, errMsg } from "@/lib/apiError"
import { ago, compact } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import type { Live } from "./LiveStatus"

// Arrêt demandé à la session elle-même : Claude Code n'expose l'arrêt (TaskStop) qu'à son modèle.
export const stopTask = (id: string, task: string, desc: string, ok: string) =>
  api.sessionSend(id, `Arrête avec l'outil TaskStop la tâche ${task} (${desc}), sans autre commentaire.`).then(
    () => toast.success(ok),
    (e) => toast.error(errMsg(e))
  )

function Icon({ s }: { s: BgTask["status"] }) {
  if (s === "running") return <Fan className="size-3.5 shrink-0 animate-spin text-success" />
  if (s === "completed") return <CircleCheck className="size-3.5 shrink-0 text-success" />
  if (s === "unknown") return <CircleHelp className="size-3.5 shrink-0 text-muted-foreground" />
  return <CircleAlert className="size-3.5 shrink-0 text-warning" />
}

const dur = (ms: number) => {
  const s = Math.round(ms / 1000)
  return s < 60 ? `${s} s` : s < 3600 ? `${Math.floor(s / 60)} min ${s % 60} s` : `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} min`
}

function AgentIcon({ s }: { s: string }) {
  if (s === "running") return <Fan className="size-3 shrink-0 animate-spin text-success" />
  if (s === "done") return <CircleCheck className="size-3 shrink-0 text-success" />
  if (s === "queued") return <CircleHelp className="size-3 shrink-0 text-muted-foreground" />
  return <CircleAlert className="size-3 shrink-0 text-warning" />
}

export function WorkflowProgress({ w }: { w: Workflow }) {
  const { t } = useTranslation()
  const titles = w.phases.map((p) => p.title)
  for (const a of w.agents) if (!titles.includes(a.phase)) titles.push(a.phase)
  return (
    <div className="space-y-2 rounded-md border bg-muted/30 p-3 text-xs">
      <div className="flex flex-wrap gap-x-3 font-medium tabular-nums">
        <span>{w.name || t("conversation.tasks.workflow.title")}</span>
        <span className="text-muted-foreground">{t("conversation.tasks.workflow.agents", { n: w.agents.length })}</span>
        <span className="text-muted-foreground">{t("conversation.tasks.workflow.tokens", { n: compact(w.totalTokens) })}</span>
        {w.durationMs > 0 && <span className="text-muted-foreground">{dur(w.durationMs)}</span>}
      </div>
      {titles.map((title) => (
        <div key={title}>
          <div className="font-medium text-muted-foreground">{title || t("conversation.tasks.workflow.noPhase")}</div>
          <ul className="space-y-0.5">
            {w.agents.filter((a) => a.phase === title).map((a, i) => (
              <li key={i} className="flex items-center gap-2" title={a.model}>
                <AgentIcon s={a.state} />
                <span className="sr-only">{t(`conversation.tasks.workflow.state.${a.state}`, { defaultValue: a.state })}</span>
                <span className="min-w-0 flex-1 truncate">{a.label}</span>
                {a.lastTool && <span className="shrink-0 text-muted-foreground">{a.lastTool}</span>}
                {a.tokens > 0 && <span className="shrink-0 text-muted-foreground tabular-nums">{compact(a.tokens)}</span>}
                {a.durationMs > 0 && <span className="shrink-0 text-muted-foreground tabular-nums">{dur(a.durationMs)}</span>}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  )
}

// Tâches de fond de la session (commandes Bash en arrière-plan, Monitor, workflows) relues dans son transcript : un bouton avec le
// nombre de tâches en cours, la liste au clic, et la sortie de chacune, relue toutes les 3 s tant qu'elle tourne.
export function LiveTasks({ live: { st }, dir, id }: { live: Live; dir: string; id: string }) {
  const { t } = useTranslation()
  const [shown, setShown] = useState(false)
  const [pick, setPick] = useState<BgTask | null>(null)
  const tasks = st?.tasks ?? []
  const running = tasks.filter((x) => x.status === "running").length
  const cur = pick && tasks.find((x) => x.id === pick.id)
  const [out, setOut] = useState<{ id: string; text: string; ok: boolean } | null>(null)
  const watch = cur?.status === "running"
  const [wf, setWf] = useState<{ id: string; w?: Workflow } | null>(null)
  useEffect(() => {
    if (!pick) return
    let live = true
    const load = () => {
      void api.taskOutput(dir, id, pick.id).then(
        (r) => live && setOut({ id: pick.id, text: r.text ?? "", ok: true }),
        (e) => live && e instanceof ApiError && e.status === 404 && setOut({ id: pick.id, text: "", ok: false })
      )
      if (pick.tool === "Workflow")
        void api.workflows(dir, id).then(
          (r) => live && setWf({ id: pick.id, w: r.workflows.find((x) => x.taskId === pick.id) ?? (pick.status === "running" ? r.workflows.find((x) => !x.taskId && x.status === "running") : undefined) }),
          () => {}
        )
    }
    load()
    if (!watch) return () => void (live = false)
    const iv = setInterval(() => document.visibilityState === "visible" && load(), 3000)
    return () => {
      live = false
      clearInterval(iv)
    }
  }, [pick, watch, dir, id])
  if (tasks.length === 0) return null
  return (
    <>
      <Button size="xs" variant="ghost" onClick={() => setShown(true)} title={t("conversation.tasks.title")}>
        <ListChecks /> {t("conversation.tasks.count", { n: tasks.length })}
        {running > 0 && <span className="text-success">{t("conversation.live.runningCount", { running })}</span>}
      </Button>
      <Dialog open={shown} onOpenChange={setShown}>
        <DialogContent className="flex h-[85vh] max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>{t("conversation.tasks.dialogTitle", { n: tasks.length })}</DialogTitle>
            <DialogDescription className="sr-only">{t("conversation.tasks.description")}</DialogDescription>
          </DialogHeader>
          <ScrollArea className="min-h-0 flex-1">
          <ul className="space-y-1 px-4 py-4 text-sm">
            {[...tasks].reverse().map((x) => (
              <li key={x.id} className="flex items-center gap-1">
                <button type="button" onClick={() => setPick(x)} className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-muted" title={t("conversation.tasks.open")}>
                  <Icon s={x.status} />
                  <span className="min-w-0 flex-1 truncate font-medium">{x.desc || x.id}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">{t(`conversation.tasks.status.${x.status}`, { defaultValue: x.status })}</span>
                  {x.start && <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{ago(Date.parse(x.start) / 1000)}</span>}
                </button>
                {x.status === "running" && (
                  <Button size="icon-xs" variant="ghost" title={t("conversation.tasks.stop")} aria-label={t("conversation.tasks.stop")} onClick={() => void stopTask(id, x.id, x.desc || x.id, t("conversation.tasks.stopSent"))}>
                    <Square />
                  </Button>
                )}
              </li>
            ))}
          </ul>
          </ScrollArea>
        </DialogContent>
      </Dialog>
      <Dialog open={!!pick} onOpenChange={(o) => !o && setPick(null)}>
        <DialogContent className="flex h-[85vh] max-h-[85vh] min-w-0 flex-col gap-2 sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle className="truncate">{cur?.desc || pick?.id}</DialogTitle>
            <DialogDescription>{cur?.summary || t(`conversation.tasks.status.${cur?.status ?? "running"}`, { defaultValue: cur?.status })}</DialogDescription>
          </DialogHeader>
          <ScrollArea className="min-h-0 flex-1 rounded-md border bg-muted/30">
            {wf?.id === pick?.id && wf?.w && <div className="p-3 pb-0"><WorkflowProgress w={wf.w} /></div>}
            <pre className="p-3 font-mono text-xs break-all whitespace-pre-wrap">{out?.id === pick?.id ? (out?.ok ? out.text || t("conversation.tasks.empty") : t("conversation.tasks.noOutput")) : "…"}</pre>
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </>
  )
}
