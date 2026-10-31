import { useSyncExternalStore } from "react"

// Monaco ramène des fins de ligne mixtes (ou un CR isolé) à la majoritaire : enregistrer réécrirait tout le fichier.
export const mixedEol = (content: string) => {
  const hasLoneCarriageReturn = /\r(?!\n)/.test(content)
  if (hasLoneCarriageReturn) return true
  const hasCrlf = content.includes("\r\n")
  const hasBareLf = /(^|[^\r])\n/.test(content)
  return hasCrlf && hasBareLf
}

// Le fournisseur de thème de gld pose la classe sur <html> : on la suit, « système » compris.
const watchTheme = (onThemeChange: () => void) => {
  const observer = new MutationObserver(onThemeChange)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] })
  return () => observer.disconnect()
}
const readTheme = () => (document.documentElement.classList.contains("dark") ? "dark" : "light")
export const useResolvedTheme = () => useSyncExternalStore(watchTheme, readTheme)

export const parentOf = (path: string) => path.slice(0, Math.max(0, path.lastIndexOf("/")))
export const nameOf = (path: string) => path.slice(path.lastIndexOf("/") + 1)

export const clamp = (value: number, lowest: number, highest: number) => Math.min(highest, Math.max(lowest, value))

// Onglets mémorisés : chemins seulement, jamais le contenu ; tout stockage illisible vaut liste vide.
export const MAX_TABS = 30
export const parseTabs = (serialized: string): { paths: string[]; active: string } => {
  try {
    const parsed: unknown = JSON.parse(serialized)
    const stored = (parsed && typeof parsed === "object" ? parsed : {}) as { paths?: unknown; active?: unknown }
    const paths = Array.isArray(stored.paths)
      ? stored.paths.filter((path): path is string => typeof path === "string").slice(0, MAX_TABS)
      : []
    const active = typeof stored.active === "string" ? stored.active : ""
    return { paths, active }
  } catch {
    return { paths: [], active: "" }
  }
}
