import { useEffect, useRef, useState } from "react"

export type WatchEvent = { path: string; type: "create" | "write" | "remove" | "rename" | "resync" }

const MAX_DIRS = 200
const MAX_QUERY = 6000
const depth = (p: string) => (p ? p.split("/").length : 0)
// Au-delà du plafond du serveur, les dossiers les moins profonds d'abord : ce sont les plus visibles.
export const capDirs = (dirs: string[]) => [...new Set(dirs)].sort((a, b) => depth(a) - depth(b) || (a < b ? -1 : a > b ? 1 : 0)).slice(0, MAX_DIRS)

// Toute réouverture après la première (reconnexion automatique d'EventSource) vaut resync : des évènements ont pu se perdre.
export function useWatch(dir: string, declaredDirs: string[], onEvent: (e: WatchEvent) => void) {
  const [connected, setConnected] = useState(false)
  const cb = useRef(onEvent)
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
    const timer = setTimeout(() => {
      let opened = false
      es = new EventSource(`/fs-watch${query}`)
      es.onopen = () => {
        setConnected(true)
        if (opened) cb.current({ path: "", type: "resync" })
        opened = true
      }
      es.onerror = () => setConnected(false)
      es.onmessage = (m) => {
        try {
          const e: unknown = JSON.parse(m.data)
          if (e && typeof e === "object" && typeof (e as WatchEvent).path === "string" && typeof (e as WatchEvent).type === "string") cb.current(e as WatchEvent)
        } catch {
          return
        }
      }
    }, 300)
    return () => {
      clearTimeout(timer)
      es?.close()
      setConnected(false)
    }
  }, [query])
  return { connected }
}
