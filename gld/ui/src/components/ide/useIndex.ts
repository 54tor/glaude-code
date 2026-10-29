import { useCallback, useEffect, useState } from "react"
import { api } from "@/lib/api"

export type FsIndexEntry = { path: string; size: number; mtime: number }
export type IndexState = "building" | "ready" | "partial"

// Le serveur répond « building » (liste vide) tant qu'il construit : on garde la dernière liste connue et on resonde.
export function useIndex(dir: string) {
  const [ix, setIx] = useState<{ files: FsIndexEntry[]; state: IndexState }>({ files: [], state: "building" })
  const [tick, setTick] = useState(0)
  useEffect(() => {
    let off = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const poll = async () => {
      const r = await api.fsIndex(dir).catch(() => null)
      if (off) return
      const s = r?.state
      if (r && (s === "ready" || s === "partial")) return setIx({ files: r.files ?? [], state: s })
      setIx((p) => (p.state === "building" ? p : { ...p, state: "building" }))
      timer = setTimeout(poll, r ? 1000 : 5000)
    }
    poll()
    return () => {
      off = true
      clearTimeout(timer)
    }
  }, [dir, tick])
  const refetch = useCallback(() => setTick((n) => n + 1), [])
  return { ...ix, refetch }
}
