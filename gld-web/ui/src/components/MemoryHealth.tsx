import { AlertTriangle, Wrench } from "lucide-react"
import type { Issue } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"

// --- santé -------------------------------------------------------------------------------------------

const KINDS: Record<string, string> = {
  "index-ligne": "Index : ligne orpheline",
  "index-absent": "Index : entrée absente",
  "index-long": "Index trop long",
  import: "Import cassé",
  lien: "Lien cassé",
  "front-matter": "Front matter",
  perime: "Entrée ancienne",
}

export function HealthDialog({
  open,
  issues,
  onClose,
  onOpenKey,
  onRepair,
}: {
  open: boolean
  issues: Issue[]
  onClose: () => void
  onOpenKey: (key: string) => void
  onRepair: (dir: string) => void
}) {
  const repairable = [...new Set(issues.filter((i) => i.dir && (i.kind === "index-ligne" || i.kind === "index-absent")).map((i) => i.dir!))]
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Santé de la mémoire</DialogTitle>
          <DialogDescription>
            {issues.length ? `${issues.length} point${issues.length > 1 ? "s" : ""} à vérifier. Rien n'est modifié sans votre clic.` : "Rien à signaler."}
          </DialogDescription>
        </DialogHeader>
        {repairable.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {repairable.map((d) => (
              <Button key={d} size="xs" variant="outline" onClick={() => onRepair(d)} title="Retire les lignes mortes et ajoute les entrées manquantes de MEMORY.md">
                <Wrench /> Réparer l'index de {d}
              </Button>
            ))}
          </div>
        )}
        <ScrollArea className="max-h-96">
          <ul className="space-y-1 pr-3">
            {issues.map((i, n) => (
              <li key={n}>
                <button onClick={() => (onOpenKey(i.key), onClose())} className="flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted">
                  <AlertTriangle className={cn("mt-0.5 size-3.5 shrink-0", i.kind === "perime" ? "text-muted-foreground" : "text-amber-500")} />
                  <span className="min-w-0">
                    <span className="block text-xs font-medium text-muted-foreground">
                      {KINDS[i.kind] ?? i.kind} · {i.key}
                    </span>
                    <span className="break-words">{i.msg}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
