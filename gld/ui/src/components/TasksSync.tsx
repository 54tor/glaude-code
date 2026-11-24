import { useEffect } from "react"
import { api } from "@/lib/api"
import { clearTasks, publishTasks } from "@/lib/tasksStore"

const TASKS_REFRESH_MS = 5000

export type LiveSessionRef = { id: string; dir: string }

// Publie les tâches de fond de toutes les sessions actives vers la barre d'état, quelle que soit la page affichée.
export function TasksSync({ sessions }: { sessions: LiveSessionRef[] }) {
  const signature = sessions.map((s) => `${s.id}\0${s.dir}`).join("\n")
  useEffect(() => {
    const refs: LiveSessionRef[] = signature
      ? signature.split("\n").map((entry) => {
          const [id, dir] = entry.split("\0")
          return { id, dir }
        })
      : []
    let isStillMounted = true
    const load = () =>
      refs.forEach(({ id, dir }) =>
        api.sessionLive(dir, id).then(
          (liveState) =>
            isStillMounted && publishTasks(id, dir, liveState.tasks ?? []),
          () => {}
        )
      )
    load()
    const refreshInterval = setInterval(
      () => document.visibilityState === "visible" && load(),
      TASKS_REFRESH_MS
    )
    return () => {
      isStillMounted = false
      clearInterval(refreshInterval)
      refs.forEach(({ id }) => clearTasks(id))
    }
  }, [signature])
  return null
}
