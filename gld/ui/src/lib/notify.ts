import { useSyncExternalStore, type MouseEvent, type ReactNode } from "react"

export type NotifyKind = "success" | "error" | "info" | "warning" | "loading"

export type NotifyAction = {
  label: ReactNode
  onClick: (event: MouseEvent<HTMLButtonElement>) => void
}

export type NotifyOptions = {
  id?: string | number
  description?: ReactNode
  duration?: number
  action?: NotifyAction
  closeButton?: boolean
  onDismiss?: () => void
  onAutoClose?: () => void
}

export type Notification = {
  id: string
  kind: NotifyKind
  message: ReactNode
  description?: ReactNode
  action?: NotifyAction
  createdAt: number
  closed: boolean
  read: boolean
}

export type NotificationsSnapshot = {
  visible: Notification[]
  history: Notification[]
}

export const AUTO_CLOSE_MS = 5000
export const HISTORY_LIMIT = 100

const KINDS_WITHOUT_AUTO_CLOSE: NotifyKind[] = ["error", "loading"]

type Entry = { notification: Notification; options: NotifyOptions }

let entries: Entry[] = []
let snapshot: NotificationsSnapshot = { visible: [], history: [] }
let counter = 0
const timers = new Map<string, ReturnType<typeof setTimeout>>()
const listeners = new Set<() => void>()

function publish() {
  const history = entries.map((entry) => entry.notification)
  snapshot = { visible: history.filter((n) => !n.closed), history }
  listeners.forEach((listener) => listener())
}

function clearTimer(id: string) {
  const timer = timers.get(id)
  if (timer === undefined) return
  clearTimeout(timer)
  timers.delete(id)
}

function autoCloseDelay(kind: NotifyKind, options: NotifyOptions) {
  if (options.duration !== undefined) return options.duration
  return KINDS_WITHOUT_AUTO_CLOSE.includes(kind) ? Infinity : AUTO_CLOSE_MS
}

function closeEntry(id: string) {
  const entry = entries.find((candidate) => candidate.notification.id === id)
  if (!entry || entry.notification.closed) return undefined
  clearTimer(id)
  entry.notification = { ...entry.notification, closed: true }
  return entry
}

function scheduleAutoClose(
  id: string,
  kind: NotifyKind,
  options: NotifyOptions
) {
  const delay = autoCloseDelay(kind, options)
  if (!Number.isFinite(delay)) return
  timers.set(
    id,
    setTimeout(() => {
      const entry = closeEntry(id)
      if (!entry) return
      publish()
      entry.options.onAutoClose?.()
    }, delay)
  )
}

function trimHistory() {
  if (entries.length <= HISTORY_LIMIT) return
  const evicted = entries.slice(0, entries.length - HISTORY_LIMIT)
  evicted.forEach((entry) => clearTimer(entry.notification.id))
  entries = entries.slice(-HISTORY_LIMIT)
}

function push(
  kind: NotifyKind,
  message: ReactNode,
  options: NotifyOptions = {}
) {
  counter += 1
  const id = String(options.id ?? `n${counter}`)
  clearTimer(id)
  const notification: Notification = {
    id,
    kind,
    message,
    description: options.description,
    action: options.action,
    createdAt: Date.now(),
    closed: false,
    read: false,
  }
  const existing = entries.find((entry) => entry.notification.id === id)
  if (existing) {
    existing.notification = notification
    existing.options = options
  } else {
    entries.push({ notification, options })
    trimHistory()
  }
  scheduleAutoClose(id, kind, options)
  publish()
  return id
}

function dismiss(id?: string | number) {
  const ids =
    id === undefined
      ? entries.map((entry) => entry.notification.id)
      : [String(id)]
  const closed = ids.map(closeEntry).filter((entry) => entry !== undefined)
  if (closed.length === 0) return
  publish()
  closed.forEach((entry) => entry.options.onDismiss?.())
}

export const toast = Object.assign(
  (message: ReactNode, options?: NotifyOptions) =>
    push("info", message, options),
  {
    success: (message: ReactNode, options?: NotifyOptions) =>
      push("success", message, options),
    error: (message: ReactNode, options?: NotifyOptions) =>
      push("error", message, options),
    info: (message: ReactNode, options?: NotifyOptions) =>
      push("info", message, options),
    warning: (message: ReactNode, options?: NotifyOptions) =>
      push("warning", message, options),
    loading: (message: ReactNode, options?: NotifyOptions) =>
      push("loading", message, options),
    dismiss,
  }
)

export function getNotifications(): NotificationsSnapshot {
  return snapshot
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => void listeners.delete(listener)
}

export function useNotifications() {
  return useSyncExternalStore(subscribe, getNotifications)
}

export function markAllRead() {
  if (entries.every((entry) => entry.notification.read)) return
  entries.forEach((entry) => {
    entry.notification = { ...entry.notification, read: true }
  })
  publish()
}

export function clearHistory() {
  entries = entries.filter((entry) => !entry.notification.closed)
  publish()
}

export function resetNotifyForTests() {
  timers.forEach((timer) => clearTimeout(timer))
  timers.clear()
  entries = []
  counter = 0
  publish()
}
