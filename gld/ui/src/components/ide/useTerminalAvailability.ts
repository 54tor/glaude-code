import { useEffect, useState } from "react"
import { api, HttpError, type Data } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { useAuth, useCan } from "@/lib/auth"

export type Session = Data["groups"][number]["sessions"][number]
// nosessions : liste des sessions refusée (droit sessions_read) ; unavailable : liste injoignable et rien de lu encore.
export type TermState = "loading" | "disabled" | "forbidden" | "nosessions" | "unavailable" | "nosession" | "ready"

const bare = (p: string) => p.replace(/[/\\]+$/, "")

// Sans le droit exec, ni /term-info ni sondage : l'état est « forbidden » sans aucun appel.
// GET /api demande sessions_read : sans lui, aucune lecture ; après un 403, le sondage s'arrête (relu au retour sur l'onglet).
// useCan répond oui tant que l'authentification charge : on attend que les droits soient connus avant tout appel.
export function useTerminalAvailability(dir: string): { state: TermState; sessions: Session[] } {
  const auth = useAuth().state
  const known = auth === "ok" || auth === "open"
  const allowed = useCan()
  const can = allowed("exec")
  const exec = known && can
  const list = allowed("sessions_read")
  const [on, setOn] = useState<boolean | null>(null)
  const [data, setData] = useState<Data | null>(null)
  const [fail, setFail] = useState(0)
  useEffect(() => {
    if (!exec) return
    let alive = true
    api.termInfo().then(
      (r) => alive && setOn(r.enabled),
      () => alive && setOn(false)
    )
    return () => {
      alive = false
    }
  }, [exec])
  useEffect(() => {
    if (!exec || !on || !list) return
    let alive = true
    let timer: ReturnType<typeof setInterval> | undefined
    const start = () => void (timer ??= setInterval(load, 10000))
    const stop = () => (clearInterval(timer), (timer = undefined))
    // Onglet en arrière-plan : aucun sondage, une relecture dès qu'il redevient visible.
    function load() {
      if (document.hidden) return
      api.data().then(
        (d) => {
          if (!alive) return
          setData(d)
          setFail(0)
          start()
        },
        (e) => {
          if (!alive) return
          const st = e instanceof HttpError || e instanceof ApiError ? e.status : -1
          setFail(st)
          if (st === 403) stop()
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
  }, [exec, on, list])
  const live = new Set(data?.live ?? [])
  const sessions = (data?.groups ?? [])
    .flatMap((g) => g.sessions)
    .filter((s) => bare(s.cwd) === bare(dir) && live.has(s.id))
    .sort((a, b) => b.mtime - a.mtime)
  const state: TermState = !known
    ? "loading"
    : !can
      ? "forbidden"
      : on === null
        ? "loading"
        : !on
          ? "disabled"
          : !list || fail === 403
            ? "nosessions"
            : !data
              ? fail
                ? "unavailable"
                : "loading"
              : sessions.length
                ? "ready"
                : "nosession"
  return { state, sessions }
}
