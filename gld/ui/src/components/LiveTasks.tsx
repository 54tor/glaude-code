import { useTranslation } from "react-i18next"
import { useEffect, useState } from "react"
import { CircleAlert, CircleCheck, CircleHelp, ListChecks, Fan, OctagonAlert } from "lucide-react"
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
    (error) => toast.error(errMsg(error))
  )

const OUTPUT_REFRESH_MS = 3000
const SECONDS_PER_MINUTE = 60
const SECONDS_PER_HOUR = 3600

function Icon({ s: status }: { s: BgTask["status"] }) {
  if (status === "running") return <Fan className="size-3.5 shrink-0 animate-spin text-success" />
  if (status === "completed") return <CircleCheck className="size-3.5 shrink-0 text-success" />
  if (status === "unknown") return <CircleHelp className="size-3.5 shrink-0 text-muted-foreground" />
  return <CircleAlert className="size-3.5 shrink-0 text-warning" />
}

const formatDuration = (milliseconds: number) => {
  const totalSeconds = Math.round(milliseconds / 1000)
  if (totalSeconds < SECONDS_PER_MINUTE) return `${totalSeconds} s`
  if (totalSeconds < SECONDS_PER_HOUR) return `${Math.floor(totalSeconds / SECONDS_PER_MINUTE)} min ${totalSeconds % SECONDS_PER_MINUTE} s`
  const hours = Math.floor(totalSeconds / SECONDS_PER_HOUR)
  const minutes = Math.floor((totalSeconds % SECONDS_PER_HOUR) / SECONDS_PER_MINUTE)
  return `${hours} h ${minutes} min`
}

function AgentIcon({ s: state }: { s: string }) {
  if (state === "running") return <Fan className="size-3 shrink-0 animate-spin text-success" />
  if (state === "done") return <CircleCheck className="size-3 shrink-0 text-success" />
  if (state === "queued") return <CircleHelp className="size-3 shrink-0 text-muted-foreground" />
  return <CircleAlert className="size-3 shrink-0 text-warning" />
}

export function WorkflowProgress({ w: workflow }: { w: Workflow }) {
  const { t } = useTranslation()
  const titles = workflow.phases.map((phase) => phase.title)
  for (const agent of workflow.agents) {
    if (!titles.includes(agent.phase)) titles.push(agent.phase)
  }
  return (
    <div className="space-y-2 rounded-md border bg-muted/30 p-3 text-xs">
      <div className="flex flex-wrap gap-x-3 font-medium tabular-nums">
        <span>{workflow.name || t("conversation.tasks.workflow.title")}</span>
        <span className="text-muted-foreground">{t("conversation.tasks.workflow.agents", { n: workflow.agents.length })}</span>
        <span className="text-muted-foreground">{t("conversation.tasks.workflow.tokens", { n: compact(workflow.totalTokens) })}</span>
        {workflow.durationMs > 0 && <span className="text-muted-foreground">{formatDuration(workflow.durationMs)}</span>}
      </div>
      {titles.map((title) => (
        <div key={title}>
          <div className="font-medium text-muted-foreground">{title || t("conversation.tasks.workflow.noPhase")}</div>
          <ul className="space-y-0.5">
            {workflow.agents.filter((agent) => agent.phase === title).map((agent, agentIndex) => (
              <li key={agentIndex} className="flex items-center gap-2" title={agent.model}>
                <AgentIcon s={agent.state} />
                <span className="sr-only">{t(`conversation.tasks.workflow.state.${agent.state}`, { defaultValue: agent.state })}</span>
                <span className="min-w-0 flex-1 truncate">{agent.label}</span>
                {agent.lastTool && <span className="shrink-0 text-muted-foreground">{agent.lastTool}</span>}
                {agent.tokens > 0 && <span className="shrink-0 text-muted-foreground tabular-nums">{compact(agent.tokens)}</span>}
                {agent.durationMs > 0 && <span className="shrink-0 text-muted-foreground tabular-nums">{formatDuration(agent.durationMs)}</span>}
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
  const [pickedTask, setPickedTask] = useState<BgTask | null>(null)
  const tasks = st?.tasks ?? []
  const runningCount = tasks.filter((task) => task.status === "running").length
  const currentTask = pickedTask && tasks.find((task) => task.id === pickedTask.id)
  const [output, setOutput] = useState<{ id: string; text: string; ok: boolean } | null>(null)
  const watch = currentTask?.status === "running"
  const [workflowState, setWorkflowState] = useState<{ id: string; w?: Workflow } | null>(null)
  useEffect(() => {
    if (!pickedTask) return
    let isStillMounted = true
    const findTaskWorkflow = (workflows: Workflow[]) => {
      const ownWorkflow = workflows.find((workflow) => workflow.taskId === pickedTask.id)
      if (ownWorkflow) return ownWorkflow
      if (pickedTask.status !== "running") return undefined
      return workflows.find((workflow) => !workflow.taskId && workflow.status === "running")
    }
    const load = () => {
      void api.taskOutput(dir, id, pickedTask.id).then(
        (outputResponse) => isStillMounted && setOutput({ id: pickedTask.id, text: outputResponse.text ?? "", ok: true }),
        (error) => isStillMounted && error instanceof ApiError && error.status === 404 && setOutput({ id: pickedTask.id, text: "", ok: false })
      )
      if (pickedTask.tool === "Workflow")
        void api.workflows(dir, id).then(
          (workflowsResponse) => isStillMounted && setWorkflowState({ id: pickedTask.id, w: findTaskWorkflow(workflowsResponse.workflows) }),
          () => {}
        )
    }
    load()
    if (!watch) return () => void (isStillMounted = false)
    const refreshInterval = setInterval(() => document.visibilityState === "visible" && load(), OUTPUT_REFRESH_MS)
    return () => {
      isStillMounted = false
      clearInterval(refreshInterval)
    }
  }, [pickedTask, watch, dir, id])
  const renderOutputText = () => {
    if (output?.id !== pickedTask?.id) return "…"
    if (!output?.ok) return t("conversation.tasks.noOutput")
    return output.text || t("conversation.tasks.empty")
  }
  if (tasks.length === 0) return null
  return (
    <>
      <Button size="xs" variant="ghost" onClick={() => setShown(true)} title={t("conversation.tasks.title")}>
        <ListChecks /> {t("conversation.tasks.count", { n: tasks.length })}
        {runningCount > 0 && <span className="text-success">{t("conversation.live.runningCount", { running: runningCount })}</span>}
      </Button>
      <Dialog open={shown} onOpenChange={setShown}>
        <DialogContent className="flex h-[85vh] max-h-[85vh] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-4">
            <DialogTitle>{t("conversation.tasks.dialogTitle", { n: tasks.length })}</DialogTitle>
            <DialogDescription className="sr-only">{t("conversation.tasks.description")}</DialogDescription>
          </DialogHeader>
          <ScrollArea className="min-h-0 flex-1">
          <ul className="space-y-1 px-4 py-4 text-sm">
            {[...tasks].reverse().map((task) => (
              <li key={task.id} className="flex items-center gap-1">
                <button type="button" onClick={() => setPickedTask(task)} className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-muted" title={t("conversation.tasks.open")}>
                  <Icon s={task.status} />
                  <span className="min-w-0 flex-1 truncate font-medium">{task.desc || task.id}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">{t(`conversation.tasks.status.${task.status}`, { defaultValue: task.status })}</span>
                  {task.start && <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{ago(Date.parse(task.start) / 1000)}</span>}
                </button>
                {task.status === "running" && (
                  <Button size="icon-xs" variant="ghost" title={t("conversation.tasks.stop")} aria-label={t("conversation.tasks.stop")} onClick={() => void stopTask(id, task.id, task.desc || task.id, t("conversation.tasks.stopSent"))}>
                    <OctagonAlert />
                  </Button>
                )}
              </li>
            ))}
          </ul>
          </ScrollArea>
        </DialogContent>
      </Dialog>
      <Dialog open={!!pickedTask} onOpenChange={(isOpen) => !isOpen && setPickedTask(null)}>
        <DialogContent className="flex h-[85vh] max-h-[85vh] min-w-0 flex-col gap-2 sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle className="truncate">{currentTask?.desc || pickedTask?.id}</DialogTitle>
            <DialogDescription>{currentTask?.summary || t(`conversation.tasks.status.${currentTask?.status ?? "running"}`, { defaultValue: currentTask?.status })}</DialogDescription>
          </DialogHeader>
          <ScrollArea className="min-h-0 flex-1 rounded-md border bg-muted/30">
            {workflowState?.id === pickedTask?.id && workflowState?.w && <div className="p-3 pb-0"><WorkflowProgress w={workflowState.w} /></div>}
            <pre className="p-3 font-mono text-xs break-all whitespace-pre-wrap">{renderOutputText()}</pre>
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </>
  )
}
