import { useTranslation } from "react-i18next"
import { GitBranch } from "lucide-react"
import type { GitState } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { GitBadge } from "./GitBadge"
import { GitChanges } from "./GitChanges"
import { GitPush } from "./GitPush"

// À pousser : des commits en avance, ou une branche sans amont.
const needsPush = (st: GitState | undefined) => !!st && !st.detached && (!st.upstream || st.ahead > 0)

// Les dépôts des dossiers ajoutés à la session : une pastille résumé, et une liste à l'ouverture avec, par dépôt, la branche, les
// changements et le bouton Pousser. Les dépôts à pousser sont en tête.
export function GitRepos({ dirs, status }: { dirs: string[]; status: Record<string, GitState> }) {
  const { t } = useTranslation()
  if (dirs.length === 0) return null
  const sorted = [...dirs].sort((a, b) => Number(needsPush(status[b])) - Number(needsPush(status[a])))
  const push = dirs.filter((d) => needsPush(status[d])).length
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button size="xs" variant="outline" className={cn(push > 0 && "text-warning")} title={t("conversation.gitRepos.title")}>
          <GitBranch /> {push > 0 ? t("conversation.gitRepos.toPush", { count: push }) : t("conversation.gitRepos.repos", { count: dirs.length })}
        </Button>
      </PopoverTrigger>
      <PopoverContent>
        <ul className="flex flex-col gap-1.5">
          {sorted.map((d) => (
            <li key={d} className="flex flex-wrap items-center gap-1.5 text-xs" title={d}>
              <span className="max-w-32 truncate font-medium">{d.split("/").filter(Boolean).pop()}</span>
              <GitBadge st={status[d]} />
              <GitPush dir={d} st={status[d]} />
              <GitChanges dir={d} count={status[d] ? status[d].changed + status[d].untracked : 0} />
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  )
}
