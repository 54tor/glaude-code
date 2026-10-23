import { useEffect, useState } from "react"
import { TriangleAlert } from "lucide-react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { useActions } from "./actions"

type Warn = Awaited<ReturnType<typeof api.cleanupWarning>>

// Claude Code efface seul les transcripts plus vieux que `cleanupPeriodDays` : on prévient dès qu'il reste moins de 24 h.
export function CleanupWarning() {
  const { t } = useTranslation()
  const a = useActions()
  const [w, setW] = useState<(Warn & { at: number }) | null>(null) // at : heure du relevé, pour compter les heures restantes
  useEffect(() => {
    let live = true
    const load = () => document.visibilityState === "visible" && api.cleanupWarning().then((r) => live && setW({ ...r, at: Date.now() / 1000 }), () => {})
    load()
    const iv = setInterval(load, 10 * 60_000)
    return () => {
      live = false
      clearInterval(iv)
    }
  }, [])
  if (!w || w.sessions.length === 0) return null
  const left = (e: number) => {
    const h = Math.ceil((e - w.at) / 3600)
    return h <= 0 ? t("shell.cleanup.overdue") : h <= 1 ? t("shell.cleanup.lessThanHour") : t("shell.cleanup.inHours", { h })
  }
  const first = w.sessions[0]
  const n = w.sessions.length
  return (
    <div role="alert" className="mx-3 mb-2 rounded-lg border border-amber-500/50 bg-amber-500/10 p-2.5 text-xs">
      <p className="flex items-start gap-2 font-medium">
        <TriangleAlert className="mt-0.5 size-4 shrink-0 text-amber-600" aria-hidden />
        <span>
          {t("shell.cleanup.warn", { count: n, left: left(first.expires) })} {t("shell.cleanup.keeps", { count: w.days })} (<code>cleanupPeriodDays</code>).
        </span>
      </p>
      <ul className="mt-1.5 space-y-0.5 pl-6">
        {w.sessions.slice(0, 3).map((s) => (
          <li key={s.id}>
            <button className="max-w-full truncate text-left underline-offset-2 hover:underline" onClick={() => a.read({ id: s.id, dir: s.dir } as never)} title={s.title}>
              {s.title}
            </button>{" "}
            <span className="text-muted-foreground">· {left(s.expires)}</span>
          </li>
        ))}
        {n > 3 && (
          <li className="text-muted-foreground">
            {t("shell.cleanup.more", { count: n - 3 })}
          </li>
        )}
      </ul>
      <p className="mt-1.5 pl-6 text-muted-foreground">
        {t("shell.cleanup.hintPre")} <code>cleanupPeriodDays</code> {t("shell.cleanup.hintPost")}
      </p>
    </div>
  )
}
