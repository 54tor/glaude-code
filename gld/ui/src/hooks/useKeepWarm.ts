import { useSyncExternalStore } from "react"
import type { WarmState } from "@/lib/api"
import { api } from "@/lib/api"

// État du maintien au chaud des sessions, partagé par l'en-tête de la fenêtre et la liste « Cache chaud » : une seule relecture
// (15 s, onglet visible) tant qu'au moins un composant l'utilise ; relue aussitôt après un changement.
let state: Record<string, WarmState> = {}
const subs = new Set<() => void>()
let timer: ReturnType<typeof setInterval> | undefined

export const refreshWarm = () =>
  api.keepWarm().then(
    (r) => {
      if (JSON.stringify(r.items) === JSON.stringify(state)) return
      state = r.items
      subs.forEach((f) => f())
    },
    () => {}
  )
// Changement de compte : l'état du précédent ne s'affiche pas en attendant la relecture.
export function resetWarm() {
  state = {}
  subs.forEach((f) => f())
}
const tick = () => document.visibilityState === "visible" && refreshWarm()

function subscribe(f: () => void) {
  subs.add(f)
  if (subs.size === 1) {
    tick()
    timer = setInterval(tick, 15_000)
    document.addEventListener("visibilitychange", tick)
  }
  return () => {
    subs.delete(f)
    if (subs.size === 0) {
      clearInterval(timer)
      document.removeEventListener("visibilitychange", tick)
    }
  }
}

export const useKeepWarm = (): Record<string, WarmState> => useSyncExternalStore(subscribe, () => state)
