import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { dismissGitPush, gitPushOutputTail, setGitPushTerminalOpen, useGitPushRun, type GitPushRun } from "@/hooks/useGitPush"
import { GitPushSendDialog } from "./GitPushSendDialog"
import { GitPushTerminal } from "./GitPushTerminal"

const TOAST_ID = "git-push"
const TOAST_TAIL_LINES = 3
const TOAST_REFRESH_MS = 1000

// Monté une fois dans l'application : affiche le push en cours sous forme de toast persistant (les dernières lignes de sa sortie,
// puis son résultat), le terminal complet à la demande, et l'envoi de l'échec à Claude.
export function GitPushHost() {
  const run = useGitPushRun()
  const [isSendOpen, setIsSendOpen] = useState(false)
  const requestSend = useCallback(() => setIsSendOpen(true), [])
  useGitPushToast(run, requestSend)
  return (
    <>
      <GitPushTerminal />
      {run && <GitPushSendDialog run={run} open={isSendOpen} onOpenChange={setIsSendOpen} />}
    </>
  )
}

function ToastBody({ tail, openLabel }: { tail?: string; openLabel: string }) {
  return (
    <div className="flex flex-col gap-1">
      {tail && <pre className="max-h-16 overflow-hidden text-xs whitespace-pre-wrap break-all">{tail}</pre>}
      <button type="button" onClick={() => setGitPushTerminalOpen(true)} className="w-fit cursor-pointer text-xs font-medium underline">
        {openLabel}
      </button>
    </div>
  )
}

function useGitPushToast(run: GitPushRun | null, requestSend: () => void) {
  const { t } = useTranslation()
  const status = run?.status
  const exitCode = run?.exitCode
  const errorMessage = run?.errorMessage
  const branch = run?.branch
  const remote = run?.remote
  useEffect(() => {
    if (!status || !branch || !remote) {
      toast.dismiss(TOAST_ID)
      return
    }
    const sendToClaude = {
      label: t("conversation.gitPush.sendToClaude"),
      onClick: (event: React.MouseEvent) => {
        event.preventDefault()
        requestSend()
      },
    }
    const shared = { id: TOAST_ID, duration: Infinity, onDismiss: dismissGitPush }
    const showToast = () => {
      const tail = status === "error" ? errorMessage : gitPushOutputTail(TOAST_TAIL_LINES)
      const description = <ToastBody tail={tail} openLabel={t("conversation.gitPush.openTerminal")} />
      if (status === "running") toast.loading(t("conversation.gitPush.toastRunning", { branch, remote }), { ...shared, description, dismissible: false })
      else if (status === "succeeded") toast.success(t("conversation.gitPush.toastDone", { branch, remote }), { ...shared, description })
      else toast.error(t("conversation.gitPush.toastFailed", { remote, code: exitCode ?? "?" }), { ...shared, description, action: sendToClaude })
    }
    showToast()
    if (status !== "running") return
    const refreshTimer = setInterval(showToast, TOAST_REFRESH_MS)
    return () => clearInterval(refreshTimer)
  }, [status, exitCode, errorMessage, branch, remote, requestSend, t])
}
