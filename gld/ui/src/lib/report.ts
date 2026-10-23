import { toast } from "sonner"
import { i18n } from "@/i18n"
import type { Result } from "./api"

// Réponse {ok, out} du serveur : une ligne en titre du toast (la première, ou la dernière avec pick "last"), le reste en
// description. Un `out` de plusieurs lignes ne s'affiche plus en bloc brut. Renvoie r.ok.
export function reportResult(r: Result, o: { id?: string | number; pick?: "first" | "last" } = {}) {
  const lines = r.out
    .trim()
    .split("\n")
    .filter((l) => l.trim())
  const main = (o.pick === "last" ? lines.at(-1) : lines[0]) ?? i18n.t(r.ok ? "shell.act.done" : "shell.act.failed")
  const rest = lines.filter((l) => l !== main).join("\n")
  ;(r.ok ? toast.success : toast.error)(main, { id: o.id, description: rest || undefined })
  return r.ok
}
