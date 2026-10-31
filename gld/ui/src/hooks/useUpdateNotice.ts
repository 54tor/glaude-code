import { useEffect } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api } from "@/lib/api"

// Une seule fois par chargement : prévient qu'une release plus récente de gld existe sur GitHub, et que Claude Code n'est pas à jour (échec réseau : silence).
export function useUpdateNotice() {
  const { t } = useTranslation()
  useEffect(() => {
    api.updateInfo().then((u) => {
      if (u.claudeNewer) toast.warning(t("shell.app.claudeOutdated", { current: u.claudeCurrent, latest: u.claudeLatest }), { duration: 15000 })
      if (!u.newer) return
      toast.info(t("shell.app.updateAvailable", { latest: u.latest, current: u.current }), {
        duration: 15000,
        action: { label: t("shell.app.updateOpen"), onClick: () => window.open(u.url, "_blank", "noopener") },
      })
    }, () => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
}
