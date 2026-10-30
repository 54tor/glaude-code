import { useEffect, useRef } from "react"
import { toast } from "sonner"
import { i18n } from "@/i18n"
import { useKeepWarm } from "@/hooks/useKeepWarm"
import { HOT } from "@/lib/sessionsView"

const WARN = 600 // prévenir quand il reste moins de 10 minutes de cache

// Un toast par session ouverte dont le cache de prompt va expirer ; ré-armé quand la session redevient active (mtime change).
export function useCacheWarning(rows: { id: string; title: string; mtime: number }[], now: number, open: (id: string) => void) {
  const warm = useKeepWarm()
  const seen = useRef(new Set<string>())
  useEffect(() => {
    for (const r of rows) {
      const left = HOT - (now - r.mtime)
      const key = `${r.id}:${r.mtime}`
      if (left <= 0 || left > WARN || seen.current.has(key) || warm[r.id]?.on) continue
      seen.current.add(key)
      toast.warning(i18n.t("shell.app.cacheExpiring", { title: r.title, left: Math.max(1, Math.ceil(left / 60)) }), {
        duration: 15_000,
        action: { label: i18n.t("shell.app.cacheOpen"), onClick: () => open(r.id) },
      })
    }
  }, [rows, now, warm, open])
}
