import { useSyncExternalStore } from "react"

// État ouvert/fermé des blocs repliables (dossiers de l'arbre, projets, dépôts, fichiers modifiés, bloc-notes), gardé dans ce
// navigateur. Seuls les choix de l'utilisateur sont enregistrés ; sans choix, un bloc est fermé (ou la valeur par défaut donnée).
const KEY = "gld.open"
const subs = new Set<() => void>()
let state: Record<string, boolean> = (() => {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? "{}")
    return v && typeof v === "object" ? v : {}
  } catch {
    return {}
  }
})()

const subscribe = (f: () => void) => {
  subs.add(f)
  return () => void subs.delete(f)
}

export function setOpen(key: string, open: boolean) {
  state = { ...state, [key]: open }
  try {
    localStorage.setItem(KEY, JSON.stringify(state))
  } catch {
    /* stockage indisponible : le choix vaut pour cette page seulement */
  }
  subs.forEach((f) => f())
}

// Tout l'état : pour les listes de blocs (un arbre) qui lisent plusieurs clés.
export const useOpenState = () => useSyncExternalStore(subscribe, () => state)

// Un bloc : [ouvert, inverser].
export function useOpen(key: string, byDefault = false): [boolean, () => void] {
  const open = useOpenState()[key] ?? byDefault
  return [open, () => setOpen(key, !open)]
}
