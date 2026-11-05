import { RefreshCw } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { useStaleBuild } from "@/lib/staleBuild"

export function StaleBuildSplash() {
  const { t } = useTranslation()
  const isStale = useStaleBuild()
  if (!isStale) return null
  return (
    <div role="alertdialog" aria-modal="true" className="safe-t safe-b fixed inset-0 z-[100] flex flex-col items-center justify-center gap-6 bg-background/95 p-6 text-center backdrop-blur">
      <RefreshCw className="size-12 text-primary" aria-hidden />
      <h2 className="text-xl font-semibold">{t("common.newVersionTitle")}</h2>
      <p className="max-w-prose text-muted-foreground">{t("common.newVersionDescription")}</p>
      <Button onClick={() => window.location.reload()}>{t("common.newVersionReload")}</Button>
    </div>
  )
}
