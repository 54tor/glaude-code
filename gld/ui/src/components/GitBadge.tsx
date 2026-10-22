import { ArrowDown, ArrowUp, GitBranch } from "lucide-react"
import type { GitState } from "@/lib/api"

// Branche et état d'un dossier : points ambrés pour les fichiers modifiés, flèches pour l'avance et le retard sur l'amont.
export function GitBadge({ st }: { st: GitState | undefined }) {
  if (!st) return null
  const branch = st.detached ? "HEAD détaché" : st.branch
  const dirty = st.changed + st.untracked
  const title = [
    `Branche : ${branch}`,
    st.upstream ? "" : "pas d'amont",
    st.changed ? `${st.changed} fichier(s) modifié(s)` : "",
    st.untracked ? `${st.untracked} non suivi(s)` : "",
    st.ahead ? `${st.ahead} commit(s) à pousser` : "",
    st.behind ? `${st.behind} commit(s) à récupérer` : "",
    dirty === 0 && !st.ahead && !st.behind ? "propre" : "",
  ]
    .filter(Boolean)
    .join(" · ")
  return (
    <span className="flex shrink-0 items-center gap-1 text-[11px] text-muted-foreground" title={title}>
      <GitBranch className="size-3" aria-hidden />
      <span className="max-w-16 truncate">{branch}</span>
      {dirty > 0 && <span className="font-medium text-amber-600 tabular-nums dark:text-amber-400">●{dirty}</span>}
      {st.ahead > 0 && (
        <span className="flex items-center tabular-nums">
          <ArrowUp className="size-3" aria-hidden />
          {st.ahead}
        </span>
      )}
      {st.behind > 0 && (
        <span className="flex items-center tabular-nums">
          <ArrowDown className="size-3" aria-hidden />
          {st.behind}
        </span>
      )}
    </span>
  )
}
