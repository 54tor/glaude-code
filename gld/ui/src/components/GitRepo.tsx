import type { GitState } from "@/lib/api"
import { GitBadge } from "./GitBadge"
import { GitChanges } from "./GitChanges"
import { GitPush } from "./GitPush"

// L'état d'un dépôt en une rangée : branche et compteurs, bouton Pousser, changements. chip : la branche dans un cadre de 24 px,
// à la hauteur des boutons voisins (en-tête de la conversation).
export function GitRepo({ dir, st, chip }: { dir: string; st: GitState | undefined; chip?: boolean }) {
  const badge = <GitBadge st={st} />
  return (
    <>
      {chip ? <span className="flex h-6 items-center rounded-md border px-2">{badge}</span> : badge}
      <GitPush dir={dir} st={st} />
      <GitChanges dir={dir} count={st ? st.changed + st.untracked : 0} />
    </>
  )
}
