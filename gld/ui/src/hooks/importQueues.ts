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
const IMPORT_DONE_KEY = "gld.queueImported"
const RESTORED_PREVIEW_LENGTH = 60
let runningImport: Promise<void> | null = null

const truncateForPreview = (text: string) => {
  if (text.length <= RESTORED_PREVIEW_LENGTH) return text
  return text.slice(0, RESTORED_PREVIEW_LENGTH) + "…"
}

const restoreToDraft = (sessionId: string, text: string) => {
  const currentDraft = draftOf(sessionId)
  const draftHasText = currentDraft.text.trim() !== ""
  setDraft(sessionId, { text: draftHasText ? `${currentDraft.text}\n${text}` : text })
  toast.error(i18n.t("conversation.composer.importRestored", { text: truncateForPreview(text) }))
}

async function importQueuesUnderLock(): Promise<void> {
  if (localStorage.getItem(IMPORT_DONE_KEY)) return // relu sous le verrou : un autre onglet a pu finir entre-temps
  for (const [sessionId, legacyQueue] of [...legacyQueues]) {
    const sendingSince = legacyQueue.sending
    const sendIsStale = sendingSince !== undefined && Date.now() - sendingSince > SEND_TIMEOUT
    if (sendIsStale && legacyQueue.queue.length > 0) {
      const [staleText, ...remainingTexts] = legacyQueue.queue
      legacyQueue.queue = remainingTexts
      legacyQueue.sending = undefined
      restoreToDraft(sessionId, withoutFollowUps(staleText))
    }
    while (legacyQueue.queue.length > 0) {
      const text = withoutFollowUps(legacyQueue.queue[0])
      let refused = false
      try {
        await api.queueAdd(sessionId, text)
      } catch (error) {
        const serverGaveNoJsonAnswer = error instanceof ApiError && (error.net || error.code.startsWith("http_"))
        if (serverGaveNoJsonAnswer) return // la reprise rejouera
        refused = true
      }
      if (!legacyQueues.has(sessionId)) return // changement de compte pendant l'envoi
      legacyQueue.queue = legacyQueue.queue.slice(1)
      if (refused) restoreToDraft(sessionId, text)
      saveDrafts()
    }
    legacyQueues.delete(sessionId)
  }
  saveDrafts()
  localStorage.setItem(IMPORT_DONE_KEY, "1")
}

const startImport = (): Promise<void> => {
  if (navigator.locks) return navigator.locks.request(IMPORT_DONE_KEY, importQueuesUnderLock)
  return importQueuesUnderLock()
}

export function importLocalQueues(): Promise<void> {
  try {
    if (localStorage.getItem(IMPORT_DONE_KEY)) return Promise.resolve()
    runningImport ??= startImport()
      .catch(() => {})
      .finally(() => void (runningImport = null))
    return runningImport
  } catch {
    return Promise.resolve() // stockage indisponible
  }
}
