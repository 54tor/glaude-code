import { useEffect, useState } from "react"
import { api, type QItem } from "@/lib/api"

// File serveur d'une session, relue toutes les 2 s tant qu'un composeur la montre (le sondage de /v1/sessions/{id}/live ne vit que dans
// la conversation ouverte, pas dans le composeur). refreshPending : relecture immédiate après une action.
const POLL_INTERVAL_MS = 2000
const listeners = new Set<() => void>()
export const refreshPending = () => listeners.forEach((listener) => listener())

export function usePending(id: string): QItem[] {
  const [loaded, setLoaded] = useState<{ id: string; items: QItem[] }>({ id, items: [] })
  useEffect(() => {
    let mounted = true
    const load = () =>
      api.queue(id).then(
        (response) =>
          mounted &&
          setLoaded((previous) => {
            const unchanged = previous.id === id && JSON.stringify(previous.items) === JSON.stringify(response.items)
            if (unchanged) return previous
            return { id, items: response.items ?? [] }
          }),
        () => {}
      )
    load()
    listeners.add(load)
    const pollTimer = setInterval(() => document.visibilityState === "visible" && load(), POLL_INTERVAL_MS)
    return () => {
      mounted = false
      listeners.delete(load)
      clearInterval(pollTimer)
    }
  }, [id])
  return loaded.id === id ? loaded.items : []
}
