import { useTranslation } from "react-i18next"
import { GitBranch } from "lucide-react"
import type { GitState } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { GitRepo } from "./GitRepo"
import { needsPush } from "./GitPush"

// Les dépôts des dossiers ajoutés à la session : une pastille résumé, et une liste à l'ouverture avec, par dépôt, la branche, les
// changements et le bouton Pousser. Les dépôts à pousser sont en tête.
export function GitRepos({ dirs, status }: { dirs: string[]; status: Record<string, GitState> }) {
  const { t } = useTranslation()
  if (dirs.length === 0) return null
  const dirsToPushFirst = [...dirs].sort(
    (firstDir, secondDir) => Number(needsPush(status[secondDir])) - Number(needsPush(status[firstDir]))
  )
  const reposToPushCount = dirs.filter((repoDir) => needsPush(status[repoDir])).length
  const hasReposToPush = reposToPushCount > 0
  const summaryLabel = hasReposToPush
    ? t("conversation.gitRepos.toPush", { count: reposToPushCount })
    : t("conversation.gitRepos.repos", { count: dirs.length })
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button size="xs" variant="outline" className={cn(hasReposToPush && "text-warning")} title={t("conversation.gitRepos.title")}>
          <GitBranch /> {summaryLabel}
        </Button>
      </PopoverTrigger>
      <PopoverContent>
        <ul className="flex flex-col gap-1.5">
          {dirsToPushFirst.map((repoDir) => (
            <li key={repoDir} className="flex flex-wrap items-center gap-1.5 text-xs" title={repoDir}>
              <span className="max-w-32 truncate font-medium">{repoDir.split("/").filter(Boolean).pop()}</span>
              <GitRepo dir={repoDir} st={status[repoDir]} chip />
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  )
}
