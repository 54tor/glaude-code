import { useEffect, useSyncExternalStore } from "react"
import { api, HttpError, type ShareInfo } from "@/lib/api"

// Partages de l'hôte (GET /shares), lus par un seul sondage commun : 10 s onglet visible, 5 s tant qu'un overlay est ouvert.
type State = { shares: ShareInfo[] | null; error: boolean }
let state: State = { shares: null, error: false }
const subs = new Set<() => void>()
let users = 0
let fasts = 0
let timer: ReturnType<typeof setInterval> | undefined

const set = (next: State) => {
  if (JSON.stringify(next) === JSON.stringify(state)) return
  state = next
  subs.forEach((f) => f())
}

// seq : seule la dernière lecture lancée compte, un sondage lent et ancien ne ressuscite pas une invitation révoquée.
let seq = 0
const load = () => {
  const n = ++seq
  return api.shares().then(
    (r) => void (n === seq && set({ shares: r.shares ?? [], error: false })),
    (e) => void (n === seq && set(e instanceof HttpError && (e.status === 403 || e.status === 404) ? { shares: [], error: false } : { shares: state.shares, error: true }))
  )
}
const onVisible = () => document.visibilityState === "visible" && void load()

const arm = () => {
  clearInterval(timer)
  timer = users ? setInterval(() => document.visibilityState === "visible" && void load(), fasts ? 5000 : 10000) : undefined
}

const subscribe = (f: () => void) => {
  subs.add(f)
  return () => void subs.delete(f)
}

export function resetShares() {
  state = { shares: null, error: false }
  users = fasts = 0
  seq = 0
  document.removeEventListener("visibilitychange", onVisible)
  clearInterval(timer)
  timer = undefined
}

export function useShares(fast = false) {
  const s = useSyncExternalStore(subscribe, () => state)
  useEffect(() => {
    users++
    if (fast) fasts++
    if (users === 1) {
      void load()
      document.addEventListener("visibilitychange", onVisible)
    }
    arm()
    return () => {
      users--
      if (fast) fasts--
      if (!users) document.removeEventListener("visibilitychange", onVisible)
      arm()
    }
  }, [fast])
  return { shares: s.shares, error: s.error, refresh: load }
}
