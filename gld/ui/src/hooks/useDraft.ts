import { useSyncExternalStore } from "react"

// Brouillon d'une session, gardé hors du composant et dans le navigateur (localStorage) : fermer la fenêtre ou recharger la page
// ne le perd pas. La file des messages vit sur le serveur ; les anciennes files locales sont reprises une fois (importLocalQueues).
type Entry = { text: string }
type Legacy = { queue: string[]; sending?: number }
type StoredEntry = { text?: unknown; queue?: unknown; sending?: unknown }
const emptyEntry: Entry = { text: "" }
const STORAGE_KEY = "gld.drafts"
const MAX_SESSIONS = 50

// Files locales d'avant le serveur, tenues de côté et réécrites telles quelles tant qu'elles ne sont pas importées.
const legacy = new Map<string, Legacy>()

const readStoredEntry = (sessionId: string, storedEntry: StoredEntry): [string, Entry] => {
  const queue = Array.isArray(storedEntry.queue)
    ? storedEntry.queue.filter((queuedText): queuedText is string => typeof queuedText === "string")
    : []
  if (queue.length > 0) {
    const sending = typeof storedEntry.sending === "number" ? storedEntry.sending : undefined
    legacy.set(sessionId, { queue, sending })
  }
  const text = typeof storedEntry.text === "string" ? storedEntry.text : ""
  return [sessionId, { text }]
}

const load = (): Map<string, Entry> => {
  legacy.clear()
  try {
    const raw = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}") as Record<string, StoredEntry>
    return new Map(Object.entries(raw).map(([sessionId, storedEntry]) => readStoredEntry(sessionId, storedEntry)))
  } catch {
    return new Map()
  }
}
const entries = load()
const subscribers = new Set<() => void>()
const notifySubscribers = () => subscribers.forEach((subscriber) => subscriber())

let writable = () => true
export const guardDrafts = (isWritable: () => boolean) => void (writable = isWritable)

const save = () => {
  if (!writable()) return
  try {
    // les plus récemment modifiées en dernier : au-delà de la limite, les plus anciennes partent
    const keptEntries = [...entries].slice(-MAX_SESSIONS)
    const serialized: Record<string, Entry & Partial<Legacy>> = Object.fromEntries(keptEntries)
    for (const [sessionId, legacyQueue] of legacy) serialized[sessionId] = { ...(serialized[sessionId] ?? emptyEntry), ...legacyQueue }
    localStorage.setItem(STORAGE_KEY, JSON.stringify(serialized))
  } catch {
    /* stockage plein ou indisponible : le magasin en mémoire continue de servir */
  }
}

// Un autre onglet a réécrit le stockage : on repart de sa copie, sinon le prochain save de cet onglet l'écraserait.
if (typeof window !== "undefined")
  window.addEventListener("storage", (event) => {
    if (event.key !== STORAGE_KEY || !writable()) return
    entries.clear()
    for (const [sessionId, entry] of load()) entries.set(sessionId, entry)
    notifySubscribers()
  })

// Changement de compte : seule la copie en mémoire part ; le stockage est vidé par l'appelant, ou appartient déjà à un autre compte.
export function clearDrafts() {
  entries.clear()
  legacy.clear() // la file locale du compte précédent ne part jamais vers la file serveur du suivant
  notifySubscribers()
}

export const draftOf = (id: string): Entry => entries.get(id) ?? emptyEntry

export function setDraft(id: string, patch: Partial<Entry>) {
  const nextEntry = { ...draftOf(id), ...patch }
  entries.delete(id) // ré-insérée à la fin
  if (nextEntry.text) entries.set(id, nextEntry)
  save()
  notifySubscribers()
}

export const subscribe = (subscriber: () => void) => {
  subscribers.add(subscriber)
  return () => void subscribers.delete(subscriber)
}

export const useDraft = (id: string): Entry => useSyncExternalStore(subscribe, () => draftOf(id))

// Pour importLocalQueues (importQueues.ts, à part : api.ts importe ce module via auth.ts).
export const legacyQueues = legacy
export const SEND_TIMEOUT = 25_000
export const saveDrafts = save
