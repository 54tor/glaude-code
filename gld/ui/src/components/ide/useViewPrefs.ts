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

const clampFont = (size: number) => Math.min(FONT_MAX, Math.max(FONT_MIN, size))
export const zoomed = (current: number, delta: 1 | -1 | 0) =>
  delta === 0 ? DEFAULT_PREFS.fontSize : clampFont(current + delta)

const parseBool = (stored: string, fallback: boolean) => {
  if (stored === "1") return true
  if (stored === "0") return false
  return fallback
}
const parseFont = (stored: string) =>
  /^\d+$/.test(stored) ? clampFont(Number(stored)) : DEFAULT_PREFS.fontSize

const serialize = (value: boolean | number) => {
  if (typeof value === "number") return String(clampFont(Math.round(value)))
  return value ? "1" : "0"
}

// Préférences globales (pas par dossier), une clé par valeur : une valeur illisible ne fait perdre qu'elle-même.
export function useViewPrefs(): ViewPrefs {
  const storageKey = (name: keyof ViewPrefValues) => `gld.ide.view.${name}`
  const stored = {
    wordWrap: useLocal(storageKey("wordWrap"), ""),
    minimap: useLocal(storageKey("minimap"), ""),
    fontSize: useLocal(storageKey("fontSize"), ""),
    showTree: useLocal(storageKey("showTree"), ""),
    showTerminal: useLocal(storageKey("showTerminal"), ""),
    showTerminalChosen: useLocal(storageKey("showTerminalChosen"), ""),
    showStatus: useLocal(storageKey("showStatus"), ""),
  }
  return {
    wordWrap: parseBool(stored.wordWrap[0], DEFAULT_PREFS.wordWrap),
    minimap: parseBool(stored.minimap[0], DEFAULT_PREFS.minimap),
    fontSize: parseFont(stored.fontSize[0]),
    showTree: parseBool(stored.showTree[0], DEFAULT_PREFS.showTree),
    showTerminal: parseBool(stored.showTerminal[0], DEFAULT_PREFS.showTerminal),
    showTerminalChosen: parseBool(stored.showTerminalChosen[0], DEFAULT_PREFS.showTerminalChosen),
    showStatus: parseBool(stored.showStatus[0], DEFAULT_PREFS.showStatus),
    set(name, value) {
      const setStored = stored[name][1]
      setStored(serialize(value))
      if (name === "showTerminal") stored.showTerminalChosen[1]("1")
    },
  }
}
