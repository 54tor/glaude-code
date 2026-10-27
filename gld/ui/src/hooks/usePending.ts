import { useEffect, useState } from "react"
import { api, type QItem } from "@/lib/api"

// File serveur d'une session, relue toutes les 2 s tant qu'un composeur la montre (le sondage de /session-live ne vit que dans
// la conversation ouverte, pas dans le composeur). refreshPending : relecture immédiate après une action.
const listeners = new Set<() => void>()
export const refreshPending = () => listeners.forEach((f) => f())

export function usePending(id: string): QItem[] {
  const [got, setGot] = useState<{ id: string; items: QItem[] }>({ id, items: [] })
  useEffect(() => {
    let live = true
    const load = () => api.queue(id).then((r) => live && setGot((p) => (p.id === id && JSON.stringify(p.items) === JSON.stringify(r.items) ? p : { id, items: r.items ?? [] })), () => {})
    load()
    listeners.add(load)
    const t = setInterval(() => document.visibilityState === "visible" && load(), 2000)
    return () => {
      live = false
      listeners.delete(load)
      clearInterval(t)
    }
  }, [id])
  return got.id === id ? got.items : []
}
