import { useSyncExternalStore } from "react"
import type { WarmState } from "@/lib/api"
import { api } from "@/lib/api"

// État du maintien au chaud des sessions, partagé par l'en-tête de la fenêtre et la liste « Cache chaud » : une seule relecture
// (15 s, onglet visible) tant qu'au moins un composant l'utilise ; relue aussitôt après un changement.
const REFRESH_INTERVAL_MS = 15_000
let state: Record<string, WarmState> = {}
const subscribers = new Set<() => void>()
let refreshTimer: ReturnType<typeof setInterval> | undefined

const notifySubscribers = () => subscribers.forEach((subscriber) => subscriber())

export const refreshWarm = () =>
  api.keepWarm().then(
    (response) => {
      if (JSON.stringify(response.items) === JSON.stringify(state)) return
      state = response.items
      notifySubscribers()
    },
    () => {}
  )
// Changement de compte : l'état du précédent ne s'affiche pas en attendant la relecture.
export function resetWarm() {
  state = {}
  notifySubscribers()
}
const refreshIfVisible = () => document.visibilityState === "visible" && refreshWarm()

function subscribe(subscriber: () => void) {
  subscribers.add(subscriber)
  if (subscribers.size === 1) {
    refreshIfVisible()
    refreshTimer = setInterval(refreshIfVisible, REFRESH_INTERVAL_MS)
    document.addEventListener("visibilitychange", refreshIfVisible)
  }
  return () => {
    subscribers.delete(subscriber)
    if (subscribers.size === 0) {
      clearInterval(refreshTimer)
      document.removeEventListener("visibilitychange", refreshIfVisible)
    }
  }
}

export const useKeepWarm = (): Record<string, WarmState> => useSyncExternalStore(subscribe, () => state)
