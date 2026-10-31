import { useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowUpFromLine } from "lucide-react"
import { toast } from "sonner"
import { api, type GitRemote, type GitState } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { refreshGit } from "@/hooks/useGit"
import { syncOf } from "./GitBadge"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

// À pousser : des commits en avance sur un remote, une branche absente d'un remote, ou sans amont (jamais un HEAD détaché).
export const needsPush = (st: GitState | undefined): st is GitState => {
  if (!st || st.detached) return false
  const { ahead, missing } = syncOf(st)
  return !st.upstream || ahead > 0 || missing.length > 0
}

// Pousse la branche courante vers un remote du dépôt. Le menu nomme la cible : choisir un remote est la confirmation.
// Proposé quand il y a des commits à pousser, ou que la branche n'a pas encore d'amont.
export function GitPush({ dir, st }: { dir: string; st: GitState | undefined }) {
  const { t } = useTranslation()
  const [remotes, setRemotes] = useState<GitRemote[] | null>(null)
  const [busy, setBusy] = useState(false)
  if (!needsPush(st)) return null
  const pushToRemote = async (remote: GitRemote) => {
    setBusy(true)
    const loadingToastId = toast.loading(t("conversation.gitPush.pushing", { branch: st.branch, remote: remote.name }))
    try {
      const pushResult = await api.gitPush(dir, remote.name)
      setBusy(false)
      toast.dismiss(loadingToastId)
      toast.success(t("conversation.gitPush.pushed", { branch: st.branch, remote: remote.name }), { description: pushResult.message })
    } catch (error) {
      setBusy(false)
      toast.dismiss(loadingToastId)
      toast.error(t("conversation.gitPush.failed", { remote: remote.name }), { description: errMsg(error) })
    }
    refreshGit()
  }
  const loadRemotesOnOpen = (isOpen: boolean) => {
    if (!isOpen) return
    api.gitRemotes(dir).then(
      (remotesResponse) => setRemotes(remotesResponse.remotes),
      () => setRemotes([])
    )
  }
  const triggerTitle = st.upstream ? t("conversation.git.ahead", { n: syncOf(st).ahead }) : t("conversation.gitPush.noUpstream")
  const syncOfRemote = (name: string) => st.remotes?.find((remoteSync) => remoteSync.name === name)
  return (
    <DropdownMenu onOpenChange={loadRemotesOnOpen}>
      <DropdownMenuTrigger asChild>
        <Button size="xs" variant="outline" disabled={busy} title={triggerTitle}>
          <ArrowUpFromLine /> {t("conversation.gitPush.button")}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-w-96">
        <DropdownMenuLabel className="whitespace-normal">{t("conversation.gitPush.label", { branch: st.branch })}</DropdownMenuLabel>
        {remotes === null && <DropdownMenuItem disabled>{t("conversation.gitPush.reading")}</DropdownMenuItem>}
        {remotes?.length === 0 && <DropdownMenuItem disabled>{t("conversation.gitPush.none")}</DropdownMenuItem>}
        {remotes?.map((remote) => {
          const sync = syncOfRemote(remote.name)
          const state = !sync ? "" : sync.missing ? t("conversation.gitPush.newBranch") : sync.ahead || sync.behind ? t("conversation.gitPush.position", { ahead: sync.ahead, behind: sync.behind }) : t("conversation.git.upToDate")
          return (
            <DropdownMenuItem key={remote.name} onSelect={() => pushToRemote(remote)}>
              <span className="font-medium">{remote.name}</span>
              {state && <span className="ml-auto pl-3 text-xs text-muted-foreground">{state}</span>}
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
