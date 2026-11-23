import { useEffect, useRef } from "react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { toast } from "@/lib/notify"
import { useActions } from "./actions"

type Warn = Awaited<ReturnType<typeof api.cleanupWarning>>

const NOTIFICATION_ID = "cleanup-warning"
const REFRESH_INTERVAL_MS = 10 * 60_000
const SECONDS_PER_HOUR = 3600
const MAX_LISTED_SESSIONS = 3

// Claude Code efface seul les transcripts plus vieux que `cleanupPeriodDays` : on prévient dès qu'il reste moins de 24 h.
export function CleanupWarning() {
  const { t } = useTranslation()
  const actions = useActions()
  const actionsRef = useRef(actions)
  useEffect(() => {
    actionsRef.current = actions
  }, [actions])
  useEffect(() => {
    let isMounted = true
    let lastKey = ""
    const describeTimeLeft = (expiresAt: number, at: number) => {
      const hoursLeft = Math.ceil((expiresAt - at) / SECONDS_PER_HOUR)
      if (hoursLeft <= 0) return t("shell.cleanup.overdue")
      if (hoursLeft <= 1) return t("shell.cleanup.lessThanHour")
      return t("shell.cleanup.inHours", { h: hoursLeft })
    }
    const publish = (warning: Warn) => {
      const at = Date.now() / 1000
      const sessionCount = warning.sessions.length
      const listed = warning.sessions.slice(0, MAX_LISTED_SESSIONS)
      toast.warning(
        t("shell.cleanup.warn", {
          count: sessionCount,
          left: describeTimeLeft(warning.sessions[0].expires, at),
        }),
        {
          id: NOTIFICATION_ID,
          duration: Infinity,
          description: (
            <div className="space-y-1.5">
              <p>
                {t("shell.cleanup.keeps", { count: warning.days })} (
                <code>cleanupPeriodDays</code>).
              </p>
              <ul className="space-y-0.5">
                {listed.map((session) => (
                  <li key={session.id}>
                    <button
                      className="max-w-full truncate text-left text-primary underline underline-offset-2 hover:decoration-2"
                      onClick={() => actionsRef.current.read(session)}
                      title={session.title}
                    >
                      {session.title}
                    </button>{" "}
                    · {describeTimeLeft(session.expires, at)}
                  </li>
                ))}
                {sessionCount > MAX_LISTED_SESSIONS && (
                  <li>
                    {t("shell.cleanup.more", {
                      count: sessionCount - MAX_LISTED_SESSIONS,
                    })}
                  </li>
                )}
              </ul>
              <p>
                {t("shell.cleanup.hintPre")} <code>cleanupPeriodDays</code>{" "}
                {t("shell.cleanup.hintPost")}
              </p>
            </div>
          ),
        }
      )
    }
    const loadWarning = () => {
      if (document.visibilityState !== "visible") return
      api.cleanupWarning().then(
        (warning) => {
          if (!isMounted) return
          if (warning.sessions.length === 0) {
            lastKey = ""
            return void toast.dismiss(NOTIFICATION_ID)
          }
          const key = warning.sessions.map((session) => session.id).join(",")
          if (key === lastKey) return
          lastKey = key
          publish(warning)
        },
        () => {}
      )
    }
    loadWarning()
    const refreshTimer = setInterval(loadWarning, REFRESH_INTERVAL_MS)
    return () => {
      isMounted = false
      clearInterval(refreshTimer)
      toast.dismiss(NOTIFICATION_ID)
    }
  }, [t])
  return null
}
