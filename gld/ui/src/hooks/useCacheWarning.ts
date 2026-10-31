import { useEffect, useRef } from "react"
import { toast } from "sonner"
import { i18n } from "@/i18n"
import { useKeepWarm } from "@/hooks/useKeepWarm"
import { HOT } from "@/lib/sessionsView"

const WARN_BELOW_SECONDS = 600
const TOAST_DURATION_MS = 15_000

// Un toast par session ouverte dont le cache de prompt va expirer ; ré-armé quand la session redevient active (mtime change).
export function useCacheWarning(rows: { id: string; title: string; mtime: number }[], now: number, open: (id: string) => void) {
  const warmStates = useKeepWarm()
  const alreadyWarned = useRef(new Set<string>())
  useEffect(() => {
    for (const row of rows) {
      const secondsLeft = HOT - (now - row.mtime)
      const warningKey = `${row.id}:${row.mtime}`
      const outsideWarningWindow = secondsLeft <= 0 || secondsLeft > WARN_BELOW_SECONDS
      const keptWarm = warmStates[row.id]?.on
      if (outsideWarningWindow || alreadyWarned.current.has(warningKey) || keptWarm) continue
      alreadyWarned.current.add(warningKey)
      const minutesLeft = Math.max(1, Math.ceil(secondsLeft / 60))
      toast.warning(i18n.t("shell.app.cacheExpiring", { title: row.title, left: minutesLeft }), {
        duration: TOAST_DURATION_MS,
        action: { label: i18n.t("shell.app.cacheOpen"), onClick: () => open(row.id) },
      })
    }
  }, [rows, now, warmStates, open])
}
