import { api } from "@/lib/api"
import { withoutFollowUps } from "@/lib/queue"
import { legacyQueues, saveDrafts, SEND_TIMEOUT } from "./useDraft"

// Reprise unique des files locales : chaque texte part dans l'ordre vers la file du serveur. Un premier message dont l'envoi
// local est incertain (marqueur « sending » trop ancien) n'est pas repris : il a pu arriver, le rejouer le dupliquerait.
// Au moindre échec réseau, les données locales restent et le garde n'est pas posé : un prochain démarrage réessaie.
const DONE = "gld.queueImported"
export async function importLocalQueues(): Promise<void> {
  try {
    if (localStorage.getItem(DONE)) return
  } catch {
    return
  }
  for (const [id, l] of [...legacyQueues]) {
    const head = l.sending !== undefined && Date.now() - l.sending > SEND_TIMEOUT ? l.queue.slice(0, 1) : []
    for (const text of l.queue.slice(head.length)) {
      const r = await api.queueAdd(id, withoutFollowUps(text))
      if (r.net) return
      l.queue = [...head, ...l.queue.slice(head.length + 1)] // retiré au fur et à mesure ; refusé (file pleine) : inutile de rejouer à l'identique
    }
    legacyQueues.delete(id)
  }
  saveDrafts()
  try {
    localStorage.setItem(DONE, "1")
  } catch {
    /* stockage indisponible */
  }
}
