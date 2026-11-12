import { useEffect, useRef, useState } from "react"
import { Terminal } from "@xterm/xterm"
import "@xterm/xterm/css/xterm.css"
import { useTranslation } from "react-i18next"
import { errMsg } from "@/lib/apiError"
import { streamGitPush } from "@/lib/gitPushStream"
import { refreshGit } from "@/hooks/useGit"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

type PushOutcome = { kind: "running" } | { kind: "finished"; exitCode: number } | { kind: "error"; message: string }

// Terminal en lecture seule qui montre la sortie du push (hook local compris), puis le code de retour de git.
export function GitPushTerminal({ dir, remote, branch, onClose }: { dir: string; remote: string; branch: string; onClose: () => void }) {
  const { t } = useTranslation()
  const containerRef = useRef<HTMLDivElement | null>(null)
  const [outcome, setOutcome] = useState<PushOutcome>({ kind: "running" })

  useEffect(() => {
    if (!containerRef.current) return
    const terminal = new Terminal({ fontSize: 13, disableStdin: true, convertEol: true, scrollback: 2000, cursorInactiveStyle: "none" })
    terminal.open(containerRef.current)
    let isDisposed = false
    streamGitPush(dir, remote, (chunk) => terminal.write(chunk)).then(
      (exitCode) => !isDisposed && setOutcome({ kind: "finished", exitCode }),
      (pushError) => !isDisposed && setOutcome({ kind: "error", message: errMsg(pushError) })
    ).finally(refreshGit)
    return () => {
      isDisposed = true
      terminal.dispose()
    }
  }, [dir, remote])

  const isRunning = outcome.kind === "running"
  const isSuccess = outcome.kind === "finished" && outcome.exitCode === 0
  return (
    <Dialog open onOpenChange={(isOpen) => !isOpen && !isRunning && onClose()}>
      <DialogContent className="min-w-0 sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t("conversation.gitPush.terminalTitle", { branch, remote })}</DialogTitle>
          <DialogDescription className="sr-only">{t("conversation.gitPush.terminalHint")}</DialogDescription>
        </DialogHeader>
        <div className="h-72 overflow-hidden rounded-md bg-black p-2">
          <div ref={containerRef} className="h-full w-full" />
        </div>
        {isRunning && <p className="text-sm text-muted-foreground">{t("conversation.gitPush.running")}</p>}
        {isSuccess && <p role="status" className="text-sm font-medium text-green-600">{t("conversation.gitPush.exitOk")}</p>}
        {outcome.kind === "finished" && !isSuccess && (
          <p role="alert" className="text-sm font-medium text-destructive">{t("conversation.gitPush.exitFailed", { code: outcome.exitCode })}</p>
        )}
        {outcome.kind === "error" && <p role="alert" className="text-sm font-medium text-destructive">{outcome.message}</p>}
      </DialogContent>
    </Dialog>
  )
}
