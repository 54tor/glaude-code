import { i18n } from "@/i18n"

// Message envoyé à Claude quand un push échoue : ce qui a été lancé, le code de retour et la mention « @chemin » du fichier joint
// qui porte la sortie complète (hook compris), trop longue pour le message.
export function buildGitPushReport(
  run: {
    dir: string
    remote: string
    branch: string
    exitCode?: number
    errorMessage?: string
  },
  attachmentMention?: string
): string {
  const intro = i18n.t("conversation.gitPush.reportIntro", {
    branch: run.branch,
    remote: run.remote,
    dir: run.dir,
  })
  const result =
    run.exitCode !== undefined
      ? i18n.t("conversation.gitPush.reportExit", { code: run.exitCode })
      : i18n.t("conversation.gitPush.reportError", {
          message: run.errorMessage ?? "",
        })
  const outro = i18n.t("conversation.gitPush.reportOutro", {
    branch: run.branch,
    remote: run.remote,
  })
  const attachment = attachmentMention
    ? `${i18n.t("conversation.gitPush.reportAttachment")} ${attachmentMention}\n\n`
    : ""
  return `${intro}\n${result}\n\n${attachment}${outro}`
}
