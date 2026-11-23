import type { SyntheticEvent } from "react"
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
import {
  toast,
  useNotifications,
  type Notification,
  type NotifyKind,
} from "@/lib/notify"

export const MAX_VISIBLE_CHIPS = 5

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
  notification,
  onOpen,
}: {
  notification: Notification
  onOpen: (id: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex max-w-[16rem] min-w-0 items-center rounded-md border bg-muted/40 text-xs">
      <button
        type="button"
        onClick={() => onOpen(notification.id)}
        className="flex min-w-0 items-center gap-1.5 rounded-l-md px-2 py-1 hover:bg-muted"
      >
        <KindIcon kind={notification.kind} />
        <span className="truncate">{notification.message}</span>
      </button>
      {notification.kind !== "loading" && (
        <button
          type="button"
          aria-label={t("shell.statusBar.dismiss")}
          onClick={() => toast.dismiss(notification.id)}
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

export function StatusBar({
  onOpen,
}: {
  onOpen: (notificationId: string | null) => void
}) {
  const { t } = useTranslation()
  const { visible, history } = useNotifications()
  const shown = visible.slice(-MAX_VISIBLE_CHIPS)
  const hiddenCount = visible.length - shown.length
  const unreadCount = history.filter((entry) => !entry.read).length
  return (
    <div
      role="region"
      aria-label={t("shell.statusBar.label")}
      onPointerDown={keepDialogsOpen}
      onFocus={keepDialogsOpen}
      className="status-bar pointer-events-auto fixed inset-x-0 bottom-0 z-[60] flex h-[var(--bar-h)] items-center gap-2 border-t bg-background px-2 pb-[env(safe-area-inset-bottom)]"
    >
      <div className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden">
        {shown.map((notification) => (
          <Chip
            key={notification.id}
            notification={notification}
            onOpen={onOpen}
          />
        ))}
        {hiddenCount > 0 && (
          <span className="shrink-0 text-xs text-muted-foreground">
            {t("shell.statusBar.moreHidden", { count: hiddenCount })}
          </span>
        )}
      </div>
      <button
        type="button"
        aria-label={t("shell.statusBar.history")}
        title={t("shell.statusBar.history")}
        onClick={() => onOpen(null)}
        className="flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
      >
        <Bell className="size-3.5" />
        {unreadCount > 0 && (
          <span
            aria-label={t("shell.statusBar.unread", { count: unreadCount })}
          >
            {unreadCount}
          </span>
        )}
      </button>
    </div>
  )
}
