import { useEffect, useState } from "react"
import { api, type Data } from "@/lib/api"
import { useAuth, useCan } from "@/lib/auth"

export type Session = Data["groups"][number]["sessions"][number]
export type TermState = "loading" | "disabled" | "forbidden" | "nosession" | "ready"

const bare = (p: string) => p.replace(/[/\\]+$/, "")

// Sans le droit exec, ni /term-info ni sondage : l'état est « forbidden » sans aucun appel.
// useCan répond oui tant que l'authentification charge : on attend que les droits soient connus avant tout appel.
export function useTerminalAvailability(dir: string): { state: TermState; sessions: Session[] } {
  const auth = useAuth().state
  const known = auth === "ok" || auth === "open"
  const can = useCan()("exec")
  const exec = known && can
  const [on, setOn] = useState<boolean | null>(null)
  const [data, setData] = useState<Data | null>(null)
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
    if (!exec || !on) return
    let alive = true
    // Onglet en arrière-plan : aucun sondage, une relecture dès qu'il redevient visible.
    const load = () => {
      if (document.hidden) return
      api.data().then(
        (d) => alive && setData(d),
        () => {}
      )
    }
    load()
    const timer = setInterval(load, 10000)
    document.addEventListener("visibilitychange", load)
    return () => {
      alive = false
      clearInterval(timer)
      document.removeEventListener("visibilitychange", load)
    }
  }, [exec, on])
  const live = new Set(data?.live ?? [])
  const sessions = (data?.groups ?? [])
    .flatMap((g) => g.sessions)
    .filter((s) => bare(s.cwd) === bare(dir) && live.has(s.id))
    .sort((a, b) => b.mtime - a.mtime)
  const state: TermState = !known ? "loading" : !can ? "forbidden" : on === null ? "loading" : !on ? "disabled" : !data ? "loading" : sessions.length ? "ready" : "nosession"
  return { state, sessions }
}
