import type { GitState } from "@/lib/api"
import { Chip } from "@/components/ui/chip"
import { GitBadge } from "./GitBadge"
import { GitChanges } from "./GitChanges"
import { GitPush } from "./GitPush"

// L'état d'un dépôt en une rangée : branche et compteurs, bouton Pousser, changements. chip : la branche dans un Chip,
// à la taille des boutons voisins (en-tête de la conversation).
export function GitRepo({ dir, st, chip }: { dir: string; st: GitState | undefined; chip?: boolean }) {
  const badge = <GitBadge st={st} />
  return (
    <>
      {chip ? <Chip>{badge}</Chip> : badge}
      <GitPush dir={dir} st={st} />
      <GitChanges dir={dir} count={st ? st.changed + st.untracked : 0} />
    </>
  )
}
