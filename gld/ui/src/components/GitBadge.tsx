import { ArrowDown, ArrowUp, Bot, GitBranch } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { GitState } from "@/lib/api"
import { cn } from "@/lib/utils"

const dirtyOf = (st: GitState | undefined) => (st ? st.changed + st.untracked : 0)

// Navigation : « <changements en attente> [git] » ; rien pour un dossier qui n'est pas un dépôt.
export function GitMini({ states }: { states: (GitState | undefined)[] }) {
  const { t } = useTranslation()
  const dirty = states.reduce((n, s) => n + dirtyOf(s), 0)
  if (dirty === 0) return null // rien en attente : ni nombre ni icône
  return (
    <span className="flex shrink-0 items-center gap-0.5 text-xs text-amber-600 dark:text-amber-400" title={t("conversation.git.pending", { n: dirty })}>
      <span className="font-medium tabular-nums">{dirty}</span>
      <GitBranch className="size-3.5" aria-label={t("conversation.git.repoAria")} />
    </span>
  )
}

// Navigation : « <nombre de sessions> [robot] », en jaune quand une session du dossier est ouverte.
export function SessionCount({ n, active }: { n: number; active: boolean }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn("flex shrink-0 items-center gap-0.5 text-xs tabular-nums", active ? "font-medium text-amber-500" : "text-muted-foreground")}
      title={active ? t("conversation.git.sessionsOpen", { n }) : t("conversation.git.sessions", { n })}
    >
      {n}
      <Bot className="size-3.5" aria-label={t("conversation.git.sessionsAria")} />
    </span>
  )
}

// Vue projet : branche, changements, avance et retard sur l'amont.
export function GitBadge({ st }: { st: GitState | undefined }) {
  const { t } = useTranslation()
  if (!st) return null
  const branch = st.detached ? t("conversation.git.detached") : st.branch
  const title = [
    t("conversation.git.branch", { branch }),
    st.upstream ? "" : t("conversation.git.noUpstream"),
    st.changed ? t("conversation.git.changedTitle", { n: st.changed }) : "",
    st.untracked ? t("conversation.git.untrackedTitle", { n: st.untracked }) : "",
    st.ahead ? t("conversation.git.ahead", { n: st.ahead }) : "",
    st.behind ? t("conversation.git.behind", { n: st.behind }) : "",
    dirtyOf(st) === 0 && !st.ahead && !st.behind ? t("conversation.git.clean") : "",
  ]
    .filter(Boolean)
    .join(" · ")
  return (
    <span className="flex shrink-0 items-center gap-2 text-xs text-muted-foreground" title={title}>
      <span className="flex items-center gap-1">
        <GitBranch className="size-3.5" aria-hidden />
        <span className="max-w-40 truncate font-mono">{branch}</span>
      </span>
      {st.changed > 0 && <span className="font-medium text-amber-600 tabular-nums dark:text-amber-400">{t("conversation.git.modified", { count: st.changed })}</span>}
      {st.untracked > 0 && <span className="tabular-nums">{t("conversation.git.untracked", { count: st.untracked })}</span>}
      {st.ahead > 0 && (
        <span className="flex items-center tabular-nums">
          <ArrowUp className="size-3.5" aria-hidden />
          {st.ahead}
        </span>
      )}
      {st.behind > 0 && (
        <span className="flex items-center tabular-nums">
          <ArrowDown className="size-3.5" aria-hidden />
          {st.behind}
        </span>
      )}
      {dirtyOf(st) === 0 && !st.ahead && !st.behind && <span>{t("conversation.git.clean")}</span>}
    </span>
  )
}
