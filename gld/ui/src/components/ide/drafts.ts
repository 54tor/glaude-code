import { useEffect, useRef } from "react"
import { useLeaveGuard } from "@/hooks/useEditorDraft"
import { getAuth } from "@/lib/auth"
import { isDirty, type Tab } from "./ideState"

// Brouillons des onglets sales, en sessionStorage seulement : un .env ou une clé ouverts ne doivent pas survivre à l'onglet du navigateur.
export type Draft = { content: string; baseEtag: string }
const MAX_DRAFTS_BYTES = 2 * 1024 * 1024
const DRAFT_WRITE_DELAY_MS = 500
const draftsKey = (dir: string) => `gld.ide.${dir}.drafts`

const isValidDraft = (candidate: Partial<Draft> | null): candidate is Draft => {
  if (!candidate) return false
  return typeof candidate.content === "string" && typeof candidate.baseEtag === "string"
}

export function readDrafts(dir: string): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  try {
    const parsed: unknown = JSON.parse(sessionStorage.getItem(draftsKey(dir)) ?? "{}")
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return out
    for (const [path, draft] of Object.entries(parsed as Record<string, Partial<Draft> | null>)) {
      if (isValidDraft(draft)) out[path] = { content: draft.content, baseEtag: draft.baseEtag }
    }
  } catch {
    /* stockage illisible ou indisponible : aucun brouillon */
  }
  return out
}

// Au-delà du plafond, le brouillon de trop est sauté : la garde de sortie reste le filet.
export function writeDrafts(dir: string, tabs: Tab[]) {
  // Après déconnexion, le démontage de l'IDE ne doit pas réécrire ce que signedOut vient d'effacer.
  const auth = getAuth()
  if (auth.state === "anon" && !auth.owner) return
  const out: Record<string, Draft> = {}
  let total = 0
  for (const tab of tabs) {
    const exceedsCap = total + tab.content.length > MAX_DRAFTS_BYTES
    if (tab.readOnly || !isDirty(tab) || exceedsCap) continue
    total += tab.content.length
    out[tab.path] = { content: tab.content, baseEtag: tab.etag }
  }
  try {
    if (Object.keys(out).length) sessionStorage.setItem(draftsKey(dir), JSON.stringify(out))
    else sessionStorage.removeItem(draftsKey(dir))
  } catch {
    /* stockage plein ou indisponible : la garde de sortie reste */
  }
}

// ready : tant que la restauration n'a pas posé les onglets, écrire effacerait les brouillons à réappliquer.
export function useDrafts(dir: string, tabs: Tab[], ready: boolean) {
  const latestTabs = useRef(tabs)
  useEffect(() => {
    latestTabs.current = tabs
  })
  useEffect(() => {
    if (!ready) return
    const timer = setTimeout(() => writeDrafts(dir, tabs), DRAFT_WRITE_DELAY_MS)
    return () => clearTimeout(timer)
  }, [dir, tabs, ready])
  useEffect(() => {
    if (!ready) return
    const flush = () => writeDrafts(dir, latestTabs.current)
    addEventListener("pagehide", flush)
    return () => {
      removeEventListener("pagehide", flush)
      flush()
    }
  }, [dir, ready])
  useLeaveGuard(tabs.some(isDirty))
}
