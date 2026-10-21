import { useState, type ReactNode } from "react"
import { toast } from "sonner"
import { api, type CfgEdit, type CfgIssue } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { DiffView } from "./MemoryDiff"

type Preview = { edit?: CfgEdit; raw?: { file: string; text: string }; before: string; after: string; warnings: CfgIssue[] }
type Reply = { ok: boolean; out: string; before: string; after: string; warnings?: CfgIssue[] }

// Toute modification d'un fichier de réglages passe par ici : aperçu des différences, confirmation, puis écriture
// (le serveur fait une sauvegarde avant). Renvoie propose() et le dialogue à afficher.
export function useConfigEdit(onDone: () => void): { propose: (e: CfgEdit) => Promise<void>; proposeRaw: (file: string, text: string) => Promise<string | null>; dialog: ReactNode } {
  const [pv, setPv] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  const propose = async (edit: CfgEdit) => {
    const r = (await api.post("/config-edit", { ...edit, dry: true })) as Reply
    if (!r.ok) return void toast.error(r.out)
    if (r.before === r.after) return void toast.info("Aucun changement")
    setPv({ edit, before: r.before, after: r.after, warnings: r.warnings ?? [] })
  }
  // JSON brut : renvoie le message d'erreur (avec ligne et colonne) ou null si l'aperçu s'ouvre.
  const proposeRaw = async (file: string, text: string) => {
    const r = (await api.post("/config-raw", { file, text, dry: true })) as Reply
    if (!r.ok) return r.out
    if (r.before === r.after) {
      toast.info("Aucun changement")
      return null
    }
    setPv({ raw: { file, text }, before: r.before, after: r.after, warnings: r.warnings ?? [] })
    return null
  }
  const apply = async () => {
    if (!pv) return
    setBusy(true)
    const r = (pv.raw ? await api.post("/config-raw", { ...pv.raw, dry: false }) : await api.post("/config-edit", pv.edit)) as Reply
    setBusy(false)
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) {
      setPv(null)
      onDone()
    }
  }
  const dialog = (
    <Dialog open={!!pv} onOpenChange={(o) => !o && setPv(null)}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>Enregistrer cette modification ?</DialogTitle>
          <DialogDescription>
            {(pv?.edit?.file ?? pv?.raw?.file) === "u" ? "~/.claude/settings.json" : (pv?.edit?.file ?? pv?.raw?.file)} — une sauvegarde de l'état actuel est faite avant l'écriture.
          </DialogDescription>
        </DialogHeader>
        {!!pv?.warnings.length && (
          <ul className="space-y-1 rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs">
            {pv.warnings.map((w, i) => (
              <li key={i}>⚠ {w.msg}</li>
            ))}
          </ul>
        )}
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
  return { propose, proposeRaw, dialog }
}
