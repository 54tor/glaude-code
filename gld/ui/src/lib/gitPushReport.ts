import { i18n } from "@/i18n"

const MAX_REPORT_LINES = 120

// Message envoyé à Claude quand un push échoue : ce qui a été lancé, le code de retour et la fin de la sortie (hook compris).
export function buildGitPushReport(run: { dir: string; remote: string; branch: string; exitCode?: number; errorMessage?: string }, outputTail: string): string {
  const intro = i18n.t("conversation.gitPush.reportIntro", { branch: run.branch, remote: run.remote, dir: run.dir })
  const result = run.exitCode !== undefined ? i18n.t("conversation.gitPush.reportExit", { code: run.exitCode }) : i18n.t("conversation.gitPush.reportError", { message: run.errorMessage ?? "" })
  const keptLines = outputTail.split("\n").slice(-MAX_REPORT_LINES).join("\n")
  const outro = i18n.t("conversation.gitPush.reportOutro")
  return `${intro}\n${result}\n\n\`\`\`\n${keptLines}\n\`\`\`\n\n${outro}`
}
