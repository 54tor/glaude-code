import { useEffect, useState } from "react"
import { Trash2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import { api, type ScratchStats } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { size } from "@/lib/format"
import { Button } from "@/components/ui/button"

// Dossiers temporaires de sessions supprimées : comptés d'abord, effacés après une confirmation en deux temps.
export function ScratchCleanup() {
  const { t } = useTranslation()
  const [stats, setStats] = useState<ScratchStats | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")
  const [error, setError] = useState("")

  useEffect(() => {
    api
      .scratchStats()
      .then(setStats)
      .catch(() => setStats(null))
  }, [])

  const purge = async () => {
    setBusy(true)
    setError("")
    try {
      const freed = await api.scratchPurge()
      setMessage(
        t("backup.scratch.done", {
          count: freed.count,
          size: size(freed.kb),
        })
      )
      setStats({ count: 0, kb: 0 })
    } catch (purgeError) {
      setError(errMsg(purgeError))
    }
    setConfirming(false)
    setBusy(false)
  }

  if (!stats) return null

  return (
    <section className="space-y-3">
      <h2 className="text-base font-medium">{t("backup.scratch.title")}</h2>
      <p className="text-sm text-muted-foreground">
        {t("backup.scratch.hint")}
      </p>
      <p className="text-sm">
        {stats.count === 0
          ? t("backup.scratch.none")
          : t("backup.scratch.found", {
              count: stats.count,
              size: size(stats.kb),
            })}
      </p>
      {message && <p className="text-sm text-success">{message}</p>}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {stats.count > 0 && (
        <div className="flex gap-2">
          {confirming ? (
            <>
              <Button variant="destructive" onClick={purge} disabled={busy}>
                <Trash2 /> {t("backup.scratch.confirm")}
              </Button>
              <Button variant="outline" onClick={() => setConfirming(false)}>
                {t("backup.scratch.cancel")}
              </Button>
            </>
          ) : (
            <Button variant="outline" onClick={() => setConfirming(true)}>
              <Trash2 /> {t("backup.scratch.clean")}
            </Button>
          )}
        </div>
      )}
    </section>
  )
}
