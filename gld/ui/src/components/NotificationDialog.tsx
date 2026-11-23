import { useEffect } from "react"
import { useTranslation } from "react-i18next"
import { ArrowLeft } from "lucide-react"
import {
  clearHistory,
  markAllRead,
  toast,
  useNotifications,
  type Notification,
} from "@/lib/notify"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { useTaskChips, type TaskChip } from "@/lib/tasksStore"
import { stopTask } from "./LiveTasks"
import { KindIcon, TASK_PREFIX } from "./StatusBar"

type Target = string | null | undefined

const timeOf = (notification: Notification) =>
  new Date(notification.createdAt).toLocaleTimeString()

function Detail({
  notification,
  onBack,
  onDone,
}: {
  notification: Notification
  onBack: () => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-3">
      <Button variant="ghost" size="sm" onClick={onBack}>
        <ArrowLeft /> {t("shell.statusBar.back")}
      </Button>
      <div className="flex items-start gap-2">
        <KindIcon kind={notification.kind} />
        <div className="min-w-0 flex-1 space-y-2 break-words">
          <div className="font-medium">{notification.message}</div>
          {notification.description && (
            <div
              className="text-muted-foreground"
              onClick={(event) => {
                if ((event.target as Element).closest("button, a")) onDone()
              }}
            >
              {notification.description}
            </div>
          )}
          <div className="text-xs text-muted-foreground">
            {timeOf(notification)}
          </div>
          {notification.action && (
            <Button
              size="sm"
              onClick={(event) => {
                notification.action?.onClick(event)
                if (event.defaultPrevented) return
                toast.dismiss(notification.id)
                onDone()
              }}
            >
              {notification.action.label}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

function TaskDetail({ chip, onBack }: { chip: TaskChip; onBack: () => void }) {
  const { t } = useTranslation()
  const { task, sessionId } = chip
  return (
    <div className="space-y-3">
      <Button variant="ghost" size="sm" onClick={onBack}>
        <ArrowLeft /> {t("shell.statusBar.back")}
      </Button>
      <div className="flex items-start gap-2">
        <KindIcon kind="loading" />
        <div className="min-w-0 flex-1 space-y-2 break-words">
          <div className="font-medium">{task.desc || task.id}</div>
          <div className="text-xs text-muted-foreground">
            {t(`conversation.tasks.status.${task.status}`)}
          </div>
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              void stopTask(
                sessionId,
                task.id,
                task.desc,
                t("conversation.tasks.stopSent")
              )
            }
          >
            {t("conversation.tasks.stop")}
          </Button>
        </div>
      </div>
    </div>
  )
}

function HistoryList({
  history,
  onPick,
}: {
  history: Notification[]
  onPick: (id: string) => void
}) {
  const { t } = useTranslation()
  if (history.length === 0)
    return (
      <p className="py-6 text-center text-muted-foreground">
        {t("shell.statusBar.empty")}
      </p>
    )
  return (
    <div className="space-y-2">
      <ul className="max-h-[60dvh] space-y-1 overflow-y-auto">
        {[...history].reverse().map((notification) => (
          <li key={notification.id}>
            <button
              type="button"
              onClick={() => onPick(notification.id)}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-muted"
            >
              <KindIcon kind={notification.kind} />
              <span className="min-w-0 flex-1 truncate">
                {notification.message}
              </span>
              <span className="shrink-0 text-xs text-muted-foreground">
                {timeOf(notification)}
              </span>
            </button>
          </li>
        ))}
      </ul>
      <Button variant="outline" size="sm" onClick={clearHistory}>
        {t("shell.statusBar.clear")}
      </Button>
    </div>
  )
}

export function NotificationDialog({
  target,
  onTarget,
}: {
  target: Target
  onTarget: (target: Target) => void
}) {
  const { t } = useTranslation()
  const { history } = useNotifications()
  const isOpen = target !== undefined
  useEffect(() => {
    if (isOpen) markAllRead()
  }, [isOpen, history])
  const taskChips = useTaskChips()
  const isTask = typeof target === "string" && target.startsWith(TASK_PREFIX)
  const selectedTask = isTask
    ? taskChips.find((chip) => `${TASK_PREFIX}${chip.key}` === target)
    : undefined
  const selected =
    typeof target === "string" && !isTask
      ? history.find((notification) => notification.id === target)
      : undefined
  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onTarget(undefined)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {selected || selectedTask
              ? t("shell.statusBar.detailTitle")
              : t("shell.statusBar.historyTitle")}
          </DialogTitle>
          <DialogDescription className="sr-only">
            {t("shell.statusBar.history")}
          </DialogDescription>
        </DialogHeader>
        {selectedTask && (
          <TaskDetail chip={selectedTask} onBack={() => onTarget(null)} />
        )}
        {selected && (
          <Detail
            notification={selected}
            onBack={() => onTarget(null)}
            onDone={() => onTarget(undefined)}
          />
        )}
        {!selected && !selectedTask && (
          <HistoryList history={history} onPick={onTarget} />
        )}
      </DialogContent>
    </Dialog>
  )
}
