import { toast } from "sonner"
import { i18n } from "@/i18n"
import { errMsg } from "./apiError"

// Une ligne en titre du toast (la première, ou la dernière avec pick "last"), le reste en
// description. Un `out` de plusieurs lignes ne s'affiche plus en bloc brut. Renvoie r.ok.
type Show = { id?: string | number; pick?: "first" | "last" }
function showToast(ok: boolean, text: string, options: Show) {
  const lines = text
    .trim()
    .split("\n")
    .filter((line) => line.trim())
  const pickedLine = options.pick === "last" ? lines.at(-1) : lines[0]
  const fallbackTitle = i18n.t(ok ? "shell.act.done" : "shell.act.failed")
  const title = pickedLine ?? fallbackTitle
  const description = lines.filter((line) => line !== title).join("\n")
  const notify = ok ? toast.success : toast.error
  notify(title, { id: options.id, description: description || undefined })
  return ok
}
// Action de l'API /v1 : le message de la réponse (succès) ou de l'ApiError (échec) s'affiche de la même façon. Renvoie le succès.
export type Reported = { message?: string; failed?: { item: string; error: { message: string } }[] }
export async function reportAction(run: () => Promise<Reported | void>, options: Show = {}) {
  try {
    const result = await run()
    const ok = showToast(true, result?.message ?? "", options)
    if (result?.failed?.length) {
      const failureLines = result.failed.map((failure) => `${failure.item} : ${failure.error.message}`)
      showToast(false, failureLines.join("\n"), {})
    }
    return ok
  } catch (actionError) {
    return showToast(false, errMsg(actionError), options)
  }
}
