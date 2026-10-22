import { useSyncExternalStore } from "react"
import type { GitState } from "@/lib/api"
import { api } from "@/lib/api"

// État git des dossiers de projet, partagé par tous les composants qui l'affichent : une seule relecture périodique (30 s, onglet
// visible, et au retour sur l'onglet) tant qu'au moins un composant l'utilise.
export type Git = { status: Record<string, GitState>; repos: Record<string, string[]> } // repos : dossier de sessions → ses dépôts
let state: Git = { status: {}, repos: {} }
const subs = new Set<() => void>()
let timer: ReturnType<typeof setInterval> | undefined

const load = () =>
  document.visibilityState === "visible" &&
  api.gitStatus().then(
    (r) => {
      if (JSON.stringify(r) === JSON.stringify(state)) return // inchangé : pas de rendu
      state = r
      subs.forEach((f) => f())
    },
    () => {}
  )

function subscribe(f: () => void) {
  subs.add(f)
  if (subs.size === 1) {
    load()
    timer = setInterval(load, 30_000)
    document.addEventListener("visibilitychange", load)
  }
  return () => {
    subs.delete(f)
    if (subs.size === 0) {
      clearInterval(timer)
      document.removeEventListener("visibilitychange", load)
    }
  }
}

export const refreshGit = () => void load()

export const useGit = (): Git => useSyncExternalStore(subscribe, () => state)
