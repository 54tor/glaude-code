import { useState } from "react"
import { Copy, Download, FilePen, Upload } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

type Doc = Record<string, unknown> | undefined

// Le fragment de settings.json qui contient les règles : {"permissions": {"allow": […], "ask": […], "deny": […]}}.
// Il peut être gardé, partagé, ou collé dans un autre fichier de réglages. defaultMode n'en fait pas partie.
export function permsFragment(doc: Doc): string {
  const p = (doc?.permissions ?? {}) as Record<string, unknown>
  const out: Record<string, string[]> = {}
  for (const k of ["allow", "ask", "deny"]) if (Array.isArray(p[k]) && (p[k] as unknown[]).length) out[k] = (p[k] as unknown[]).filter((x): x is string => typeof x === "string")
  return JSON.stringify({ permissions: out }, null, 2) + "\n"
}

export function PermIOButtons({ file, label, doc, onImport, onRaw }: { file: string; label: string; doc: Doc; onImport: () => void; onRaw: () => void }) {
  const count = ["allow", "ask", "deny"].reduce((n, k) => n + (Array.isArray((doc?.permissions as Record<string, unknown> | undefined)?.[k]) ? ((doc!.permissions as Record<string, unknown[]>)[k]).length : 0), 0)
  const download = () => {
    const a = document.createElement("a")
    a.href = URL.createObjectURL(new Blob([permsFragment(doc)], { type: "application/json" }))
    a.download = `permissions-${file === "u" ? "utilisateur" : label.replace(/[^\w.-]+/g, "-").slice(0, 40)}.json`
    a.click()
    URL.revokeObjectURL(a.href)
  }
  const copy = () =>
    navigator.clipboard.writeText(permsFragment(doc)).then(
      () => toast.success("Règles copiées"),
      () => toast.error("Copie impossible")
    )
  return (
    <div className="mb-4 flex flex-wrap items-center gap-2">
      <Button size="sm" variant="outline" onClick={download} disabled={count === 0} title="Télécharge les règles de ce fichier en JSON">
        <Download /> Exporter ({count})
      </Button>
      <Button size="sm" variant="ghost" onClick={copy} disabled={count === 0}>
        <Copy /> Copier
      </Button>
      <Button size="sm" variant="outline" onClick={onRaw} title="Modifie toutes les règles en texte, avec vérification de la syntaxe">
        <FilePen /> Éditer en texte…
      </Button>
      <Button size="sm" variant="outline" onClick={onImport} title="Ajoute ou remplace les règles de ce fichier d'après un export">
        <Upload /> Importer…
      </Button>
    </div>
  )
}

// Importer des règles : un fichier exporté, ou un fragment collé. Fusionner ajoute ce qui manque ; Remplacer substitue les
// listes présentes dans le fragment. Un aperçu des différences précède toute écriture.
export function PermImportDialog({
  open,
  file,
  label,
  propose,
  onClose,
}: {
  open: boolean
  file: string
  label: string
  propose: (file: string, text: string, mode: string) => Promise<string | null>
  onClose: () => void
}) {
  const [text, setText] = useState("")
  const [mode, setMode] = useState<"merge" | "replace">("merge")
  const [error, setError] = useState("")
  const load = async (f: File | undefined) => {
    if (!f) return
    if (f.size > 1 << 20) return void setError("Fichier trop gros")
    setText(await f.text())
    setError("")
  }
  const preview = async () => {
    const err = await propose(file, text, mode)
    if (err) setError(err)
    else onClose()
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Importer des règles de permission</DialogTitle>
          <DialogDescription>Vers {label}. Le mode par défaut (defaultMode) n'est jamais importé.</DialogDescription>
        </DialogHeader>
        <input type="file" accept=".json,application/json" onChange={(e) => load(e.target.files?.[0])} aria-label="Fichier à importer" className="text-sm file:mr-3 file:rounded-md file:border file:bg-muted file:px-3 file:py-1" />
        <textarea
          value={text}
          onChange={(e) => (setText(e.target.value), setError(""))}
          placeholder={'ou collez ici : {"permissions": {"allow": ["Bash(git status:*)"]}}'}
          spellCheck={false}
          aria-label="Règles à importer"
          className="h-56 w-full resize-y rounded-md border bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        />
        <div className="flex flex-wrap items-center gap-2 text-sm" role="radiogroup" aria-label="Mode d'import">
          <Button size="sm" variant={mode === "merge" ? "secondary" : "ghost"} role="radio" aria-checked={mode === "merge"} onClick={() => setMode("merge")}>
            Fusionner
          </Button>
          <Button size="sm" variant={mode === "replace" ? "secondary" : "ghost"} role="radio" aria-checked={mode === "replace"} onClick={() => setMode("replace")}>
            Remplacer
          </Button>
          <span className="text-xs text-muted-foreground">{mode === "merge" ? "Ajoute les règles qui manquent, ne retire rien." : "Les listes allow, ask ou deny présentes dans l'import remplacent celles du fichier."}</span>
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Annuler
          </Button>
          <Button onClick={preview} disabled={!text.trim()}>
            Aperçu
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
