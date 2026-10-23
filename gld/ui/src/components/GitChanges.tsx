import { useEffect, useState } from "react"
import { FileDiff } from "lucide-react"
import { api, type GitChange } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"

// Couleur de la lettre d'état : ajouté (vert), supprimé (rouge), conflit (rouge vif), non suivi (gris), modifié ou renommé (ambre).
const tone = (s: string) =>
  s.includes("U") || s === "AA" || s === "DD" ? "text-destructive" : s === "??" ? "text-muted-foreground" : s.includes("D") ? "text-red-500" : s.includes("A") ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400"

// Les fichiers en cours de modification d'un dépôt : un bouton ouvre la liste dans une fenêtre. La liste est lue à l'ouverture, et
// relue quand le nombre de changements annoncé par l'état git (relu toutes les 30 s) change. Le chemin du dépôt n'y est pas répété :
// il est déjà affiché à côté du bouton.
export function GitChanges({ dir, count }: { dir: string; count: number }) {
  const [files, setFiles] = useState<GitChange[]>([])
  const [more, setMore] = useState(0)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (!open || count === 0) return
    let live = true
    api.gitChanges(dir).then(
      (r) => {
        if (!live) return
        setFiles(r.files)
        setMore(r.more ?? 0)
      },
      () => {}
    )
    return () => {
      live = false
    }
  }, [dir, count, open])
  if (count === 0) return null
  const name = dir.split("/").filter(Boolean).pop() ?? dir
  return (
    <>
      <Button size="xs" variant="ghost" onClick={() => setOpen(true)} className="text-muted-foreground" title="Voir les fichiers modifiés">
        <FileDiff /> {count} fichier{count > 1 ? "s" : ""}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>
              {count} fichier{count > 1 ? "s" : ""} en cours de modification
            </DialogTitle>
            <DialogDescription>Dépôt « {name} » : fichiers modifiés, ajoutés, supprimés ou non suivis.</DialogDescription>
          </DialogHeader>
          <ScrollArea className="max-h-[60vh] rounded-md border">
            <ul className="space-y-px bg-muted/40 p-1 font-mono text-xs">
              {files.map((f) => (
                <li key={f.path} className="flex items-baseline gap-2 px-1.5 py-0.5">
                  <span className={cn("w-5 shrink-0 text-center font-semibold whitespace-pre", tone(f.status))} title={f.status}>
                    {f.status.trim() || " "}
                  </span>
                  <span className="min-w-0 flex-1 break-all" title={f.from ? `renommé depuis ${f.from}` : undefined}>
                    {f.path}
                    {f.from && <span className="text-muted-foreground"> ← {f.from}</span>}
                  </span>
                </li>
              ))}
              {more > 0 && <li className="px-1.5 py-0.5 text-muted-foreground">… et {more} autre{more > 1 ? "s" : ""}</li>}
            </ul>
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </>
  )
}
