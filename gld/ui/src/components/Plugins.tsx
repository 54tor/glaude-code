import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "wouter"
import { ArchiveRestore } from "lucide-react"
import { api, type CfgOverview } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { PluginsPanel } from "./PluginsDialog"

export function Plugins({ home }: { home: string }) {
  const { t } = useTranslation()
  const [projects, setProjects] = useState<CfgOverview["projects"]>([])
  const reload = useCallback(() => api.config().then((o) => setProjects(o.projects), () => {}), [])
  useEffect(() => void reload(), [reload])
  return (
    <div className="mx-auto w-full max-w-page space-y-4 p-4 md:p-6">
      <div className="flex justify-end">
        <Button size="xs" variant="outline" asChild>
          <Link to="/sauvegarde?cats=plugins">
            <ArchiveRestore /> {t("extensions.exportImport")}
          </Link>
        </Button>
      </div>
      <PluginsPanel inline projects={projects} home={home} onChange={reload} />
    </div>
  )
}
