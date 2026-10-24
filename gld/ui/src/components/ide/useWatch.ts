import { useEffect, useRef, useState } from "react"

export type WatchEvent = { path: string; type: "create" | "write" | "remove" | "rename" | "resync" }

const MAX_DIRS = 200
const MAX_QUERY = 6000
const depth = (p: string) => (p ? p.split("/").length : 0)
// Au-delà du plafond du serveur, les dossiers les moins profonds d'abord : ce sont les plus visibles.
export const capDirs = (dirs: string[]) => [...new Set(dirs)].sort((a, b) => depth(a) - depth(b) || (a < b ? -1 : a > b ? 1 : 0)).slice(0, MAX_DIRS)

export type WatchState = { connected: boolean; state: "open" | "connecting" | "waiting" | "browserRetry"; retryIn: number }
const BACKOFF = [5, 15, 30]
const CONNECTING: WatchState = { connected: false, state: "connecting", retryIn: 0 }

// Toute réouverture après la première (reconnexion d'EventSource ou nouveau flux après un changement de dossiers) vaut resync :
// des évènements ont pu se perdre entre les deux.
// Fermé définitivement (réponse non 200, session expirée), l'EventSource ne réessaie plus : on rouvre nous-mêmes après 5 s, 15 s, puis 30 s.
// state : « waiting » pendant notre propre attente avant de rouvrir (retryIn en secondes),
// « browserRetry » quand le flux est coupé et que le navigateur réessaie seul.
export function useWatch(dir: string, declaredDirs: string[], onEvent: (e: WatchEvent) => void): WatchState {
  const [w, setW] = useState<WatchState>(CONNECTING)
  const cb = useRef(onEvent)
  const opened = useRef(false)
  useEffect(() => {
    cb.current = onEvent
  })
  // Un paramètre par dossier : une virgule dans un nom ne coupe rien. Budget d'URL : les plus profonds tombent d'abord.
  let query = `?dir=${encodeURIComponent(dir)}`
  for (const d of capDirs(declaredDirs)) {
    const part = `&paths=${encodeURIComponent(d)}`
    if (query.length + part.length > MAX_QUERY) break
    query += part
  }
  useEffect(() => {
    if (typeof EventSource === "undefined") return
    let es: EventSource | undefined
    let timer: ReturnType<typeof setTimeout> | undefined
    let tries = 0
    const connect = (ms: number) => {
      timer = setTimeout(() => {
        const s = new EventSource(`/fs-watch${query}`)
        es = s
        s.onopen = () => {
          tries = 0
          setW({ connected: true, state: "open", retryIn: 0 })
          if (opened.current) cb.current({ path: "", type: "resync" })
          opened.current = true
        }
        s.onerror = () => {
          if (s.readyState !== 2) return setW({ connected: false, state: "browserRetry", retryIn: 0 })
          s.close()
          es = undefined
          const wait = BACKOFF[Math.min(tries++, BACKOFF.length - 1)]
          setW({ connected: false, state: "waiting", retryIn: wait })
          connect(wait * 1000)
        }
        s.onmessage = (m) => {
          try {
            const e: unknown = JSON.parse(m.data)
            if (e && typeof e === "object" && typeof (e as WatchEvent).path === "string" && typeof (e as WatchEvent).type === "string") cb.current(e as WatchEvent)
          } catch {
            return
          }
        }
      }, ms)
    }
    connect(300)
    return () => {
      clearTimeout(timer)
      es?.close()
      setW(CONNECTING)
    }
  }, [query])
  return w
}
