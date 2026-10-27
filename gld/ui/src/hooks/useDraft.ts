import { useSyncExternalStore } from "react"

// Brouillon d'une session, gardé hors du composant et dans le navigateur (localStorage) : fermer la fenêtre ou recharger la page
// ne le perd pas. La file des messages vit sur le serveur ; les anciennes files locales sont reprises une fois (importLocalQueues).
type Entry = { text: string }
type Legacy = { queue: string[]; sending?: number }
const empty: Entry = { text: "" }
const KEY = "gld.drafts"
const MAX_SESSIONS = 50

// Files locales d'avant le serveur, tenues de côté et réécrites telles quelles tant qu'elles ne sont pas importées.
const legacy = new Map<string, Legacy>()
const load = (): Map<string, Entry> => {
  legacy.clear()
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? "{}") as Record<string, { text?: unknown; queue?: unknown; sending?: unknown }>
    return new Map(
      Object.entries(raw).map(([id, e]) => {
        const queue = Array.isArray(e.queue) ? e.queue.filter((q): q is string => typeof q === "string") : []
        if (queue.length > 0) legacy.set(id, { queue, sending: typeof e.sending === "number" ? e.sending : undefined })
        return [id, { text: typeof e.text === "string" ? e.text : "" }]
      })
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
    const out: Record<string, Entry & Partial<Legacy>> = Object.fromEntries(kept)
    for (const [id, l] of legacy) out[id] = { ...(out[id] ?? empty), ...l }
    localStorage.setItem(KEY, JSON.stringify(out))
  } catch {
    /* stockage plein ou indisponible : le magasin en mémoire continue de servir */
  }
}

// Un autre onglet a réécrit le stockage : on repart de sa copie, sinon le prochain save de cet onglet l'écraserait.
if (typeof window !== "undefined")
  window.addEventListener("storage", (e) => {
    if (e.key !== KEY || !writable()) return
    entries.clear()
    for (const [id, v] of load()) entries.set(id, v)
    subs.forEach((f) => f())
  })

// Changement de compte : seule la copie en mémoire part ; le stockage est vidé par l'appelant, ou appartient déjà à un autre compte.
export function clearDrafts() {
  entries.clear()
  subs.forEach((f) => f())
}

export const draftOf = (id: string): Entry => entries.get(id) ?? empty

export function setDraft(id: string, patch: Partial<Entry>) {
  const next = { ...draftOf(id), ...patch }
  entries.delete(id) // ré-insérée à la fin
  if (next.text) entries.set(id, next)
  save()
  subs.forEach((f) => f())
}

export const subscribe = (f: () => void) => {
  subs.add(f)
  return () => void subs.delete(f)
}

export const useDraft = (id: string): Entry => useSyncExternalStore(subscribe, () => draftOf(id))

// Pour importLocalQueues (importQueues.ts, à part : api.ts importe ce module via auth.ts).
export const legacyQueues = legacy
export const SEND_TIMEOUT = 25_000
export const saveDrafts = save
