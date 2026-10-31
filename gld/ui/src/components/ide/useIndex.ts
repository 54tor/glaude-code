import { useCallback, useEffect, useState } from "react"
import { api } from "@/lib/api"

export type FsIndexEntry = { path: string; size: number; mtime: number }
export type IndexState = "building" | "ready" | "partial"

const POLL_BUILDING_MS = 1000
const POLL_FAILED_MS = 5000

// Le serveur répond « building » (liste vide) tant qu'il construit : on garde la dernière liste connue et on resonde.
export function useIndex(dir: string) {
  const [index, setIndex] = useState<{ files: FsIndexEntry[]; state: IndexState }>({ files: [], state: "building" })
  const [refetchCount, setRefetchCount] = useState(0)
  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const poll = async () => {
      const response = await api.fsIndex(dir).catch(() => null)
      if (cancelled) return
      const serverState = response?.state
      const isUsable = serverState === "ready" || serverState === "partial"
      if (response && isUsable) return setIndex({ files: response.files ?? [], state: serverState })
      setIndex((previous) => (previous.state === "building" ? previous : { ...previous, state: "building" }))
      timer = setTimeout(poll, response ? POLL_BUILDING_MS : POLL_FAILED_MS)
    }
    poll()
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [dir, refetchCount])
  const refetch = useCallback(() => setRefetchCount((count) => count + 1), [])
  return { ...index, refetch }
}
