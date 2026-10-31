import { useEffect, useState } from "react"
import { api, type Data } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { useAuth, useCan } from "@/lib/auth"

export type Session = Data["groups"][number]["sessions"][number]
// nosessions : liste des sessions refusée (droit sessions_read) ; unavailable : liste injoignable et rien de lu encore.
export type TermState = "loading" | "disabled" | "forbidden" | "nosessions" | "unavailable" | "nosession" | "ready"

const POLL_INTERVAL_MS = 10000
const FORBIDDEN = 403
const bare = (path: string) => path.replace(/[/\\]+$/, "")

// Sans le droit exec, ni /v1/terminal ni sondage : l'état est « forbidden » sans aucun appel.
// GET /v1/state demande sessions_read : sans lui, aucune lecture ; après un 403, le sondage s'arrête (relu au retour sur l'onglet).
// useCan répond oui tant que l'authentification charge : on attend que les droits soient connus avant tout appel.
export function useTerminalAvailability(dir: string): { state: TermState; sessions: Session[] } {
  const auth = useAuth().state
  const known = auth === "ok" || auth === "open"
  const allowed = useCan()
  const can = allowed("exec")
  const exec = known && can
  const list = allowed("sessions_read")
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [data, setData] = useState<Data | null>(null)
  const [failedStatus, setFailedStatus] = useState(0)
  useEffect(() => {
    if (!exec) return
    let alive = true
    api.termInfo().then(
      (info) => alive && setEnabled(info.enabled),
      () => alive && setEnabled(false)
    )
    return () => {
      alive = false
    }
  }, [exec])
  useEffect(() => {
    if (!exec || !enabled || !list) return
    let alive = true
    let timer: ReturnType<typeof setInterval> | undefined
    const start = () => void (timer ??= setInterval(load, POLL_INTERVAL_MS))
    const stop = () => {
      clearInterval(timer)
      timer = undefined
    }
    // Onglet en arrière-plan : aucun sondage, une relecture dès qu'il redevient visible.
    function load() {
      if (document.hidden) return
      api.data().then(
        (received) => {
          if (!alive) return
          setData(received)
          setFailedStatus(0)
          start()
        },
        (error) => {
          if (!alive) return
          const status = error instanceof ApiError ? error.status : -1
          setFailedStatus(status)
          if (status === FORBIDDEN) stop()
        }
      )
    }
    load()
    start()
    document.addEventListener("visibilitychange", load)
    return () => {
      alive = false
      stop()
      document.removeEventListener("visibilitychange", load)
    }
  }, [exec, enabled, list])
  const live = new Set(data?.live ?? [])
  const sessions = (data?.groups ?? [])
    .flatMap((group) => group.sessions)
    .filter((session) => bare(session.cwd) === bare(dir) && live.has(session.id))
    .sort((left, right) => right.mtime - left.mtime)
  const resolveState = (): TermState => {
    if (!known) return "loading"
    if (!can) return "forbidden"
    if (enabled === null) return "loading"
    if (!enabled) return "disabled"
    if (!list || failedStatus === FORBIDDEN) return "nosessions"
    if (!data) return failedStatus ? "unavailable" : "loading"
    if (sessions.length) return "ready"
    return "nosession"
  }
  return { state: resolveState(), sessions }
}
