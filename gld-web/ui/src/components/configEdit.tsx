import { useState, type ReactNode } from "react"
import { toast } from "sonner"
import { api, type CfgEdit } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { DiffView } from "./MemoryDiff"

type Preview = { edit: CfgEdit; before: string; after: string }
type Reply = { ok: boolean; out: string; before: string; after: string }

// Toute modification d'un fichier de réglages passe par ici : aperçu des différences, confirmation, puis écriture
// (le serveur fait une sauvegarde avant). Renvoie propose() et le dialogue à afficher.
export function useConfigEdit(onDone: () => void): { propose: (e: CfgEdit) => Promise<void>; dialog: ReactNode } {
  const [pv, setPv] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  const propose = async (edit: CfgEdit) => {
    const r = (await api.post("/config-edit", { ...edit, dry: true })) as Reply
    if (!r.ok) return void toast.error(r.out)
    if (r.before === r.after) return void toast.info("Aucun changement")
    setPv({ edit, before: r.before, after: r.after })
  }
  const apply = async () => {
    if (!pv) return
    setBusy(true)
    const r = (await api.post("/config-edit", pv.edit)) as Reply
    setBusy(false)
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) {
      setPv(null)
      onDone()
    }
  }
  const dialog = (
    <Dialog open={!!pv} onOpenChange={(o) => !o && setPv(null)}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>Enregistrer cette modification ?</DialogTitle>
          <DialogDescription>
            {pv?.edit.file === "u" ? "~/.claude/settings.json" : pv?.edit.file} — une sauvegarde de l'état actuel est faite avant l'écriture.
          </DialogDescription>
        </DialogHeader>
        <ScrollArea className="max-h-96 rounded-md border">{pv && <DiffView old={pv.before} cur={pv.after} compact />}</ScrollArea>
        <DialogFooter>
          <Button variant="ghost" onClick={() => setPv(null)}>
            Annuler
          </Button>
          <Button onClick={apply} disabled={busy}>
            Enregistrer
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
  return { propose, dialog }
}
