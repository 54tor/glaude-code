import { useSyncExternalStore } from "react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { tilde } from "@/lib/format"

const TRUST_POLL_ATTEMPTS = 25
const TRUST_POLL_INTERVAL_MS = 1000

const askedDirs = new Set<string>() // dossiers dont la question est déjà affichée

export const resetTrust = () => askedDirs.clear()

// Dossiers en attente de réponse, affichés par TrustDialog (modale plein écran, une question à la fois).
export type TrustAsk = { dir: string; label: string; answer: (yes: boolean) => void }
let pendingAsks: TrustAsk[] = []
const subscribers = new Set<() => void>()
const setPendingAsks = (nextAsks: TrustAsk[]) => {
  pendingAsks = nextAsks
  subscribers.forEach((notify) => notify())
}
export const useTrustAsk = () =>
  useSyncExternalStore(
    (notify) => {
      subscribers.add(notify)
      return () => void subscribers.delete(notify)
    },
    () => pendingAsks
  )

const wait = (durationMs: number) => new Promise((resolve) => setTimeout(resolve, durationMs))

// Une session lancée dans un dossier neuf attend, dans son terminal, la réponse au dialogue de confiance de Claude Code. On guette
// ce dialogue quelques secondes et on pose la question ici ; la réponse est envoyée au terminal.
export async function watchTrust(home: string) {
  for (let attempt = 0; attempt < TRUST_POLL_ATTEMPTS; attempt++) {
    await wait(TRUST_POLL_INTERVAL_MS)
    const { dirs } = await api.trustPending().catch(() => ({ dirs: [] as string[] }))
    const newDirs = dirs.filter((dir) => !askedDirs.has(dir))
    for (const dir of newDirs) {
      askedDirs.add(dir)
      const sendAnswer = (yes: boolean) =>
        api.trustAnswer(dir, yes).then(
          () => void askedDirs.delete(dir),
          (answerError) => {
            askedDirs.delete(dir)
            return toast.error(errMsg(answerError))
          }
        )
      const label = tilde(dir, home)
      const answer = (yes: boolean) => {
        setPendingAsks(pendingAsks.filter((ask) => ask.dir !== dir))
        void sendAnswer(yes)
      }
      setPendingAsks([...pendingAsks, { dir, label, answer }])
    }
    if (dirs.length > 0) return
  }
}
