import { useSyncExternalStore } from "react"
import type { GitState } from "@/lib/api"
import { api } from "@/lib/api"

// État git des dossiers de projet, partagé par tous les composants qui l'affichent : une seule relecture périodique (30 s, onglet
// visible, et au retour sur l'onglet) tant qu'au moins un composant l'utilise.
export type Git = { status: Record<string, GitState>; repos: Record<string, string[]> } // repos : dossier de sessions → ses dépôts
const REFRESH_INTERVAL_MS = 30_000
let state: Git = { status: {}, repos: {} }
const subscribers = new Set<() => void>()
let refreshTimer: ReturnType<typeof setInterval> | undefined

const notifySubscribers = () => subscribers.forEach((subscriber) => subscriber())

const load = () =>
  document.visibilityState === "visible" &&
  api.gitStatus().then(
    (freshState) => {
      if (JSON.stringify(freshState) === JSON.stringify(state)) return // inchangé : pas de rendu
      state = freshState
      notifySubscribers()
    },
    () => {}
  )

function subscribe(subscriber: () => void) {
  subscribers.add(subscriber)
  if (subscribers.size === 1) {
    load()
    refreshTimer = setInterval(load, REFRESH_INTERVAL_MS)
    document.addEventListener("visibilitychange", load)
  }
  return () => {
    subscribers.delete(subscriber)
    if (subscribers.size === 0) {
      clearInterval(refreshTimer)
      document.removeEventListener("visibilitychange", load)
    }
  }
}

export const refreshGit = () => void load()

// Changement de compte : l'état du précédent ne s'affiche pas en attendant la relecture.
export function resetGit() {
  state = { status: {}, repos: {} }
  notifySubscribers()
}

export const useGit = (): Git => useSyncExternalStore(subscribe, () => state)
