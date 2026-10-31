import { useEffect, useSyncExternalStore } from "react"
import { api, type ShareInfo } from "@/lib/api"
import { ApiError } from "@/lib/apiError"

// Partages de l'hôte (GET /v1/shares), lus par un seul sondage commun : 10 s onglet visible, 5 s tant qu'un overlay est ouvert.
type State = { shares: ShareInfo[] | null; error: boolean }
const FAST_POLL_INTERVAL_MS = 5000
const SLOW_POLL_INTERVAL_MS = 10000
let state: State = { shares: null, error: false }
const subscribers = new Set<() => void>()
let activeUsers = 0
let fastUsers = 0
let pollTimer: ReturnType<typeof setInterval> | undefined

const setState = (nextState: State) => {
  if (JSON.stringify(nextState) === JSON.stringify(state)) return
  state = nextState
  subscribers.forEach((subscriber) => subscriber())
}

const stateAfterError = (error: unknown): State => {
  const sharingUnavailable = error instanceof ApiError && (error.status === 403 || error.status === 404)
  if (sharingUnavailable) return { shares: [], error: false }
  return { shares: state.shares, error: true }
}

// latestRequest : seule la dernière lecture lancée compte, un sondage lent et ancien ne ressuscite pas une invitation révoquée.
let latestRequest = 0
const load = () => {
  const requestNumber = ++latestRequest
  return api.shares().then(
    (response) => void (requestNumber === latestRequest && setState({ shares: response.shares ?? [], error: false })),
    (error) => void (requestNumber === latestRequest && setState(stateAfterError(error)))
  )
}
const onVisible = () => document.visibilityState === "visible" && void load()

const armPollTimer = () => {
  clearInterval(pollTimer)
  if (!activeUsers) {
    pollTimer = undefined
    return
  }
  const interval = fastUsers ? FAST_POLL_INTERVAL_MS : SLOW_POLL_INTERVAL_MS
  pollTimer = setInterval(() => document.visibilityState === "visible" && void load(), interval)
}

const subscribe = (subscriber: () => void) => {
  subscribers.add(subscriber)
  return () => void subscribers.delete(subscriber)
}

export function resetShares() {
  state = { shares: null, error: false }
  activeUsers = 0
  fastUsers = 0
  latestRequest = 0
  document.removeEventListener("visibilitychange", onVisible)
  clearInterval(pollTimer)
  pollTimer = undefined
}

export function useShares(fast = false) {
  const snapshot = useSyncExternalStore(subscribe, () => state)
  useEffect(() => {
    activeUsers++
    if (fast) fastUsers++
    if (activeUsers === 1) {
      void load()
      document.addEventListener("visibilitychange", onVisible)
    }
    armPollTimer()
    return () => {
      activeUsers--
      if (fast) fastUsers--
      if (!activeUsers) document.removeEventListener("visibilitychange", onVisible)
      armPollTimer()
    }
  }, [fast])
  return { shares: snapshot.shares, error: snapshot.error, refresh: load }
}
