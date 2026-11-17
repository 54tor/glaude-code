import { useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowUpFromLine } from "lucide-react"
import type { GitState } from "@/lib/api"
import { syncOf } from "./GitBadge"
import { isGitPushRunning, useGitPushRuns } from "@/hooks/useGitPush"
import { Button } from "@/components/ui/button"
import { GitPushDialog } from "./GitPushDialog"

// À pousser : des commits en avance sur un remote, une branche absente d'un remote, ou sans amont (jamais un HEAD détaché).
export const needsPush = (st: GitState | undefined): st is GitState => {
  if (!st || st.detached) return false
  const { ahead, missing } = syncOf(st)
  return !st.upstream || ahead > 0 || missing.length > 0
}

// Ouvre le dialogue de poussée : aperçu des commits manquants par remote, puis poussée vers les remotes cochés. Le dialogue
// nomme la cible : le bouton « Pousser » du dialogue est la confirmation. Le push tourne hors de ce composant (hooks/useGitPush) :
// sa sortie, hook local compris, est dans un terminal en lecture seule qu'on peut réduire en toast.
export function GitPush({ dir, st }: { dir: string; st: GitState | undefined }) {
  const { t } = useTranslation()
  const [isDialogOpen, setIsDialogOpen] = useState(false)
  useGitPushRuns()
  if (!needsPush(st)) return null
  const triggerTitle = st.upstream ? t("conversation.git.ahead", { n: syncOf(st).ahead }) : t("conversation.gitPush.noUpstream")
  return (
    <>
      <Button size="xs" variant="outline" disabled={isGitPushRunning(dir)} title={triggerTitle} onClick={() => setIsDialogOpen(true)}>
        <ArrowUpFromLine /> {t("conversation.gitPush.button")}
      </Button>
      <GitPushDialog dir={dir} branch={st.branch} open={isDialogOpen} onOpenChange={setIsDialogOpen} />
    </>
  )
}
