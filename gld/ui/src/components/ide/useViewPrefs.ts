import { useLocal } from "@/hooks/useLocal"

// showTerminalChosen : distingue « aucun choix encore » (ouverture d'emblée possible) d'un choix explicite de l'utilisateur.
export type ViewPrefValues = {
  wordWrap: boolean
  minimap: boolean
  fontSize: number
  showTree: boolean
  showTerminal: boolean
  showTerminalChosen: boolean
  showStatus: boolean
}
export type ViewPrefs = ViewPrefValues & {
  set<K extends keyof ViewPrefValues>(k: K, v: ViewPrefValues[K]): void
}

export const DEFAULT_PREFS: ViewPrefValues = {
  wordWrap: false,
  minimap: false,
  fontSize: 13,
  showTree: true,
  showTerminal: true,
  showTerminalChosen: false,
  showStatus: true,
}
const FONT_MIN = 10
const FONT_MAX = 28

const clampFont = (n: number) => Math.min(FONT_MAX, Math.max(FONT_MIN, n))
export const zoomed = (cur: number, delta: 1 | -1 | 0) =>
  delta === 0 ? DEFAULT_PREFS.fontSize : clampFont(cur + delta)

const bool = (s: string, d: boolean) =>
  s === "1" ? true : s === "0" ? false : d
const font = (s: string) =>
  /^\d+$/.test(s) ? clampFont(Number(s)) : DEFAULT_PREFS.fontSize

// Préférences globales (pas par dossier), une clé par valeur : une valeur illisible ne fait perdre qu'elle-même.
export function useViewPrefs(): ViewPrefs {
  const k = (n: keyof ViewPrefValues) => `gld.ide.view.${n}`
  const s = {
    wordWrap: useLocal(k("wordWrap"), ""),
    minimap: useLocal(k("minimap"), ""),
    fontSize: useLocal(k("fontSize"), ""),
    showTree: useLocal(k("showTree"), ""),
    showTerminal: useLocal(k("showTerminal"), ""),
    showTerminalChosen: useLocal(k("showTerminalChosen"), ""),
    showStatus: useLocal(k("showStatus"), ""),
  }
  return {
    wordWrap: bool(s.wordWrap[0], DEFAULT_PREFS.wordWrap),
    minimap: bool(s.minimap[0], DEFAULT_PREFS.minimap),
    fontSize: font(s.fontSize[0]),
    showTree: bool(s.showTree[0], DEFAULT_PREFS.showTree),
    showTerminal: bool(s.showTerminal[0], DEFAULT_PREFS.showTerminal),
    showTerminalChosen: bool(
      s.showTerminalChosen[0],
      DEFAULT_PREFS.showTerminalChosen
    ),
    showStatus: bool(s.showStatus[0], DEFAULT_PREFS.showStatus),
    set(name, v) {
      s[name][1](
        typeof v === "number" ? String(clampFont(Math.round(v))) : v ? "1" : "0"
      )
      if (name === "showTerminal") s.showTerminalChosen[1]("1")
    },
  }
}
