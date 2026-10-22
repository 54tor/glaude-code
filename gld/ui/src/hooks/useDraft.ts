import { useSyncExternalStore } from "react"

// Brouillon et messages en file d'attente d'une session, gardés hors du composant : fermer la fenêtre de la session ne les perd pas.
// Le tour en cours et l'envoi des messages en file continuent aussi, car ils ne dépendent pas de l'affichage.
type Entry = { text: string; queue: string[] }
const empty: Entry = { text: "", queue: [] }
const entries = new Map<string, Entry>()
const subs = new Set<() => void>()

const emit = () => subs.forEach((f) => f())

export const draftOf = (id: string): Entry => entries.get(id) ?? empty

export function setDraft(id: string, patch: Partial<Entry>) {
  const next = { ...draftOf(id), ...patch }
  if (!next.text && next.queue.length === 0) entries.delete(id)
  else entries.set(id, next)
  emit()
}

const subscribe = (f: () => void) => {
  subs.add(f)
  return () => void subs.delete(f)
}

export const useDraft = (id: string): Entry => useSyncExternalStore(subscribe, () => draftOf(id))
