import { useEffect, useState } from "react"
import { api, type Data } from "@/lib/api"
import { useCan } from "@/lib/auth"

export type Session = Data["groups"][number]["sessions"][number]
export type TermState = "loading" | "disabled" | "forbidden" | "nosession" | "ready"

const bare = (p: string) => p.replace(/[/\\]+$/, "")

// Sans le droit exec, ni /term-info ni sondage : l'état est « forbidden » sans aucun appel.
export function useTerminalAvailability(dir: string): { state: TermState; sessions: Session[] } {
  const exec = useCan()("exec")
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
    const load = () =>
      api.data().then(
        (d) => alive && setData(d),
        () => {}
      )
    load()
    const timer = setInterval(load, 10000)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [exec, on])
  const live = new Set(data?.live ?? [])
  const sessions = (data?.groups ?? [])
    .flatMap((g) => g.sessions)
    .filter((s) => bare(s.cwd) === bare(dir) && live.has(s.id))
    .sort((a, b) => b.mtime - a.mtime)
  const state: TermState = !exec ? "forbidden" : on === null ? "loading" : !on ? "disabled" : !data ? "loading" : sessions.length ? "ready" : "nosession"
  return { state, sessions }
}
