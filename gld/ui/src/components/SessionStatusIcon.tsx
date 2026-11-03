import { useTranslation } from "react-i18next"
import { AlarmClock, Coffee, Fan, Info, Microwave, Snowflake } from "lucide-react"
import { useKeepWarm } from "@/hooks/useKeepWarm"
import { useActions } from "./actions"
import { CACHE_EXPIRY_WARNING_MINUTES } from "./KeepWarm"

const SECONDS_PER_MINUTE = 60

export function SessionStatusIcon({ session, now, ttl }: { session: { id: string; mtime: number }; now: number; ttl: number }) {
  const { t } = useTranslation()
  const actions = useActions()
  const warm = useKeepWarm()
  const isCold = now - session.mtime >= ttl
  const minutesLeft = Math.max(1, Math.ceil((ttl - (now - session.mtime)) / SECONDS_PER_MINUTE))
  const isExpiringSoon = !isCold && minutesLeft <= CACHE_EXPIRY_WARNING_MINUTES
  if (actions.asking.has(session.id)) return <Info className="size-4 shrink-0 animate-pulse text-warning" aria-label={t("shell.sidebar.asking")} />
  if (actions.live.has(session.id) && !actions.waiting.has(session.id)) return <Fan className="size-4 shrink-0 animate-spin text-success" aria-label={t("shell.sidebar.working")} />
  if (isExpiringSoon) return <AlarmClock className="size-4 shrink-0 text-warning" aria-label={t("shell.sidebar.cacheExpiring")} />
  if (warm[session.id]?.on) return <Microwave className="warm-wave size-4 shrink-0" aria-label={t("shell.sidebar.keepWarmOn")} />
  if (isCold) return <Snowflake className="size-4 shrink-0 text-info" aria-label={t("shell.sidebar.cold")} />
  return <Coffee className="size-4 shrink-0 text-warning" aria-hidden />
}
