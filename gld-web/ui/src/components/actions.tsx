import { createContext, useContext } from "react"
import type { Group, Item, Session } from "@/lib/api"

// Actions et état d'affichage partagés par tout l'arbre (évite de les faire descendre à la main).
export type Actions = {
  filtering: boolean
  opened: Set<string>
  toggle: (key: string) => void
  read: (s: Session) => void
  open: (body: { dir: string; id?: string }) => void
  move: (s: Session) => void
  moveGroup: (g: Group, items: Item[]) => void
  // after : appelé quand la suppression est confirmée (ex. fermer le lecteur de la session supprimée)
  remove: (items: Item[], what: string, after?: () => void) => void
}

export const ActionsCtx = createContext<Actions | null>(null)

export function useActions(): Actions {
  const a = useContext(ActionsCtx)
  if (!a) throw new Error("ActionsCtx manquant")
  return a
}
