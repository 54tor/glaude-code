import { useEffect, useRef } from "react"
import { useLeaveGuard } from "@/hooks/useEditorDraft"
import { isDirty, type Tab } from "./ideState"

// Brouillons des onglets sales, en sessionStorage seulement : un .env ou une clé ouverts ne doivent pas survivre à l'onglet du navigateur.
export type Draft = { content: string; baseEtag: string }
const MAX = 2 * 1024 * 1024
const key = (dir: string) => `gld.ide.${dir}.drafts`

export function readDrafts(dir: string): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  try {
    const v: unknown = JSON.parse(sessionStorage.getItem(key(dir)) ?? "{}")
    if (!v || typeof v !== "object" || Array.isArray(v)) return out
    for (const [p, d] of Object.entries(v as Record<string, Partial<Draft> | null>))
      if (d && typeof d.content === "string" && typeof d.baseEtag === "string") out[p] = { content: d.content, baseEtag: d.baseEtag }
  } catch {
    /* stockage illisible ou indisponible : aucun brouillon */
  }
  return out
}

// Au-delà du plafond, le brouillon de trop est sauté : la garde de sortie reste le filet.
export function writeDrafts(dir: string, tabs: Tab[]) {
  const out: Record<string, Draft> = {}
  let total = 0
  for (const t of tabs) {
    if (t.readOnly || !isDirty(t) || total + t.content.length > MAX) continue
    total += t.content.length
    out[t.path] = { content: t.content, baseEtag: t.etag }
  }
  try {
    if (Object.keys(out).length) sessionStorage.setItem(key(dir), JSON.stringify(out))
    else sessionStorage.removeItem(key(dir))
  } catch {
    /* stockage plein ou indisponible : la garde de sortie reste */
  }
}

// ready : tant que la restauration n'a pas posé les onglets, écrire effacerait les brouillons à réappliquer.
export function useDrafts(dir: string, tabs: Tab[], ready: boolean) {
  const cur = useRef(tabs)
  useEffect(() => {
    cur.current = tabs
  })
  useEffect(() => {
    if (!ready) return
    const timer = setTimeout(() => writeDrafts(dir, tabs), 500)
    return () => clearTimeout(timer)
  }, [dir, tabs, ready])
  useEffect(() => {
    if (!ready) return
    const flush = () => writeDrafts(dir, cur.current)
    addEventListener("pagehide", flush)
    return () => {
      removeEventListener("pagehide", flush)
      flush()
    }
  }, [dir, ready])
  useLeaveGuard(tabs.some(isDirty))
}
