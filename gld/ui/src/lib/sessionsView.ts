import type { Data, Hit, Item, Session, VGroup } from "./api"
import { segs } from "./paths"
import type { Entry } from "./tree"
import type { Row } from "@/components/actions"

// Sessions lancées automatiquement (revues de sécurité) : leur titre est leur premier prompt. Elles sont masquées
// de l'arbre, comptées dans la barre latérale, et se suppriment d'un coup avec le bouton dédié.
export const AUTO = ["Review this change for security vulnerabilities", "You previously flagged these candidate"]
export const WEEK = 7 * 86400
export const REFRESH = 15_000 // rafraîchissement de la liste, onglet visible seulement
export const HOT = 3600 // durée du cache des prompts : une session active depuis moins longtemps reprend à moindre coût

export const byDate = (a: Row, b: Row) => b.s.mtime - a.s.mtime
export const plural = (n: number, w = "session") => `${n} ${w}${n > 1 ? "s" : ""}`
export const cut = (t: string, n: number) => (t.length > n ? t.slice(0, n) + "…" : t)
// Clé du dossier d'un groupe dans l'arbre et l'URL.
export const groupKey = (vg: VGroup, home: string) => segs(vg.project, home).join("/")

// Sessions affichées (hors revues automatiques, selon la recherche) et revues automatiques masquées.
export function buildView(data: Data | null, qt: string, tags: Record<string, string[]>, hitMap: Map<string, Hit> | null) {
  const f = qt.toLowerCase()
  const entries: Entry[] = []
  const autoItems: Item[] = []
  let akb = 0
  let total = 0
  for (const g of data?.groups ?? []) {
    const vis: Session[] = []
    for (const s of g.sessions) {
      if (AUTO.some((p) => s.title.includes(p))) {
        autoItems.push({ dir: s.dir, id: s.id })
        akb += s.kb
      } else vis.push(s)
    }
    total += vis.length
    const tag = f.startsWith("#") ? f.slice(1) : null // « #prod » : sessions étiquetées
    const ss = vis.filter((s) =>
      tag !== null
        ? tags[s.id]?.some((t) => t.toLowerCase().includes(tag))
        : !f || g.project.toLowerCase().includes(f) || s.title.toLowerCase().includes(f) || hitMap?.has(s.id)
    )
    if (ss.length) entries.push({ g, ss })
  }
  const rows = entries.flatMap(({ g, ss }) => ss.map((s) => ({ s, g }))).sort(byDate)
  return { entries, rows, byId: new Map(rows.map((r) => [r.s.id, r])), autoItems, akb, total }
}
export type View = ReturnType<typeof buildView>
