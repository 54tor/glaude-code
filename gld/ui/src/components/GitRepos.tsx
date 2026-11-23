import { useTranslation } from "react-i18next"
import { GitBranch } from "lucide-react"
import type { GitState } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Chip } from "@/components/ui/chip"
import { GitBadge } from "./GitBadge"
import { GitChanges } from "./GitChanges"
import { GitPush, needsPush } from "./GitPush"

// Les dépôts des dossiers ajoutés à la session : une pastille résumé, et une liste à l'ouverture avec, par dépôt, la branche, les
// changements et le bouton Pousser. Les dépôts à pousser sont en tête.
export function GitRepos({
  dirs,
  status,
}: {
  dirs: string[]
  status: Record<string, GitState>
}) {
  const { t } = useTranslation()
  if (dirs.length === 0) return null
  const dirsToPushFirst = [...dirs].sort(
    (firstDir, secondDir) =>
      Number(needsPush(status[secondDir])) - Number(needsPush(status[firstDir]))
  )
  const reposToPushCount = dirs.filter((repoDir) =>
    needsPush(status[repoDir])
  ).length
  const hasReposToPush = reposToPushCount > 0
  const summaryLabel = hasReposToPush
    ? t("conversation.gitRepos.toPush", { count: reposToPushCount })
    : t("conversation.gitRepos.repos", { count: dirs.length })
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          size="xs"
          variant="outline"
          className={cn(hasReposToPush && "text-warning")}
          title={t("conversation.gitRepos.title")}
        >
          <GitBranch /> {summaryLabel}
        </Button>
      </PopoverTrigger>
      <PopoverContent>
        <ul className="space-y-2 text-xs">
          {dirsToPushFirst.map((repoDir) => (
            <li
              key={repoDir}
              title={repoDir}
              className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1"
            >
              <span className="max-w-full min-w-0 truncate font-medium">
                {repoDir.split("/").filter(Boolean).pop()}
              </span>
              <span className="ml-auto flex flex-wrap items-center justify-end gap-1.5">
                <Chip>
                  <GitBadge st={status[repoDir]} hideClean />
                </Chip>
                <GitPush dir={repoDir} st={status[repoDir]} />
                <GitChanges
                  dir={repoDir}
                  count={
                    status[repoDir]
                      ? status[repoDir].changed + status[repoDir].untracked
                      : 0
                  }
                />
              </span>
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  )
}
