import { useSyncExternalStore } from "react"
import type { BgTask } from "./api"

export type TaskChip = {
  key: string
  sessionId: string
  dir: string
  task: BgTask
}

const RUNNING = "running"

const bySession = new Map<string, { dir: string; tasks: BgTask[] }>()
const listeners = new Set<() => void>()
let chips: TaskChip[] = []

const buildChips = () =>
  [...bySession].flatMap(([sessionId, { dir, tasks }]) =>
    tasks
      .filter((task) => task.status === RUNNING)
      .map((task) => ({ key: `${sessionId}:${task.id}`, sessionId, dir, task }))
  )

function refresh() {
  const next = buildChips()
  const unchanged = JSON.stringify(next) === JSON.stringify(chips)
  if (unchanged) return
  chips = next
  listeners.forEach((listener) => listener())
}

export function publishTasks(sessionId: string, dir: string, tasks: BgTask[]) {
  bySession.set(sessionId, { dir, tasks })
  refresh()
}

export function clearTasks(sessionId: string) {
  if (bySession.delete(sessionId)) refresh()
}

export function resetTasks() {
  bySession.clear()
  refresh()
}

export const getTaskChips = () => chips

const subscribe = (listener: () => void) => {
  listeners.add(listener)
  return () => void listeners.delete(listener)
}

export const useTaskChips = () => useSyncExternalStore(subscribe, getTaskChips)
