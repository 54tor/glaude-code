import type { Data, Hit, Item, Session, VGroup } from "./api"
import { segs } from "./paths"
import type { Entry } from "./tree"
import type { Row } from "@/components/actions"

// Sessions lancées automatiquement (revues de sécurité) : leur titre est leur premier prompt. Elles sont masquées
// de l'arbre, comptées dans la barre latérale, et se suppriment d'un coup avec le bouton dédié.
export const AUTO = ["Review this change for security vulnerabilities", "You previously flagged these candidate vulnerabilities", "Perform a read-only security review of this codebase"]
export const isAuto = (session: Pick<Session, "title"> & { named?: boolean }) => {
  if (session.named) return false
  return AUTO.some((prefix) => session.title.startsWith(prefix))
}
export const WEEK = 7 * 86400
export const REFRESH = 15_000 // rafraîchissement de la liste, onglet visible seulement
export const HOT = 3600 // durée du cache des prompts : une session active depuis moins longtemps reprend à moindre coût

// Tri par dernier message de l'utilisateur (le mtime bouge aussi à chaque réponse de l'agent et réordonnerait la liste).
export const asked = (session: Pick<Session, "ask" | "mtime">) => session.ask ?? session.mtime
export const byDate = (left: Row, right: Row) => asked(right.s) - asked(left.s)
export const plural = (count: number, word = "session") => {
  const suffix = count > 1 ? "s" : ""
  return `${count} ${word}${suffix}`
}
export const cut = (text: string, maxLength: number) => {
  if (text.length <= maxLength) return text
  return text.slice(0, maxLength) + "…"
}
// Clé du dossier d'un groupe dans l'arbre et l'URL.
export const groupKey = (virtualGroup: VGroup, home: string) => segs(virtualGroup.project, home).join("/")

// Sessions affichées (hors revues automatiques, selon la recherche) et revues automatiques masquées.
export function buildView(data: Data | null, searchQuery: string, hitMap: Map<string, Hit> | null, advancedSearch = false) {
  const searchText = searchQuery.toLowerCase()
  const isSearching = searchText !== ""
  const entries: Entry[] = []
  const autoRows: Row[] = []
  let akb = 0
  let total = 0
  for (const group of data?.groups ?? []) {
    const projectMatches = advancedSearch && group.project.toLowerCase().includes(searchText)
    const visibleSessions: Session[] = []
    for (const session of group.sessions) {
      if (isAuto(session)) {
        autoRows.push({ s: session, g: group })
        akb += session.kb
      } else {
        visibleSessions.push(session)
      }
    }
    total += visibleSessions.length
    const matchesSearch = (session: Session) => {
      if (!isSearching) return true
      if (session.title.toLowerCase().includes(searchText)) return true
      if (!advancedSearch) return false
      return projectMatches || !!hitMap?.has(session.id)
    }
    const matchingSessions = visibleSessions.filter(matchesSearch)
    // Un dossier enregistré à la main (sans session) reste dans l'arbre, sauf si la recherche l'écarte.
    const isManualFolderKept = group.sessions.length === 0 && (!isSearching || projectMatches)
    if (matchingSessions.length > 0 || isManualFolderKept) entries.push({ g: group, ss: matchingSessions })
  }
  const rows = entries.flatMap(({ g: group, ss: sessions }) => sessions.map((session) => ({ s: session, g: group }))).sort(byDate)
  autoRows.sort(byDate)
  const autoItems: Item[] = autoRows.map(({ s: session }) => ({ dir: session.dir, id: session.id }))
  const byId = new Map(rows.map((row) => [row.s.id, row]))
  return { entries, rows, byId, autoRows, autoItems, akb, total }
}
export type View = ReturnType<typeof buildView>
