import { useState } from "react"
import { ArrowUpFromLine } from "lucide-react"
import { toast } from "sonner"
import { api, type GitRemote, type GitState } from "@/lib/api"
import { refreshGit } from "@/hooks/useGit"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

// Pousse la branche courante vers un remote du dépôt. Le menu nomme la cible : choisir un remote est la confirmation.
// Proposé quand il y a des commits à pousser, ou que la branche n'a pas encore d'amont.
export function GitPush({ dir, st }: { dir: string; st: GitState | undefined }) {
  const [remotes, setRemotes] = useState<GitRemote[] | null>(null)
  const [busy, setBusy] = useState(false)
  if (!st || st.detached || (st.upstream && st.ahead === 0)) return null
  const push = async (r: GitRemote) => {
    setBusy(true)
    const id = toast.loading(`Poussée de ${st.branch} vers ${r.name}…`)
    const res = await api.post("/git-push", { dir, remote: r.name }, 90_000)
    setBusy(false)
    toast.dismiss(id)
    if (res.ok) toast.success(`${st.branch} poussée vers ${r.name}`, { description: res.out })
    else toast.error(`Échec de la poussée vers ${r.name}`, { description: res.out })
    refreshGit()
  }
  return (
    <DropdownMenu onOpenChange={(open) => open && api.gitRemotes(dir).then((r) => setRemotes(r.remotes), () => setRemotes([]))}>
      <DropdownMenuTrigger asChild>
        <Button size="xs" variant="outline" disabled={busy} title={st.upstream ? `${st.ahead} commit(s) à pousser` : "Branche sans amont : la poussée la lie au remote choisi"}>
          <ArrowUpFromLine /> Pousser
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-w-96">
        <DropdownMenuLabel className="whitespace-normal">Pousser « {st.branch} » vers :</DropdownMenuLabel>
        {remotes === null && <DropdownMenuItem disabled>Lecture des remotes…</DropdownMenuItem>}
        {remotes?.length === 0 && <DropdownMenuItem disabled>Aucun remote configuré</DropdownMenuItem>}
        {remotes?.map((r) => (
          <DropdownMenuItem key={r.name} onSelect={() => push(r)} title={r.url}>
            <span className="font-medium">{r.name}</span>
            <span className="ml-2 truncate text-xs text-muted-foreground">{r.url}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
