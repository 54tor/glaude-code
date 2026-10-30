import { useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowUpFromLine } from "lucide-react"
import { toast } from "sonner"
import { api, type GitRemote, type GitState } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { refreshGit } from "@/hooks/useGit"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

// À pousser : des commits en avance, ou une branche sans amont (jamais un HEAD détaché).
export const needsPush = (st: GitState | undefined): st is GitState => !!st && !st.detached && (!st.upstream || st.ahead > 0)

// Pousse la branche courante vers un remote du dépôt. Le menu nomme la cible : choisir un remote est la confirmation.
// Proposé quand il y a des commits à pousser, ou que la branche n'a pas encore d'amont.
export function GitPush({ dir, st }: { dir: string; st: GitState | undefined }) {
  const { t } = useTranslation()
  const [remotes, setRemotes] = useState<GitRemote[] | null>(null)
  const [busy, setBusy] = useState(false)
  if (!needsPush(st)) return null
  const push = async (r: GitRemote) => {
    setBusy(true)
    const id = toast.loading(t("conversation.gitPush.pushing", { branch: st.branch, remote: r.name }))
    try {
      const res = await api.gitPush(dir, r.name)
      setBusy(false)
      toast.dismiss(id)
      toast.success(t("conversation.gitPush.pushed", { branch: st.branch, remote: r.name }), { description: res.message })
    } catch (e) {
      setBusy(false)
      toast.dismiss(id)
      toast.error(t("conversation.gitPush.failed", { remote: r.name }), { description: errMsg(e) })
    }
    refreshGit()
  }
  return (
    <DropdownMenu onOpenChange={(open) => open && api.gitRemotes(dir).then((r) => setRemotes(r.remotes), () => setRemotes([]))}>
      <DropdownMenuTrigger asChild>
        <Button size="xs" variant="outline" disabled={busy} title={st.upstream ? t("conversation.git.ahead", { n: st.ahead }) : t("conversation.gitPush.noUpstream")}>
          <ArrowUpFromLine /> {t("conversation.gitPush.button")}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-w-96">
        <DropdownMenuLabel className="whitespace-normal">{t("conversation.gitPush.label", { branch: st.branch })}</DropdownMenuLabel>
        {remotes === null && <DropdownMenuItem disabled>{t("conversation.gitPush.reading")}</DropdownMenuItem>}
        {remotes?.length === 0 && <DropdownMenuItem disabled>{t("conversation.gitPush.none")}</DropdownMenuItem>}
        {remotes?.map((r) => (
          <DropdownMenuItem key={r.name} onSelect={() => push(r)}>
            <span className="font-medium">{r.name}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
