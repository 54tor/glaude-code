import { useEffect, useState } from "react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import "@xterm/xterm/css/xterm.css"
import { Minimize2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import { readGitPushChunks, setGitPushTerminalOpen, subscribeGitPushOutput, type GitPushRun } from "@/hooks/useGitPush"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

// Terminal en lecture seule du push courant : rejoue la sortie déjà reçue puis suit la suite. Le fermer ou le réduire ne
// l'arrête pas ; le toast du push permet de le rouvrir.
export function GitPushTerminal({ run }: { run: GitPushRun }) {
  const { t } = useTranslation()
  const [terminalHost, setTerminalHost] = useState<HTMLDivElement | null>(null)
  const isOpen = run.isTerminalOpen

  useEffect(() => {
    if (!terminalHost) return
    const terminal = new Terminal({ fontSize: 13, disableStdin: true, convertEol: true, scrollback: 2000, cursorInactiveStyle: "none" })
    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(terminalHost)
    fitAddon.fit()
    const resizeObserver = new ResizeObserver(() => fitAddon.fit())
    resizeObserver.observe(terminalHost)
    readGitPushChunks(run.id).forEach((chunk) => terminal.write(chunk))
    const unsubscribe = subscribeGitPushOutput(run.id, (chunk) => terminal.write(chunk))
    return () => {
      resizeObserver.disconnect()
      unsubscribe()
      terminal.dispose()
    }
  }, [terminalHost, run.id])

  const isRunning = run.status === "running"
  const isSuccess = run.status === "succeeded"
  return (
    <Dialog open={isOpen} onOpenChange={(isOpening) => setGitPushTerminalOpen(run.id, isOpening)}>
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
            <Button size="sm" variant="outline" onClick={() => setGitPushTerminalOpen(run.id, false)}>
              <Minimize2 /> {t("conversation.gitPush.minimize")}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
