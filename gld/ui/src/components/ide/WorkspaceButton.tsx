import { useTranslation } from "react-i18next"
import { FolderTree } from "lucide-react"
import { Link } from "wouter"
import { useCan } from "@/lib/auth"
import { href } from "@/lib/route"
import { Button } from "@/components/ui/button"

export function WorkspaceButton({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const can = useCan()
  if (!dir || !can("fs_read")) return null
  return (
    <Button size="sm" variant="outline" asChild>
      <Link href={href({ kind: "workspace", dir })} title={t("ide.openHint")}>
        <FolderTree /> {t("ide.open")}
      </Link>
    </Button>
  )
}
