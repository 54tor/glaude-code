import { useEffect, useRef, useState } from "react"
import { v1 } from "@/lib/apiError"

export type WatchEvent = { path: string; type: "create" | "write" | "remove" | "rename" | "resync" }

const MAX_DIRS = 200
const MAX_QUERY = 6000
const CLOSED = 2
const FIRST_CONNECT_DELAY_MS = 300
const depth = (path: string) => (path ? path.split("/").length : 0)
const compareStrings = (left: string, right: string) => {
  if (left < right) return -1
  if (left > right) return 1
  return 0
}
const byDepthThenName = (left: string, right: string) => depth(left) - depth(right) || compareStrings(left, right)
// Au-delà du plafond du serveur, les dossiers les moins profonds d'abord : ce sont les plus visibles.
export const capDirs = (dirs: string[]) => [...new Set(dirs)].sort(byDepthThenName).slice(0, MAX_DIRS)

const isWatchEvent = (candidate: unknown): candidate is WatchEvent => {
  if (!candidate || typeof candidate !== "object") return false
  const event = candidate as WatchEvent
  return typeof event.path === "string" && typeof event.type === "string"
}

export type WatchState = { connected: boolean; state: "open" | "connecting" | "waiting" | "browserRetry"; retryIn: number }
const BACKOFF = [5, 15, 30]
const CONNECTING: WatchState = { connected: false, state: "connecting", retryIn: 0 }

// Toute réouverture après la première (reconnexion d'EventSource ou nouveau flux après un changement de dossiers) vaut resync :
// des évènements ont pu se perdre entre les deux.
// Fermé définitivement (réponse non 200, session expirée), l'EventSource ne réessaie plus : on rouvre nous-mêmes après 5 s, 15 s, puis 30 s.
// state : « waiting » pendant notre propre attente avant de rouvrir (retryIn en secondes),
// « browserRetry » quand le flux est coupé et que le navigateur réessaie seul.
export function useWatch(dir: string, declaredDirs: string[], onEvent: (event: WatchEvent) => void): WatchState {
  const [watchState, setWatchState] = useState<WatchState>(CONNECTING)
  const onEventRef = useRef(onEvent)
  const opened = useRef(false)
  useEffect(() => {
    onEventRef.current = onEvent
  })
  // Un paramètre par dossier : une virgule dans un nom ne coupe rien. Budget d'URL : les plus profonds tombent d'abord.
  let query = `?dir=${encodeURIComponent(dir)}`
  for (const declaredDir of capDirs(declaredDirs)) {
    const part = `&paths=${encodeURIComponent(declaredDir)}`
    if (query.length + part.length > MAX_QUERY) break
    query += part
  }
  useEffect(() => {
    if (typeof EventSource === "undefined") return
    let currentSource: EventSource | undefined
    let timer: ReturnType<typeof setTimeout> | undefined
    let tries = 0
    const connect = (delayMs: number) => {
      timer = setTimeout(() => {
        const source = new EventSource(v1(`/fs/watch${query}`))
        currentSource = source
        source.onopen = () => {
          tries = 0
          setWatchState({ connected: true, state: "open", retryIn: 0 })
          if (opened.current) onEventRef.current({ path: "", type: "resync" })
          opened.current = true
        }
        source.onerror = () => {
          if (source.readyState !== CLOSED) return setWatchState({ connected: false, state: "browserRetry", retryIn: 0 })
          source.close()
          currentSource = undefined
          const waitSeconds = BACKOFF[Math.min(tries++, BACKOFF.length - 1)]
          setWatchState({ connected: false, state: "waiting", retryIn: waitSeconds })
          connect(waitSeconds * 1000)
        }
        source.onmessage = (message) => {
          try {
            const parsed: unknown = JSON.parse(message.data)
            if (isWatchEvent(parsed)) onEventRef.current(parsed)
          } catch {
            return
          }
        }
      }, delayMs)
    }
    connect(FIRST_CONNECT_DELAY_MS)
    return () => {
      clearTimeout(timer)
      currentSource?.close()
      setWatchState(CONNECTING)
    }
  }, [query])
  return watchState
}
