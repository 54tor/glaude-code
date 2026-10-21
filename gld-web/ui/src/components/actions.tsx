import { createContext, useContext } from "react"
import type { Group, Hit, Item, Session, VGroup } from "@/lib/api"

export type Row = { s: Session; g: Group }

// Actions sur un groupe virtuel (sous-dossier virtuel d'un dossier de travail).
export type GroupActions = {
  rows: (vg: VGroup) => Row[] // ses sessions encore présentes, les plus récentes d'abord
  open: (vg: VGroup) => void // les reprendre toutes dans des onglets de terminal
  refs: (vg: VGroup) => void // copier leurs références, pour un nouveau prompt
  prompt: (vg: VGroup) => void // nouvelle session avec ces références
  export: (vg: VGroup) => void // un seul fichier Markdown
  select: (vg: VGroup) => void
  fav: (vg: VGroup) => void
  rename: (vg: VGroup) => void
  remove: (vg: VGroup) => void
  show: (vg: VGroup) => void
}

// État et actions partagés par la barre latérale, les listes, les menus et le lecteur.
export type Actions = {
  home: string
  q: string
  live: Set<string>
  hit: (id: string) => Hit | undefined
  vgroups: VGroup[]
  favs: Set<string> // sessions favorites
  toggleFav: (ids: string[]) => void
  inGroup: VGroup | null // groupe virtuel affiché
  groupActs: GroupActions
  picked: Set<string> // ids sélectionnés
  selected: Row[]
  pick: (id: string, range?: boolean) => void // range : Maj+clic, de la dernière case cochée à celle-ci
  hotLeft: (s: Session) => number | null // minutes de cache chaud restantes, null si froid
  read: (s: Session) => void
  open: (body: { dir: string; id?: string }) => void
  resumeWith: (r: Row) => void // reprendre en envoyant un message
  newSession: (project: string, initial?: string) => void // nouvelle session dans ce dossier de travail
  copy: (s: Session) => void
  rename: (s: Session) => void
  exportMd: (r: Row) => void
  move: (rows: Row[]) => void
  moveGroup: (g: Group, items: Item[]) => void
  group: (ids: string[], project: string, name: string | null) => void // null : nouveau groupe
  ungroup: (ids: string[], vg?: VGroup) => void // vg : par défaut le groupe affiché
  // after : appelé quand la suppression est confirmée (ex. fermer le lecteur de la session supprimée)
  remove: (items: Item[], what: string, after?: () => void) => void
}

export const ActionsCtx = createContext<Actions | null>(null)

export function useActions(): Actions {
  const a = useContext(ActionsCtx)
  if (!a) throw new Error("ActionsCtx manquant")
  return a
}
