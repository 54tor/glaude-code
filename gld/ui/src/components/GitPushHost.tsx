import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { dismissGitPush, gitPushOutputTail, setGitPushTerminalOpen, useGitPushRuns, type GitPushRun } from "@/hooks/useGitPush"
import { GitPushPill } from "./GitPushPill"
import { GitPushSendDialog } from "./GitPushSendDialog"
import { GitPushTerminal } from "./GitPushTerminal"

const toastIdOf = (run: GitPushRun) => `git-push-${run.id}`
const TOAST_TAIL_LINES = 3
const TOAST_SUCCESS_MS = 6000

// Monté une fois dans l'application : affiche chaque push en cours sous forme de pastille (les toasts gênaient l'interface), puis son résultat en toast, son terminal complet à la demande, et l'envoi de l'échec à Claude.
export function GitPushHost() {
  const runs = useGitPushRuns()
  const [sendRunId, setSendRunId] = useState<number | null>(null)
  const requestSend = useCallback((runId: number) => setSendRunId(runId), [])
  const closeSend = useCallback(() => setSendRunId(null), [])
  const runningRuns = runs.filter((run) => run.status === "running")
  const runToSend = runs.find((run) => run.id === sendRunId)
  return (
    <>
      {runs.map((run) => (
        <GitPushRunView key={run.id} run={run} requestSend={requestSend} />
      ))}
      {runningRuns.map((run, position) => !run.isTerminalOpen && <GitPushPill key={run.id} run={run} position={position} />)}
      {runToSend && <GitPushSendDialog run={runToSend} open onOpenChange={(isOpen) => !isOpen && closeSend()} />}
    </>
  )
}

function GitPushRunView({ run, requestSend }: { run: GitPushRun; requestSend: (runId: number) => void }) {
  useGitPushToast(run, requestSend)
  return <GitPushTerminal run={run} />
}

function ToastBody({ runId, tail, openLabel }: { runId: number; tail?: string; openLabel: string }) {
  return (
    <div className="flex flex-col gap-1">
      {tail && <pre className="max-h-16 overflow-hidden text-xs whitespace-pre-wrap break-all">{tail}</pre>}
      <button type="button" onClick={() => setGitPushTerminalOpen(runId, true)} className="w-fit cursor-pointer text-xs font-medium underline">
        {openLabel}
      </button>
    </div>
  )
}

function useGitPushToast(run: GitPushRun, requestSend: (runId: number) => void) {
  const { t } = useTranslation()
  const { id: runId, status, exitCode, errorMessage, branch, remote } = run
  const toastId = toastIdOf(run)
  useEffect(() => () => void toast.dismiss(toastId), [toastId])
  useEffect(() => {
    if (status === "running") {
      toast.dismiss(toastId)
      return
    }
    const sendToClaude = {
      label: t("conversation.gitPush.sendToClaude"),
      onClick: (event: React.MouseEvent) => {
        event.preventDefault()
        requestSend(runId)
      },
    }
    const dismissRun = () => dismissGitPush(runId)
    const shared = { id: toastId, duration: Infinity, onDismiss: dismissRun }
    const tail = status === "error" ? errorMessage : gitPushOutputTail(runId, TOAST_TAIL_LINES)
    const description = <ToastBody runId={runId} tail={tail} openLabel={t("conversation.gitPush.openTerminal")} />
    if (status === "succeeded") toast.success(t("conversation.gitPush.toastDone", { branch, remote }), { ...shared, description, duration: TOAST_SUCCESS_MS, onAutoClose: dismissRun })
    else toast.error(t("conversation.gitPush.toastFailed", { remote, code: exitCode ?? "?" }), { ...shared, description, action: sendToClaude, closeButton: true })
  }, [runId, toastId, status, exitCode, errorMessage, branch, remote, requestSend, t])
}
