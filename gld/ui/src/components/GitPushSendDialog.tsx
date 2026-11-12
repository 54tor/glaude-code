import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { buildGitPushReport } from "@/lib/gitPushReport"
import { dismissGitPush, gitPushOutputTail, type GitPushRun } from "@/hooks/useGitPush"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useActions, type Row } from "./actions"

const SEND_TIMEOUT_MS = 20_000
const REPORT_OUTPUT_LINES = 120
const SHORT_ID_LENGTH = 8

const sessionLabel = (row: Row) => row.s.title || row.s.id.slice(0, SHORT_ID_LENGTH)

// Envoie l'échec du push à Claude : une session du même dossier (en cours, en attente ou fermée), ou une nouvelle session.
export function GitPushSendDialog({ run, open, onOpenChange }: { run: GitPushRun; open: boolean; onOpenChange: (isOpen: boolean) => void }) {
  const { t } = useTranslation()
  const actions = useActions()
  const sameFolderRows = actions.rows.filter((row) => row.g.project === run.dir || row.s.cwd === run.dir)
  const report = () => buildGitPushReport(run, gitPushOutputTail(REPORT_OUTPUT_LINES))
  const finishSending = () => {
    onOpenChange(false)
    dismissGitPush()
  }
  const sendToSession = async (row: Row) => {
    const isLive = actions.live.has(row.s.id)
    let isDelivered = false
    try {
      if (isLive && !actions.waiting.has(row.s.id)) await api.queueAdd(row.s.id, report())
      else if (isLive) await api.sessionSend(row.s.id, report(), SEND_TIMEOUT_MS)
      isDelivered = isLive || (await actions.resumeSend(row, report()))
    } catch (sendError) {
      toast.error(errMsg(sendError))
    }
    if (!isDelivered) return
    toast.success(t("conversation.gitPush.sent", { title: sessionLabel(row) }))
    finishSending()
  }
  const sendToNewSession = () => {
    actions.newSession(run.dir, report())
    finishSending()
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="min-w-0 sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("conversation.gitPush.sendTitle")}</DialogTitle>
          <DialogDescription>{t("conversation.gitPush.sendHint")}</DialogDescription>
        </DialogHeader>
        <div className="flex max-h-72 flex-col gap-1 overflow-y-auto">
          {sameFolderRows.length === 0 && <p className="text-sm text-muted-foreground">{t("conversation.gitPush.noSession")}</p>}
          {sameFolderRows.map((row) => (
            <Button key={row.s.id} variant="outline" className="h-auto justify-between gap-3 py-2 text-left whitespace-normal" onClick={() => sendToSession(row)}>
              <span className="min-w-0 break-words">{sessionLabel(row)}</span>
              {actions.live.has(row.s.id) && <span className="shrink-0 text-xs text-muted-foreground">{t("conversation.gitPush.sessionOpen")}</span>}
            </Button>
          ))}
        </div>
        <Button onClick={sendToNewSession}>{t("conversation.gitPush.newSession")}</Button>
      </DialogContent>
    </Dialog>
  )
}
