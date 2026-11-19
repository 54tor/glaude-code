import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { authFetch } from "@/lib/auth"
import { errMsg, qs, readResponse, v1 } from "@/lib/apiError"
import { hideHome } from "@/components/Message"
import { buildGitPushReport } from "@/lib/gitPushReport"
import {
  dismissGitPush,
  gitPushOutputTail,
  type GitPushRun,
} from "@/hooks/useGitPush"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { useActions, type Row } from "./actions"

const SEND_TIMEOUT_MS = 20_000
const FULL_OUTPUT = Infinity
const SHORT_ID_LENGTH = 8

const sessionLabel = (row: Row) =>
  row.s.title || row.s.id.slice(0, SHORT_ID_LENGTH)

// Envoie l'échec du push à Claude : une session du même dossier (en cours, en attente ou fermée), ou une nouvelle session.
export function GitPushSendDialog({
  run,
  open,
  onOpenChange,
}: {
  run: GitPushRun
  open: boolean
  onOpenChange: (isOpen: boolean) => void
}) {
  const { t } = useTranslation()
  const actions = useActions()
  const sameFolderRows = actions.rows.filter(
    (row) => row.g.project === run.dir || row.s.cwd === run.dir
  )
  // La sortie complète part en fichier .txt (le message a une taille limitée) ; la mention « @chemin » la désigne à Claude.
  const report = async (sessionId?: string) => {
    const output = gitPushOutputTail(run.id, FULL_OUTPUT)
    if (!output) return buildGitPushReport(run)
    const response = await authFetch(
      v1(`/uploads/files${qs({ id: sessionId })}`),
      {
        method: "POST",
        headers: {
          "X-File-Name": encodeURIComponent(`push-${run.branch}.txt`),
        },
        body: output,
      }
    )
    const uploaded = await readResponse<{ path?: string }>(response)
    return buildGitPushReport(
      run,
      uploaded.path ? `@${hideHome(uploaded.path)}` : undefined
    )
  }
  const finishSending = () => {
    onOpenChange(false)
    dismissGitPush(run.id)
  }
  const sendToSession = async (row: Row) => {
    const isLive = actions.live.has(row.s.id)
    let isDelivered = false
    try {
      const message = await report(row.s.id)
      if (isLive && !actions.waiting.has(row.s.id))
        await api.queueAdd(row.s.id, message)
      else if (isLive) await api.sessionSend(row.s.id, message, SEND_TIMEOUT_MS)
      isDelivered = isLive || (await actions.resumeSend(row, message))
    } catch (sendError) {
      toast.error(errMsg(sendError))
    }
    if (!isDelivered) return
    toast.success(t("conversation.gitPush.sent", { title: sessionLabel(row) }))
    finishSending()
  }
  const sendToNewSession = async () => {
    try {
      actions.newSession(run.dir, await report())
    } catch (sendError) {
      toast.error(errMsg(sendError))
      return
    }
    finishSending()
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="min-w-0 sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("conversation.gitPush.sendTitle")}</DialogTitle>
          <DialogDescription>
            {t("conversation.gitPush.sendHint")}
          </DialogDescription>
        </DialogHeader>
        <div className="flex max-h-72 flex-col gap-1 overflow-y-auto">
          {sameFolderRows.length === 0 && (
            <p className="text-sm text-muted-foreground">
              {t("conversation.gitPush.noSession")}
            </p>
          )}
          {sameFolderRows.map((row) => (
            <Button
              key={row.s.id}
              variant="outline"
              className="h-auto justify-between gap-3 py-2 text-left whitespace-normal"
              onClick={() => sendToSession(row)}
            >
              <span className="min-w-0 break-words">{sessionLabel(row)}</span>
              {actions.live.has(row.s.id) && (
                <span className="shrink-0 text-xs text-muted-foreground">
                  {t("conversation.gitPush.sessionOpen")}
                </span>
              )}
            </Button>
          ))}
        </div>
        <Button onClick={sendToNewSession}>
          {t("conversation.gitPush.newSession")}
        </Button>
      </DialogContent>
    </Dialog>
  )
}
