import { useEffect, useState } from "react"
import { Terminal } from "@xterm/xterm"
import "@xterm/xterm/css/xterm.css"
import { Minimize2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import { readGitPushChunks, setGitPushTerminalOpen, subscribeGitPushOutput, useGitPushRun } from "@/hooks/useGitPush"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

// Terminal en lecture seule du push courant : rejoue la sortie déjà reçue puis suit la suite. Le fermer ou le réduire ne
// l'arrête pas ; le toast du push permet de le rouvrir.
export function GitPushTerminal() {
  const { t } = useTranslation()
  const run = useGitPushRun()
  const [terminalHost, setTerminalHost] = useState<HTMLDivElement | null>(null)
  const isOpen = !!run?.isTerminalOpen

  useEffect(() => {
    if (!terminalHost) return
    const terminal = new Terminal({ fontSize: 13, disableStdin: true, convertEol: true, scrollback: 2000, cursorInactiveStyle: "none" })
    terminal.open(terminalHost)
    readGitPushChunks().forEach((chunk) => terminal.write(chunk))
    const unsubscribe = subscribeGitPushOutput((chunk) => terminal.write(chunk))
    return () => {
      unsubscribe()
      terminal.dispose()
    }
  }, [terminalHost])

  if (!run) return null
  const isRunning = run.status === "running"
  const isSuccess = run.status === "succeeded"
  return (
    <Dialog open={isOpen} onOpenChange={setGitPushTerminalOpen}>
      <DialogContent className="min-w-0 sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t("conversation.gitPush.terminalTitle", { branch: run.branch, remote: run.remote })}</DialogTitle>
          <DialogDescription className="sr-only">{t("conversation.gitPush.terminalHint")}</DialogDescription>
        </DialogHeader>
        <div className="h-72 overflow-hidden rounded-md bg-black p-2">
          <div ref={setTerminalHost} className="h-full w-full" />
        </div>
        <div className="flex items-center gap-3">
          <div className="min-w-0 flex-1 text-sm">
            {isRunning && <p className="text-muted-foreground">{t("conversation.gitPush.running")}</p>}
            {isSuccess && <p role="status" className="font-medium text-green-600">{t("conversation.gitPush.exitOk")}</p>}
            {run.status === "failed" && <p role="alert" className="font-medium text-destructive">{t("conversation.gitPush.exitFailed", { code: run.exitCode })}</p>}
            {run.status === "error" && <p role="alert" className="font-medium text-destructive">{run.errorMessage}</p>}
          </div>
          {isRunning && (
            <Button size="sm" variant="outline" onClick={() => setGitPushTerminalOpen(false)}>
              <Minimize2 /> {t("conversation.gitPush.minimize")}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
