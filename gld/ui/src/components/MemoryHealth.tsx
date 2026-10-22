import { useState } from "react"
import { AlertTriangle, Loader2, Sparkles, Wrench } from "lucide-react"
import type { Finding, Issue } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Choose } from "./Choose"

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
  onFix,
  scopes,
  onLint,
}: {
  open: boolean
  issues: Issue[]
  onClose: () => void
  onOpenKey: (key: string) => void
  onRepair: (dir: string) => void
  onFix: (keys: string[]) => void // met entre guillemets les valeurs YAML qui cassent le front matter
  scopes: readonly (readonly [string, string])[] // portées de l'analyse : « global », puis les projets
  onLint: (scope: string) => Promise<{ findings?: Finding[]; error?: string; cost?: number }>
}) {
  const [scope, setScope] = useState("global")
  const [busy, setBusy] = useState(false)
  const [lint, setLint] = useState<{ scope: string; findings?: Finding[]; error?: string; cost?: number } | null>(null)
  const analyse = async () => {
    setBusy(true)
    setLint({ scope, ...(await onLint(scope)) })
    setBusy(false)
  }
  const fixable = [...new Set(issues.filter((i) => i.kind === "front-matter").map((i) => i.key))]
  const repairable = [...new Set(issues.filter((i) => i.dir && (i.kind === "index-ligne" || i.kind === "index-absent")).map((i) => i.dir!))]
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Santé de la mémoire</DialogTitle>
          <DialogDescription>
            {issues.length ? `${issues.length} point${issues.length > 1 ? "s" : ""} à vérifier. Rien n'est modifié sans votre clic.` : "Rien à signaler."}
          </DialogDescription>
        </DialogHeader>
        {(repairable.length > 0 || fixable.length > 0) && (
          <div className="flex flex-wrap gap-2">
            {fixable.length > 1 && (
              <Button size="xs" variant="outline" onClick={() => onFix(fixable)} title="Met entre guillemets les valeurs qui rendent le YAML invalide">
                <Wrench /> Corriger les {fixable.length} front matters
              </Button>
            )}
            {repairable.map((d) => (
              <Button key={d} size="xs" variant="outline" onClick={() => onRepair(d)} title="Retire les lignes mortes et ajoute les entrées manquantes de MEMORY.md">
                <Wrench /> Réparer l'index de {d}
              </Button>
            ))}
          </div>
        )}
        <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/30 p-2 text-xs">
          <Sparkles className="size-3.5 text-muted-foreground" />
          <span className="font-medium">Doublons, contradictions, périmé</span>
          <Choose value={scope} onChange={setScope} options={scopes} label="Portée de l'analyse" className="min-w-0 max-w-64" />
          <Button size="xs" onClick={analyse} disabled={busy} title="Haiku relit ces fichiers ; rien n'est modifié">
            {busy ? <Loader2 className="animate-spin" /> : <Sparkles />} Analyser avec Haiku
          </Button>
        </div>
        {lint && (
          <div className="space-y-1 text-sm">
            {lint.error ? (
              <p className="text-destructive">{lint.error}</p>
            ) : lint.findings?.length === 0 ? (
              <p className="text-muted-foreground">Aucun doublon ni contradiction trouvés ({lint.cost?.toFixed(4)} $).</p>
            ) : (
              lint.findings?.map((f, n) => (
                <div key={n} className="rounded-md border px-2 py-1.5">
                  <p className="text-xs font-medium text-muted-foreground">{f.kind}</p>
                  <p>{f.why}</p>
                  <p className="mt-1 flex flex-wrap gap-1">
                    {f.keys.map((k) => (
                      <Button key={k} size="xs" variant="outline" onClick={() => (onOpenKey(k), onClose())}>
                        {k.replace(/^g\/rules\//, "rules/").replace(/^m\/[^/]+\//, "")}
                      </Button>
                    ))}
                  </p>
                </div>
              ))
            )}
          </div>
        )}
        <ScrollArea className="max-h-96">
          <ul className="space-y-1 pr-3">
            {issues.map((i, n) => (
              <li key={n} className="flex items-start gap-1">
                <button onClick={() => (onOpenKey(i.key), onClose())} className="flex min-w-0 flex-1 items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted">
                  <AlertTriangle className={cn("mt-0.5 size-3.5 shrink-0", i.kind === "perime" ? "text-muted-foreground" : "text-amber-500")} />
                  <span className="min-w-0">
                    <span className="block text-xs font-medium text-muted-foreground">
                      {KINDS[i.kind] ?? i.kind} · {i.key}
                    </span>
                    <span className="break-words">{i.msg}</span>
                  </span>
                </button>
                {i.kind === "front-matter" && (
                  <Button size="xs" variant="outline" className="mt-1 shrink-0" onClick={() => onFix([i.key])} title="Met entre guillemets les valeurs qui rendent le YAML invalide">
                    <Wrench /> Corriger
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
