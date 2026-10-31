import { useEffect } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api } from "@/lib/api"

const NOTICE_DURATION_MS = 15000

// Une seule fois par chargement : prévient qu'une release plus récente de gld existe sur GitHub, et que Claude Code n'est pas à jour (échec réseau : silence).
export function useUpdateNotice() {
  const { t } = useTranslation()
  useEffect(() => {
    api.updateInfo().then((updateInfo) => {
      if (updateInfo.claudeNewer) {
        toast.warning(t("shell.app.claudeOutdated", { current: updateInfo.claudeCurrent, latest: updateInfo.claudeLatest }), { duration: NOTICE_DURATION_MS })
      }
      if (!updateInfo.newer) return
      toast.info(t("shell.app.updateAvailable", { latest: updateInfo.latest, current: updateInfo.current }), {
        duration: NOTICE_DURATION_MS,
        action: { label: t("shell.app.updateOpen"), onClick: () => window.open(updateInfo.url, "_blank", "noopener") },
      })
    }, () => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
}
