import { useSyncExternalStore } from "react"

// Monaco ramène des fins de ligne mixtes (ou un CR isolé) à la majoritaire : enregistrer réécrirait tout le fichier.
export const mixedEol = (c: string) => /\r(?!\n)/.test(c) || (c.includes("\r\n") && /(^|[^\r])\n/.test(c))

// Le fournisseur de thème de gld pose la classe sur <html> : on la suit, « système » compris.
const watchTheme = (f: () => void) => {
  const o = new MutationObserver(f)
  o.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] })
  return () => o.disconnect()
}
export const useResolvedTheme = () => useSyncExternalStore(watchTheme, () => (document.documentElement.classList.contains("dark") ? "dark" : "light"))

export const parentOf = (p: string) => p.slice(0, Math.max(0, p.lastIndexOf("/")))
export const nameOf = (p: string) => p.slice(p.lastIndexOf("/") + 1)

export const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))

// Onglets mémorisés : chemins seulement, jamais le contenu ; tout stockage illisible vaut liste vide.
export const MAX_TABS = 30
export const parseTabs = (s: string): { paths: string[]; active: string } => {
  try {
    const v: unknown = JSON.parse(s)
    const o = (v && typeof v === "object" ? v : {}) as { paths?: unknown; active?: unknown }
    const paths = Array.isArray(o.paths) ? o.paths.filter((x): x is string => typeof x === "string").slice(0, MAX_TABS) : []
    return { paths, active: typeof o.active === "string" ? o.active : "" }
  } catch {
    return { paths: [], active: "" }
  }
}
