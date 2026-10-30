import { useSyncExternalStore } from "react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { tilde } from "@/lib/format"

const asked = new Set<string>() // dossiers dont la question est déjà affichée

export const resetTrust = () => asked.clear()

// Dossiers en attente de réponse, affichés par TrustDialog (modale plein écran, une question à la fois).
export type TrustAsk = { dir: string; label: string; answer: (yes: boolean) => void }
let pending: TrustAsk[] = []
const subs = new Set<() => void>()
const set = (next: TrustAsk[]) => ((pending = next), subs.forEach((f) => f()))
export const useTrustAsk = () =>
  useSyncExternalStore(
    (f) => (subs.add(f), () => void subs.delete(f)),
    () => pending
  )

// Une session lancée dans un dossier neuf attend, dans son terminal, la réponse au dialogue de confiance de Claude Code. On guette
// ce dialogue quelques secondes et on pose la question ici ; la réponse est envoyée au terminal.
export async function watchTrust(home: string) {
  for (let i = 0; i < 25; i++) {
    await new Promise((r) => setTimeout(r, 1000))
    const { dirs } = await api.trustPending().catch(() => ({ dirs: [] as string[] }))
    for (const dir of dirs.filter((d) => !asked.has(d))) {
      asked.add(dir)
      const answer = (yes: boolean) =>
        api.trustAnswer(dir, yes).then(
          () => void asked.delete(dir),
          (e) => (asked.delete(dir), toast.error(errMsg(e)))
        )
      const label = tilde(dir, home)
      set([...pending, { dir, label, answer: (yes) => (set(pending.filter((p) => p.dir !== dir)), void answer(yes)) }])
    }
    if (dirs.length > 0) return
  }
}
