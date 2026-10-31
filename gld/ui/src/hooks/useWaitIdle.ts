import { useSyncExternalStore } from "react"

const STORAGE_KEY = "gld.waitIdle"
const subscribers = new Set<() => void>()

const readIds = (): string[] => {
  try {
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "[]")
    return Array.isArray(stored) ? stored.filter((sessionId): sessionId is string => typeof sessionId === "string") : []
  } catch {
    return []
  }
}

export const setWaitIdle = (sessionId: string, wait: boolean) => {
  const others = readIds().filter((storedId) => storedId !== sessionId)
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(wait ? [...others, sessionId].slice(-200) : others))
  } catch {
    /* stockage indisponible : le choix vaut pour cet envoi seulement */
  }
  subscribers.forEach((subscriber) => subscriber())
}

export function useWaitIdle(sessionId: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      subscribers.add(onChange)
      return () => void subscribers.delete(onChange)
    },
    () => readIds().includes(sessionId),
  )
}
