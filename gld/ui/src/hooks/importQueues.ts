import { toast } from "sonner"
import { i18n } from "@/i18n"
import { api } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { withoutFollowUps } from "@/lib/queue"
import { draftOf, legacyQueues, saveDrafts, SEND_TIMEOUT, setDraft } from "./useDraft"

// Reprise unique des files locales : chaque texte part dans l'ordre vers la file du serveur, et le stockage local est réécrit
// après chaque envoi (une reprise après échec ne duplique rien). Un texte que le serveur refuse, ou dont l'envoi local est
// incertain depuis trop longtemps (il a pu arriver : le rejouer le dupliquerait), revient dans le brouillon de sa session avec
// une notification. Au moindre échec réseau, les données locales restent et le garde n'est pas posé.
const DONE = "gld.queueImported"
let running: Promise<void> | null = null

const toDraft = (id: string, text: string) => {
  const d = draftOf(id)
  setDraft(id, { text: d.text.trim() ? `${d.text}\n${text}` : text })
  toast.error(i18n.t("conversation.composer.importRestored", { text: text.length > 60 ? text.slice(0, 60) + "…" : text }))
}

async function run(): Promise<void> {
  if (localStorage.getItem(DONE)) return // relu sous le verrou : un autre onglet a pu finir entre-temps
  for (const [id, l] of [...legacyQueues]) {
    if (l.sending !== undefined && Date.now() - l.sending > SEND_TIMEOUT && l.queue.length > 0) {
      const [stale, ...rest] = l.queue
      l.queue = rest
      l.sending = undefined
      toDraft(id, withoutFollowUps(stale))
    }
    while (l.queue.length > 0) {
      const text = withoutFollowUps(l.queue[0])
      let refused = false
      try {
        await api.queueAdd(id, text)
      } catch (e) {
        if (e instanceof ApiError && (e.net || e.code.startsWith("http_"))) return // pas de réponse JSON du serveur : la reprise rejouera
        refused = true
      }
      if (!legacyQueues.has(id)) return // changement de compte pendant l'envoi
      l.queue = l.queue.slice(1)
      if (refused) toDraft(id, text)
      saveDrafts()
    }
    legacyQueues.delete(id)
  }
  saveDrafts()
  localStorage.setItem(DONE, "1")
}

export function importLocalQueues(): Promise<void> {
  try {
    if (localStorage.getItem(DONE)) return Promise.resolve()
    running ??= (navigator.locks ? navigator.locks.request(DONE, run) : run()).catch(() => {}).finally(() => void (running = null))
    return running
  } catch {
    return Promise.resolve() // stockage indisponible
  }
}
