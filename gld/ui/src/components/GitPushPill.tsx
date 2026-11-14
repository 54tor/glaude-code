import { Loader2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import { setGitPushTerminalOpen, type GitPushRun } from "@/hooks/useGitPush"

const PILL_STACK_STEP_REM = 2.25

export function GitPushPill({ run, position }: { run: GitPushRun; position: number }) {
  const { t } = useTranslation()
  const label = t("conversation.gitPush.toastRunning", { branch: run.branch, remote: run.remote })
  return (
    <button
      type="button"
      onClick={() => setGitPushTerminalOpen(run.id, true)}
      title={t("conversation.gitPush.openTerminal")}
      style={{ bottom: `calc(0.75rem + ${position * PILL_STACK_STEP_REM}rem)` }}
      className="fixed left-3 z-[100] flex max-w-[60vw] cursor-pointer items-center gap-2 rounded-full border bg-popover px-3 py-1.5 text-xs text-popover-foreground shadow-md"
    >
      <Loader2 className="size-3.5 shrink-0 animate-spin" aria-hidden />
      <span className="truncate">{label}</span>
    </button>
  )
}
