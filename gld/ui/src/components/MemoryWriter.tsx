import { useState } from "react"
import { Loader2, Sparkles } from "lucide-react"
import { toast } from "sonner"
import { api, type MemFile } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Choose } from "./Choose"

// Ce qu'on fait rédiger : réécrire un fichier existant, ou créer une entrée (dossier de transcripts, ou "global" pour rules/).
export type WriteTarget = ({ kind: "rewrite"; key: string; name: string } | { kind: "new"; scope: string; label: string }) & {
  instruction?: string // consigne proposée (ex. conversion en CLAUDE.md)
  refs?: string[] // références proposées à la place de la sélection
}

const MODELS = [
  ["haiku", "Haiku (rapide, quelques dixièmes de centime)"],
  ["sonnet", "Sonnet"],
  ["opus", "Opus"],
  ["fable", "Fable"],
] as const

// Rédaction d'un fichier de mémoire par un petit modèle, à partir d'une consigne et de références ; le texte revient en brouillon.
export function MemoryWriter({
  target,
  refs,
  onClose,
  onDraft,
}: {
  target: WriteTarget | null
  refs: MemFile[] // références proposées (cochées au départ)
  onClose: () => void
  onDraft: (t: WriteTarget, text: string) => void
}) {
  return (
    <Dialog open={!!target} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        {target && (
          <Form key={target.kind === "rewrite" ? target.key : target.scope} target={target} refs={refs} onClose={onClose} onDraft={onDraft} />
        )}
      </DialogContent>
    </Dialog>
  )
}

function Form({
  target,
  refs,
  onClose,
  onDraft,
}: {
  target: WriteTarget
  refs: MemFile[]
  onClose: () => void
  onDraft: (t: WriteTarget, text: string) => void
}) {
  const [instruction, setInstruction] = useState(target.instruction ?? "")
  const [model, setModel] = useState("haiku")
  const [on, setOn] = useState(() => new Set(target.refs ?? refs.map((r) => r.key)))
  const [busy, setBusy] = useState(false)

  const run = async () => {
    if (!instruction.trim() || busy) return
    setBusy(true)
    const r = (await api.post("/memory-draft", {
      model,
      instruction,
      key: target.kind === "rewrite" ? target.key : "",
      refs: [...on],
    })) as { ok: boolean; out: string; text?: string; cost?: number }
    setBusy(false)
    if (!r.ok || !r.text) return void toast.error(r.out)
    toast.success("Brouillon prêt : relis-le puis enregistre", { description: r.cost ? `coût : ${r.cost.toFixed(4)} $` : undefined })
    onClose()
    onDraft(target, r.text)
  }

  return (
    <div className="grid min-w-0 gap-4">
      <DialogHeader>
        <DialogTitle>{target.kind === "rewrite" ? `Réécrire ${target.name}` : `Nouvelle entrée · ${target.label}`}</DialogTitle>
        <DialogDescription>Le texte rédigé s'ouvre dans l'éditeur : rien n'est enregistré avant ta relecture.</DialogDescription>
      </DialogHeader>
      <textarea
        value={instruction}
        onChange={(e) => setInstruction(e.target.value)}
        onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && (e.preventDefault(), run())}
        placeholder={target.kind === "rewrite" ? "Ce qu'il faut changer, ajouter ou condenser…" : "Ce que l'entrée doit retenir…"}
        autoFocus
        rows={4}
        maxLength={8000}
        disabled={busy}
        className="session-text w-full resize-y rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        aria-label="Consigne"
      />
      {refs.length > 0 && (
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">Références transmises au modèle</p>
          <ul className="max-h-40 space-y-1 overflow-y-auto">
            {refs.map((r) => (
              <li key={r.key}>
                <label className="flex cursor-pointer items-center gap-2 text-sm">
                  <Checkbox
                    checked={on.has(r.key)}
                    onCheckedChange={() =>
                      setOn((s) => {
                        const n = new Set(s)
                        if (n.has(r.key)) n.delete(r.key)
                        else n.add(r.key)
                        return n
                      })
                    }
                  />
                  <span className="truncate" title={r.path}>
                    {r.name}
                  </span>
                </label>
              </li>
            ))}
          </ul>
        </div>
      )}
      <DialogFooter className="flex-wrap items-center gap-2 sm:justify-between">
        <Choose value={model} onChange={setModel} options={MODELS} label="Modèle" size="default" />
        <div className="flex gap-2">
          <Button variant="outline" onClick={onClose} disabled={busy}>
            Annuler
          </Button>
          <Button onClick={run} disabled={!instruction.trim() || busy} title="Ctrl+Entrée">
            {busy ? <Loader2 className="animate-spin" /> : <Sparkles />} Rédiger
          </Button>
        </div>
      </DialogFooter>
    </div>
  )
}
