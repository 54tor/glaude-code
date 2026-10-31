import { useSyncExternalStore } from "react"

// État ouvert/fermé des blocs repliables (dossiers de l'arbre, projets, dépôts, fichiers modifiés, bloc-notes), gardé dans ce
// navigateur. Seuls les choix de l'utilisateur sont enregistrés ; sans choix, un bloc est fermé (ou la valeur par défaut donnée).
const STORAGE_KEY = "gld.open"
const subscribers = new Set<() => void>()

const readStoredState = (): Record<string, boolean> => {
  try {
    const storedState = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "{}")
    return storedState && typeof storedState === "object" ? storedState : {}
  } catch {
    return {}
  }
}
let state: Record<string, boolean> = readStoredState()

const notifySubscribers = () => subscribers.forEach((subscriber) => subscriber())

const subscribe = (subscriber: () => void) => {
  subscribers.add(subscriber)
  return () => void subscribers.delete(subscriber)
}

// Changement de compte : les blocs ouverts nomment ses dossiers.
export function resetOpen() {
  state = {}
  notifySubscribers()
}

export function setOpen(key: string, open: boolean) {
  state = { ...state, [key]: open }
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    /* stockage indisponible : le choix vaut pour cette page seulement */
  }
  notifySubscribers()
}

// Tout l'état : pour les listes de blocs (un arbre) qui lisent plusieurs clés.
export const useOpenState = () => useSyncExternalStore(subscribe, () => state)

// Un bloc : [ouvert, inverser].
export function useOpen(key: string, byDefault = false): [boolean, () => void] {
  const open = useOpenState()[key] ?? byDefault
  return [open, () => setOpen(key, !open)]
}
