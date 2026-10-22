import { useEffect, useState } from "react"
import { useOpen } from "@/hooks/useOpen"
import { ChevronRight } from "lucide-react"
import { api, type GitChange } from "@/lib/api"
import { cn } from "@/lib/utils"

// Couleur de la lettre d'état : ajouté (vert), supprimé (rouge), conflit (rouge vif), non suivi (gris), modifié ou renommé (ambre).
const tone = (s: string) =>
  s.includes("U") || s === "AA" || s === "DD" ? "text-destructive" : s === "??" ? "text-muted-foreground" : s.includes("D") ? "text-red-500" : s.includes("A") ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400"

// Les fichiers en cours de modification d'un dépôt, repliés sous sa ligne d'état. Relus quand le nombre de changements annoncé
// par l'état git (relu toutes les 30 s) change.
export function GitChanges({ dir, count }: { dir: string; count: number }) {
  const [files, setFiles] = useState<GitChange[]>([])
  const [more, setMore] = useState(0)
  const [open, flip] = useOpen("g/" + dir)
  useEffect(() => {
    if (count === 0) return
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
  }, [dir, count])
  if (count === 0) return null
  return (
    <div className="basis-full">
      <button onClick={flip} aria-expanded={open} className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
        <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        {count} fichier{count > 1 ? "s" : ""} en cours de modification
      </button>
      {open && (
        <ul className="mt-1 max-h-72 space-y-px overflow-y-auto rounded-md bg-muted/40 p-1 font-mono text-xs">
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
      )}
    </div>
  )
}
