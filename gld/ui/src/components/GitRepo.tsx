import type { GitState } from "@/lib/api"
import { Chip } from "@/components/ui/chip"
import { GitBadge } from "./GitBadge"
import { GitChanges } from "./GitChanges"
import type { PushOrigin } from "@/lib/gitPushStream"
import { GitPush } from "./GitPush"

// L'état d'un dépôt en une rangée : branche et compteurs, bouton Pousser, changements. chip : la branche dans un Chip,
// à la taille des boutons voisins (en-tête de la conversation).
export function GitRepo({
  dir,
  st,
  chip,
  hideClean,
  origin,
}: {
  dir: string
  st: GitState | undefined
  chip?: boolean
  hideClean?: boolean
  origin?: PushOrigin
}) {
  const badge = <GitBadge st={st} hideClean={hideClean} />
  return (
    <>
      {chip ? <Chip>{badge}</Chip> : badge}
      <GitPush dir={dir} st={st} origin={origin} />
      <GitChanges dir={dir} count={st ? st.changed + st.untracked : 0} />
    </>
  )
}
