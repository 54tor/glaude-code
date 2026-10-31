import { useEffect, useState } from "react"
import { TriangleAlert } from "lucide-react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { useActions } from "./actions"

type Warn = Awaited<ReturnType<typeof api.cleanupWarning>>

const REFRESH_INTERVAL_MS = 10 * 60_000
const SECONDS_PER_HOUR = 3600
const MAX_LISTED_SESSIONS = 3

// Claude Code efface seul les transcripts plus vieux que `cleanupPeriodDays` : on prévient dès qu'il reste moins de 24 h.
export function CleanupWarning() {
  const { t } = useTranslation()
  const actions = useActions()
  const [warning, setWarning] = useState<(Warn & { at: number }) | null>(null) // at : heure du relevé, pour compter les heures restantes
  useEffect(() => {
    let isMounted = true
    const loadWarning = () => {
      if (document.visibilityState !== "visible") return
      api.cleanupWarning().then(
        (response) => {
          if (isMounted) setWarning({ ...response, at: Date.now() / 1000 })
        },
        () => {},
      )
    }
    loadWarning()
    const refreshTimer = setInterval(loadWarning, REFRESH_INTERVAL_MS)
    return () => {
      isMounted = false
      clearInterval(refreshTimer)
    }
  }, [])
  if (!warning || warning.sessions.length === 0) return null
  const describeTimeLeft = (expiresAt: number) => {
    const hoursLeft = Math.ceil((expiresAt - warning.at) / SECONDS_PER_HOUR)
    if (hoursLeft <= 0) return t("shell.cleanup.overdue")
    if (hoursLeft <= 1) return t("shell.cleanup.lessThanHour")
    return t("shell.cleanup.inHours", { h: hoursLeft })
  }
  const firstSession = warning.sessions[0]
  const sessionCount = warning.sessions.length
  return (
    <div role="alert" className="mx-3 mb-2 rounded-lg border border-warning/50 bg-warning/10 p-2.5 text-xs">
      <p className="flex items-start gap-2 font-medium">
        <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" aria-hidden />
        <span>
          {t("shell.cleanup.warn", { count: sessionCount, left: describeTimeLeft(firstSession.expires) })} {t("shell.cleanup.keeps", { count: warning.days })} (<code>cleanupPeriodDays</code>).
        </span>
      </p>
      <ul className="mt-1.5 space-y-0.5 pl-6">
        {warning.sessions.slice(0, MAX_LISTED_SESSIONS).map((session) => (
          <li key={session.id}>
            <button className="max-w-full truncate text-left text-primary underline underline-offset-2 hover:decoration-2" onClick={() => actions.read(session)} title={session.title}>
              {session.title}
            </button>{" "}
            <span className="text-muted-foreground">· {describeTimeLeft(session.expires)}</span>
          </li>
        ))}
        {sessionCount > MAX_LISTED_SESSIONS && (
          <li className="text-muted-foreground">
            {t("shell.cleanup.more", { count: sessionCount - MAX_LISTED_SESSIONS })}
          </li>
        )}
      </ul>
      <p className="mt-1.5 pl-6 text-muted-foreground">
        {t("shell.cleanup.hintPre")} <code>cleanupPeriodDays</code> {t("shell.cleanup.hintPost")}
      </p>
    </div>
  )
}
