import { useEffect, useState } from "react"
import { TriangleAlert } from "lucide-react"
import { api } from "@/lib/api"
import { useActions } from "./actions"

type Warn = Awaited<ReturnType<typeof api.cleanupWarning>>

// Claude Code efface seul les transcripts plus vieux que `cleanupPeriodDays` : on prévient dès qu'il reste moins de 24 h.
export function CleanupWarning() {
  const a = useActions()
  const [w, setW] = useState<(Warn & { at: number }) | null>(null) // at : heure du relevé, pour compter les heures restantes
  useEffect(() => {
    let live = true
    const load = () => document.visibilityState === "visible" && api.cleanupWarning().then((r) => live && setW({ ...r, at: Date.now() / 1000 }), () => {})
    load()
    const t = setInterval(load, 10 * 60_000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [])
  if (!w || w.sessions.length === 0) return null
  const left = (e: number) => {
    const h = Math.ceil((e - w.at) / 3600)
    return h <= 0 ? "dépassé : à tout moment" : h <= 1 ? "dans moins d'1 h" : `dans ${h} h`
  }
  const first = w.sessions[0]
  const n = w.sessions.length
  return (
    <div role="alert" className="mx-3 mb-2 rounded-lg border border-amber-500/50 bg-amber-500/10 p-2.5 text-xs">
      <p className="flex items-start gap-2 font-medium">
        <TriangleAlert className="mt-0.5 size-4 shrink-0 text-amber-600" aria-hidden />
        <span>
          {n} session{n > 1 ? "s" : ""} {n > 1 ? "seront effacées" : "sera effacée"} par Claude Code ({left(first.expires)} pour la première). Il garde les
          transcripts {w.days} jour{w.days > 1 ? "s" : ""} (<code>cleanupPeriodDays</code>).
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
            et {n - 3} autre{n - 3 > 1 ? "s" : ""}…
          </li>
        )}
      </ul>
      <p className="mt-1.5 pl-6 text-muted-foreground">
        Pour les garder, augmentez <code>cleanupPeriodDays</code> dans ~/.claude/settings.json (Configuration, onglet Réglages).
      </p>
    </div>
  )
}
