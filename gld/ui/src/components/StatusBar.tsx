import { useEffect, useState, type ReactNode, type SyntheticEvent } from "react"
import { useTranslation } from "react-i18next"
import {
  Bell,
  CircleCheck,
  Fan,
  Info,
  OctagonX,
  TriangleAlert,
  X,
} from "lucide-react"
import { toast, useNotifications, type NotifyKind } from "@/lib/notify"
import { api } from "@/lib/api"
import { useTaskChips } from "@/lib/tasksStore"

export const MAX_VISIBLE_CHIPS = 5
export const TASK_PREFIX = "task:"

const KIND_STYLE: Record<NotifyKind, string> = {
  success: "text-success",
  error: "text-destructive",
  warning: "text-warning",
  info: "text-muted-foreground",
  loading: "text-success",
}

export function KindIcon({ kind }: { kind: NotifyKind }) {
  const className = `size-3.5 shrink-0 ${KIND_STYLE[kind]}`
  if (kind === "success") return <CircleCheck className={className} />
  if (kind === "error") return <OctagonX className={className} />
  if (kind === "warning") return <TriangleAlert className={className} />
  if (kind === "loading") return <Fan className={`${className} animate-spin`} />
  return <Info className={className} />
}

function Chip({
  icon,
  label,
  onOpen,
  onDismiss,
}: {
  icon: ReactNode
  label: ReactNode
  onOpen: () => void
  onDismiss?: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex max-w-[16rem] min-w-0 items-center rounded-md border bg-muted/40 text-xs">
      <button
        type="button"
        onClick={onOpen}
        className="flex min-w-0 items-center gap-1.5 rounded-l-md px-2 py-1 hover:bg-muted"
      >
        {icon}
        <span className="truncate">{label}</span>
      </button>
      {onDismiss && (
        <button
          type="button"
          aria-label={t("shell.statusBar.dismiss")}
          onClick={onDismiss}
          className="rounded-r-md px-1 py-1 text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <X className="size-3" />
        </button>
      )}
    </div>
  )
}

// Les dialogues Radix se ferment sur toute interaction hors d'eux : la barre, posée au-dessus, ne doit pas les fermer.
const keepDialogsOpen = (event: SyntheticEvent) => event.stopPropagation()

function VersionLabel() {
  const { t } = useTranslation()
  const [updateInfo, setUpdateInfo] =
    useState<Awaited<ReturnType<typeof api.updateInfo>>>()
  useEffect(() => {
    api.updateInfo().then(setUpdateInfo, () => {})
  }, [])
  if (!updateInfo) return null
  return (
    <div className="flex min-w-0 shrink items-center gap-2 text-xs text-muted-foreground tabular-nums">
      <span className="truncate" title={updateInfo.current}>
        {updateInfo.current}
      </span>
      {updateInfo.newer && (
        <a
          href={updateInfo.url}
          target="_blank"
          rel="noopener noreferrer"
          className="shrink-0 font-medium text-primary hover:underline"
        >
          {t("shell.sidebar.updateTo", { latest: updateInfo.latest })}
        </a>
      )}
    </div>
  )
}

export function StatusBar({
  onOpen,
}: {
  onOpen: (notificationId: string | null) => void
}) {
  const { t } = useTranslation()
  const { visible, history } = useNotifications()
  const taskChips = useTaskChips()
  const shownTasks = taskChips.slice(0, MAX_VISIBLE_CHIPS)
  const notificationSlots = MAX_VISIBLE_CHIPS - shownTasks.length
  const shown = notificationSlots > 0 ? visible.slice(-notificationSlots) : []
  const hiddenCount =
    taskChips.length + visible.length - shownTasks.length - shown.length
  const unreadCount = history.filter((entry) => !entry.read).length
  const historyLabel =
    unreadCount > 0
      ? `${t("shell.statusBar.history")}, ${t("shell.statusBar.unread", { count: unreadCount })}`
      : t("shell.statusBar.history")
  return (
    <div
      role="region"
      aria-label={t("shell.statusBar.label")}
      onPointerDown={keepDialogsOpen}
      onFocus={keepDialogsOpen}
      className="status-bar pointer-events-auto fixed inset-x-0 bottom-0 z-[60] flex h-[var(--bar-h)] items-center gap-2 border-t bg-background px-2 pb-[env(safe-area-inset-bottom)]"
    >
      <div
        aria-live="polite"
        className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden"
      >
        {shownTasks.map((chip) => (
          <Chip
            key={chip.key}
            icon={<KindIcon kind="loading" />}
            label={chip.task.desc || chip.task.id}
            onOpen={() => onOpen(`${TASK_PREFIX}${chip.key}`)}
          />
        ))}
        {shown.map((notification) => (
          <Chip
            key={notification.id}
            icon={<KindIcon kind={notification.kind} />}
            label={notification.message}
            onOpen={() => onOpen(notification.id)}
            onDismiss={
              notification.kind === "loading"
                ? undefined
                : () => toast.dismiss(notification.id)
            }
          />
        ))}
        {hiddenCount > 0 && (
          <span className="shrink-0 text-xs text-muted-foreground">
            {t("shell.statusBar.moreHidden", { count: hiddenCount })}
          </span>
        )}
      </div>
      <VersionLabel />
      <button
        type="button"
        aria-label={historyLabel}
        title={historyLabel}
        onClick={() => onOpen(null)}
        className="flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
      >
        <Bell className="size-3.5" />
        {unreadCount > 0 && <span aria-hidden>{unreadCount}</span>}
      </button>
    </div>
  )
}
