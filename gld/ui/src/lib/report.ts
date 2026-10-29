import { toast } from "sonner"
import { i18n } from "@/i18n"
import { errMsg } from "./apiError"
import type { Result } from "./api"

// Réponse {ok, out} du serveur : une ligne en titre du toast (la première, ou la dernière avec pick "last"), le reste en
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
export function reportResult(r: Result, o: Show = {}) {
  return show(r.ok, r.out, o)
}

// Action de l'API /v1 : le message de la réponse (succès) ou de l'ApiError (échec) s'affiche de la même façon. Renvoie le succès.
export async function reportAction(run: () => Promise<{ message?: string } | void>, o: Show = {}) {
  try {
    const r = await run()
    return show(true, r?.message ?? "", o)
  } catch (e) {
    return show(false, errMsg(e), o)
  }
}
