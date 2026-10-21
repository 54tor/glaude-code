import { useState, type ReactNode } from "react"
import { toast } from "sonner"
import { api, type CfgEdit, type CfgIssue } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { DiffView, lineDiff } from "./MemoryDiff"

// Après une écriture : ce que les sessions ouvertes en voient. Claude Code surveille ses fichiers de réglages et les recharge
// seul (permissions, hooks, variables d'environnement…) : rien à pousser. Quelques clés ne sont lues qu'au démarrage d'une
// session ou passent par une commande : on dit lesquelles, d'après les lignes qui ont changé.
export function liveNotice(before: string, after: string, sessions: number): string {
  const changed = lineDiff(before, after)
    .filter((r) => r.op !== " ")
    .map((r) => r.t)
    .join("\n")
  const todo: string[] = []
  if (/"model"\s*:/.test(changed)) todo.push("model : dans une session ouverte, tapez /model")
  if (/"(effortLevel|modelSettings)"\s*:/.test(changed)) todo.push("effort : tapez /effort dans la session")
  if (/"enabledPlugins"|"pluginConfigs"|"[\w.-]+@[\w.-]+"\s*:/.test(changed)) todo.push("plugins : tapez /reload-plugins dans la session")
  const live = sessions ? `Les ${sessions} session${sessions > 1 ? "s" : ""} ouverte${sessions > 1 ? "s" : ""} rechargent les réglages toutes seules.` : "Les sessions ouvertes rechargent les réglages toutes seules."
  return todo.length ? `${live} À faire à la main — ${todo.join(" ; ")}.` : live
}

type Preview = { then?: () => void; edit?: CfgEdit; raw?: { file: string; text: string }; perm?: { file: string; text: string; mode: string }; summary?: string; before: string; after: string; warnings: CfgIssue[] }
type Reply = { ok: boolean; out: string; before: string; after: string; warnings?: CfgIssue[] }

// Toute modification d'un fichier de réglages passe par ici : aperçu des différences, confirmation, puis écriture
// (le serveur fait une sauvegarde avant). Renvoie propose() et le dialogue à afficher.
export function useConfigEdit(onDone: () => void, sessions = 0): { propose: (e: CfgEdit) => Promise<void>; proposeRaw: (file: string, text: string) => Promise<string | null>; proposePerms: (file: string, text: string, mode: string, then?: () => void) => Promise<string | null>; dialog: ReactNode } {
  const [pv, setPv] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  // Modification simple (une règle, un réglage, une variable) : appliquée tout de suite, sans fenêtre de confirmation. Le serveur
  // fait une sauvegarde avant d'écrire (onglet Sauvegardes) ; le toast dit ce que les sessions ouvertes en voient.
  const propose = async (edit: CfgEdit) => {
    const r = (await api.post("/config-edit", edit)) as Reply
    if (!r.ok) return void toast.error(r.out)
    if (r.before === r.after) return void toast.info("Aucun changement")
    toast.success(r.out, { description: liveNotice(r.before, r.after, sessions), duration: 6000 })
    onDone()
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
  // Import de règles de permission : renvoie le message d'erreur, ou null si l'aperçu s'ouvre.
  // then : lancé après l'enregistrement réussi (abandonné si on annule), par exemple pour retirer la règle de son fichier d'origine.
  const proposePerms = async (file: string, text: string, mode: string, then?: () => void) => {
    const r = (await api.post("/config-perm-import", { file, text, mode, dry: true })) as Reply
    if (!r.ok) return r.out
    if (r.before === r.after) {
      toast.info("Aucun changement : ces règles sont déjà là")
      return null
    }
    setPv({ then, perm: { file, text, mode }, summary: r.out, before: r.before, after: r.after, warnings: r.warnings ?? [] })
    return null
  }
  const apply = async () => {
    if (!pv) return
    setBusy(true)
    const r = (pv.perm ? await api.post("/config-perm-import", { ...pv.perm, dry: false }) : pv.raw ? await api.post("/config-raw", { ...pv.raw, dry: false }) : await api.post("/config-edit", pv.edit)) as Reply
    setBusy(false)
    if (r.ok) toast.success(r.out, { description: liveNotice(r.before, r.after, sessions), duration: 9000 })
    else toast.error(r.out)
    if (r.ok) {
      setPv(null)
      onDone()
      pv.then?.()
    }
  }
  const dialog = (
    <Dialog open={!!pv} onOpenChange={(o) => !o && setPv(null)}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>Enregistrer cette modification ?</DialogTitle>
          <DialogDescription>
            {(pv?.edit?.file ?? pv?.raw?.file ?? pv?.perm?.file) === "u" ? "~/.claude/settings.json" : (pv?.edit?.file ?? pv?.raw?.file ?? pv?.perm?.file)}{pv?.summary ? ` — ${pv.summary}` : ""} — une sauvegarde de l'état actuel est faite avant l'écriture.
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
  return { propose, proposeRaw, proposePerms, dialog }
}
