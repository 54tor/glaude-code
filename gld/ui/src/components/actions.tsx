import { createContext, useContext } from "react"
import type { Caps, Group, Hit, Item, Project, Session, Snippet, VGroup } from "@/lib/api"
import type { Choice } from "@/lib/models"

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

// Projets : ensembles nommés de dossiers de travail.
export type ProjectActions = {
  add: (dir: string, name: string | null) => void // null : nouveau projet (nom demandé)
  remove: (dir: string, name: string) => void
  rename: (p: Project) => void
  delete: (p: Project) => void
}

// État et actions partagés par la barre latérale, les listes, les menus et le lecteur.
export type Actions = {
  home: string
  q: string
  root: string // dossier des transcripts
  rows: Row[] // toutes les sessions, les plus récentes d'abord
  live: Set<string>
  background: Set<string> // parmi `live`, les tâches de fond (sans terminal)
  waiting: Set<string> // ouvertes, en attente de votre message
  asking: Set<string> // ouvertes, avec une question ou une demande d'autorisation ouverte dans leur terminal
  hit: (id: string) => Hit | undefined
  registeredDirs: string[] // dossiers ajoutés à la main, sans session
  addDir: () => void // choisir un dossier à ajouter à la liste
  addDirPath: (dir: string) => void // ajouter ce dossier (déposé depuis un projet)
  removeDir: (project: string) => void // retirer un dossier de la liste (ses sessions, s'il en a, ne bougent pas)
  openPerms: (project: string) => void // onglet Permissions de Configuration, sur les réglages de ce dossier
  folder: (project: string) => Group | undefined // dossier de travail et ses sessions
  vgroups: VGroup[]
  projects: Project[]
  projectActs: ProjectActions
  favs: Set<string> // sessions favorites
  toggleFav: (ids: string[]) => void
  inGroup: VGroup | null // groupe virtuel affiché
  groupActs: GroupActions
  picked: Set<string> // ids sélectionnés
  selected: Row[]
  pick: (id: string, range?: boolean) => void // range : Maj+clic, de la dernière case cochée à celle-ci
  hotLeft: (s: Session) => number | null // minutes de cache chaud restantes, null si froid
  touch: (id: string) => void // un prompt vient d'être envoyé à la session : elle remonte tout de suite (cache chaud), la liste se relit ensuite
  read: (s: Pick<Session, "id">) => void // seul l'id sert : on peut ouvrir une session absente de la liste chargée
  info: (r: Row) => void // fiche de la session
  open: (body: { dir: string; id?: string }) => void
  resumeWith: (r: Row) => void // reprendre en envoyant un message
  resumeSend: (r: Row, text: string) => Promise<boolean> // reprendre une session fermée avec ce texte, sans fenêtre de saisie
  caps: (s: Session) => Caps // ce que sait faire le fournisseur de cette session
  snippets: Snippet[] // commandes personnelles et de projets, à insérer dans un prompt
  defaultChoice: Choice // modèle et effort des réglages de l'interface
  startSession: (project: string, prompt: string, choice: Choice) => Promise<boolean> // lance tout de suite, sans fenêtre intermédiaire
  newSession: (project: string, initial?: string, onSent?: () => void) => void // nouvelle session dans ce dossier de travail
  copy: (s: Session) => void
  rename: (s: Session) => void
  exportMd: (r: Row) => void
  move: (rows: Row[]) => void
  moveGroup: (g: Group, items: Item[]) => void
  group: (ids: string[], project: string, name: string | null) => void // null : nouveau groupe
  ungroup: (ids: string[], vg?: VGroup) => void // vg : par défaut le groupe affiché
  // after : appelé quand la suppression est confirmée (ex. fermer le lecteur de la session supprimée)
  remove: (items: Item[], what: string, after?: () => void) => void
  pickDir: (title: string, start: string) => Promise<string | null> // explorateur de dossiers
  stop: (ids: string[], onDone?: () => void) => void // onDone : après fermeture réussie ; ferme les sessions ouvertes (SIGTERM) puis met la liste à jour
}

export const ActionsCtx = createContext<Actions | null>(null)

export function useActions(): Actions {
  const a = useContext(ActionsCtx)
  if (!a) throw new Error("ActionsCtx manquant")
  return a
}
