import { toast } from "sonner"
import { i18n } from "@/i18n"
import { errMsg } from "./apiError"

// Une ligne en titre du toast (la première, ou la dernière avec pick "last"), le reste en
// description. Un `out` de plusieurs lignes ne s'affiche plus en bloc brut. Renvoie r.ok.
type Show = { id?: string | number; pick?: "first" | "last" }
function show(ok: boolean, text: string, o: Show) {
  const lines = text
    .trim()
    .split("\n")
    .filter((l) => l.trim())
  const main = (o.pick === "last" ? lines.at(-1) : lines[0]) ?? i18n.t(ok ? "shell.act.done" : "shell.act.failed")
  const rest = lines.filter((l) => l !== main).join("\n")
  ;(ok ? toast.success : toast.error)(main, { id: o.id, description: rest || undefined })
  return ok
}
// Action de l'API /v1 : le message de la réponse (succès) ou de l'ApiError (échec) s'affiche de la même façon. Renvoie le succès.
export type Reported = { message?: string; failed?: { item: string; error: { message: string } }[] }
export async function reportAction(run: () => Promise<Reported | void>, o: Show = {}) {
  try {
    const r = await run()
    const ok = show(true, r?.message ?? "", o)
    if (r?.failed?.length) show(false, r.failed.map((f) => `${f.item} : ${f.error.message}`).join("\n"), {})
    return ok
  } catch (e) {
    return show(false, errMsg(e), o)
  }
}
