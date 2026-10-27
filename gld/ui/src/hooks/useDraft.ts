import { useSyncExternalStore } from "react"

// Brouillon et messages en file d'attente d'une session, gardés hors du composant et dans le navigateur (localStorage) : fermer la
// fenêtre, échouer à livrer un message ou recharger la page ne les perd pas. Le tour en cours et l'envoi des messages en file
// continuent aussi fenêtre fermée, car ils ne dépendent pas de l'affichage ; après un rechargement, la file reste suspendue.
type Entry = { text: string; queue: string[] }
const empty: Entry = { text: "", queue: [] }
const KEY = "gld.drafts"
const MAX_SESSIONS = 50

const load = (): Map<string, Entry> => {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? "{}") as Record<string, Partial<Entry>>
    return new Map(
      Object.entries(raw).map(([id, e]) => [id, { text: typeof e.text === "string" ? e.text : "", queue: Array.isArray(e.queue) ? e.queue.filter((q): q is string => typeof q === "string") : [] }])
    )
  } catch {
    return new Map()
  }
}
const entries = load()
const subs = new Set<() => void>()

let writable = () => true
export const guardDrafts = (f: () => boolean) => void (writable = f)

const save = () => {
  if (!writable()) return
  try {
    // les plus récemment modifiées en dernier : au-delà de la limite, les plus anciennes partent
    const kept = [...entries].slice(-MAX_SESSIONS)
    localStorage.setItem(KEY, JSON.stringify(Object.fromEntries(kept)))
  } catch {
    /* stockage plein ou indisponible : le magasin en mémoire continue de servir */
  }
}

// Changement de compte : seule la copie en mémoire part ; le stockage est vidé par l'appelant, ou appartient déjà à un autre compte.
export function clearDrafts() {
  entries.clear()
  subs.forEach((f) => f())
}

export const draftOf = (id: string): Entry => entries.get(id) ?? empty

export function setDraft(id: string, patch: Partial<Entry>) {
  const next = { ...draftOf(id), ...patch }
  entries.delete(id) // ré-insérée à la fin
  if (next.text || next.queue.length > 0) entries.set(id, next)
  save()
  subs.forEach((f) => f())
}

export const subscribe = (f: () => void) => {
  subs.add(f)
  return () => void subs.delete(f)
}

// Sessions qui ont des messages en file, en une chaîne (valeur stable pour useSyncExternalStore).
export const queuedKey = () => [...entries].filter(([, e]) => e.queue.length > 0).map(([id]) => id).join(",")

export const useDraft = (id: string): Entry => useSyncExternalStore(subscribe, () => draftOf(id))
