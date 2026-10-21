import { createContext, useContext } from "react"
import type { Group, Hit, Item, Session, VGroup } from "@/lib/api"

export type Row = { s: Session; g: Group }

// État et actions partagés par la barre latérale, les listes, les menus et le lecteur.
export type Actions = {
  home: string
  q: string
  live: Set<string>
  hit: (id: string) => Hit | undefined
  vgroups: VGroup[]
  inGroup: string | null // groupe virtuel affiché
  picked: Set<string> // ids sélectionnés
  selected: Row[]
  pick: (id: string) => void
  read: (s: Session) => void
  open: (body: { dir: string; id?: string }) => void
  copy: (s: Session) => void
  rename: (s: Session) => void
  exportMd: (r: Row) => void
  move: (rows: Row[]) => void
  moveGroup: (g: Group, items: Item[]) => void
  group: (ids: string[], name: string | null) => void // null : nouveau groupe
  ungroup: (ids: string[], name?: string) => void // name : par défaut le groupe affiché
  // after : appelé quand la suppression est confirmée (ex. fermer le lecteur de la session supprimée)
  remove: (items: Item[], what: string, after?: () => void) => void
}

export const ActionsCtx = createContext<Actions | null>(null)

export function useActions(): Actions {
  const a = useContext(ActionsCtx)
  if (!a) throw new Error("ActionsCtx manquant")
  return a
}
