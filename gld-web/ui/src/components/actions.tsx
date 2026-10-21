import { createContext, useContext } from "react"
import type { Group, Item, Session } from "@/lib/api"

// Actions partagées par la liste et le lecteur (évite de les faire descendre à la main).
export type Actions = {
  home: string
  read: (s: Session) => void
  open: (body: { dir: string; id?: string }) => void
  copy: (s: Session) => void
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
