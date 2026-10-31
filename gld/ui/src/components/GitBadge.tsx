import { ArrowDown, ArrowUp, Bot, GitBranch } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { GitState } from "@/lib/api"
import { cn } from "@/lib/utils"

// Avance et retard sur le pire des remotes (l'amont compris) : un remote en retard ou sans la branche compte, même si l'amont est à jour.
export const syncOf = (gitState: GitState) => {
  const remotes = gitState.remotes ?? []
  return {
    ahead: Math.max(gitState.ahead, ...remotes.map((remote) => remote.ahead)),
    behind: Math.max(gitState.behind, ...remotes.map((remote) => remote.behind)),
    missing: remotes.filter((remote) => remote.missing),
  }
}

const dirtyOf = (gitState: GitState | undefined) => (gitState ? gitState.changed + gitState.untracked : 0)

// Navigation : « <changements en attente> [git] » ; rien pour un dossier qui n'est pas un dépôt.
// slot : réserve la place quand rien n'est en attente, pour que les icônes s'alignent d'une ligne à l'autre.
export function GitMini({ states, slot }: { states: (GitState | undefined)[]; slot?: boolean }) {
  const { t } = useTranslation()
  const dirty = states.reduce((dirtyTotal, gitState) => dirtyTotal + dirtyOf(gitState), 0)
  if (dirty === 0) return slot ? <span className="w-9 shrink-0" aria-hidden /> : null // rien en attente : ni nombre ni icône
  return (
    <span className="flex w-9 shrink-0 items-center justify-end gap-0.5 text-xs text-warning" title={t("conversation.git.pending", { n: dirty })}>
      <span className="font-medium tabular-nums">{dirty}</span>
      <GitBranch className="size-3.5" aria-label={t("conversation.git.repoAria")} />
    </span>
  )
}

// Navigation : « <nombre de sessions> [robot] », en jaune quand une session du dossier est ouverte.
export function SessionCount({ n: count, active }: { n: number; active: boolean }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn("flex w-9 shrink-0 items-center justify-end gap-0.5 text-xs tabular-nums", active ? "font-medium text-warning" : "text-muted-foreground")}
      title={active ? t("conversation.git.sessionsOpen", { n: count }) : t("conversation.git.sessions", { n: count })}
    >
      {count}
      <Bot className="size-3.5" aria-label={t("conversation.git.sessionsAria")} />
    </span>
  )
}

// Vue projet : branche, changements, avance et retard sur l'amont.
export function GitBadge({ st }: { st: GitState | undefined }) {
  const { t } = useTranslation()
  if (!st) return null
  const branch = st.detached ? t("conversation.git.detached") : st.branch
  const { ahead, behind, missing } = syncOf(st)
  const remotes = st.remotes ?? []
  const isClean = dirtyOf(st) === 0 && !ahead && !behind && missing.length === 0
  const title = [
    t("conversation.git.branch", { branch }),
    st.upstream ? "" : t("conversation.git.noUpstream"),
    st.changed ? t("conversation.git.changedTitle", { n: st.changed }) : "",
    st.untracked ? t("conversation.git.untrackedTitle", { n: st.untracked }) : "",
    remotes.length === 0 && st.ahead ? t("conversation.git.ahead", { n: st.ahead }) : "",
    remotes.length === 0 && st.behind ? t("conversation.git.behind", { n: st.behind }) : "",
    ...remotes.map((remote) => t("conversation.git.remoteLine", { name: remote.name, context: remote.missing ? "missing" : "", ahead: remote.ahead, behind: remote.behind })),
    isClean ? t("conversation.git.clean") : "",
  ]
    .filter(Boolean)
    .join(" · ")
  return (
    <span className="flex shrink-0 items-center gap-2 text-xs text-muted-foreground" title={title}>
      <span className="flex items-center gap-1">
        <GitBranch className="size-3.5" aria-hidden />
        <span className="max-w-40 truncate font-mono">{branch}</span>
      </span>
      {st.changed > 0 && <span className="font-medium text-warning tabular-nums">{t("conversation.git.modified", { count: st.changed })}</span>}
      {st.untracked > 0 && <span className="tabular-nums">{t("conversation.git.untracked", { count: st.untracked })}</span>}
      {ahead > 0 && (
        <span className="flex items-center tabular-nums">
          <ArrowUp className="size-3.5" aria-hidden />
          {ahead}
        </span>
      )}
      {behind > 0 && (
        <span className="flex items-center tabular-nums">
          <ArrowDown className="size-3.5" aria-hidden />
          {behind}
        </span>
      )}
      {missing.length > 0 && <span className="text-warning">{t("conversation.git.missingOn", { names: missing.map((remote) => remote.name).join(", ") })}</span>}
      {isClean && <span>{t("conversation.git.clean")}</span>}
    </span>
  )
}
