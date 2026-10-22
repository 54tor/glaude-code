import { ArrowDown, ArrowUp, Bot, GitBranch } from "lucide-react"
import type { GitState } from "@/lib/api"
import { cn } from "@/lib/utils"

const dirtyOf = (st: GitState | undefined) => (st ? st.changed + st.untracked : 0)

// Navigation : « <changements en attente> [git] » ; rien pour un dossier qui n'est pas un dépôt.
export function GitMini({ states }: { states: (GitState | undefined)[] }) {
  const known = states.filter((s): s is GitState => !!s)
  if (known.length === 0) return null
  const dirty = known.reduce((n, s) => n + dirtyOf(s), 0)
  return (
    <span className="flex shrink-0 items-center gap-0.5 text-xs text-muted-foreground" title={dirty ? `${dirty} changement(s) en attente` : "Aucun changement en attente"}>
      {dirty > 0 && <span className="font-medium text-amber-600 tabular-nums dark:text-amber-400">{dirty}</span>}
      <GitBranch className={cn("size-3.5", dirty > 0 && "text-amber-600 dark:text-amber-400")} aria-label="dépôt git" />
    </span>
  )
}

// Navigation : « <nombre de sessions> [robot] », en jaune quand une session du dossier est ouverte.
export function SessionCount({ n, active }: { n: number; active: boolean }) {
  return (
    <span
      className={cn("flex shrink-0 items-center gap-0.5 text-xs tabular-nums", active ? "font-medium text-amber-500" : "text-muted-foreground")}
      title={active ? `${n} session(s), dont une ouverte` : `${n} session(s)`}
    >
      {n}
      <Bot className="size-3.5" aria-label="sessions" />
    </span>
  )
}

// Vue projet : branche, changements, avance et retard sur l'amont.
export function GitBadge({ st }: { st: GitState | undefined }) {
  if (!st) return null
  const branch = st.detached ? "HEAD détaché" : st.branch
  const title = [
    `Branche : ${branch}`,
    st.upstream ? "" : "pas d'amont",
    st.changed ? `${st.changed} fichier(s) modifié(s)` : "",
    st.untracked ? `${st.untracked} non suivi(s)` : "",
    st.ahead ? `${st.ahead} commit(s) à pousser` : "",
    st.behind ? `${st.behind} commit(s) à récupérer` : "",
    dirtyOf(st) === 0 && !st.ahead && !st.behind ? "propre" : "",
  ]
    .filter(Boolean)
    .join(" · ")
  return (
    <span className="flex shrink-0 items-center gap-2 text-xs text-muted-foreground" title={title}>
      <span className="flex items-center gap-1">
        <GitBranch className="size-3.5" aria-hidden />
        <span className="max-w-40 truncate font-mono">{branch}</span>
      </span>
      {st.changed > 0 && <span className="font-medium text-amber-600 tabular-nums dark:text-amber-400">{st.changed} modifié{st.changed > 1 ? "s" : ""}</span>}
      {st.untracked > 0 && <span className="tabular-nums">{st.untracked} non suivi{st.untracked > 1 ? "s" : ""}</span>}
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
      {dirtyOf(st) === 0 && !st.ahead && !st.behind && <span>propre</span>}
    </span>
  )
}
