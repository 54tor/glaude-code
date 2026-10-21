import { useEffect, useState } from "react"
import { Brain, ChevronRight, FileText, ScrollText } from "lucide-react"
import { Link } from "wouter"
import { api, type MemFile } from "@/lib/api"
import { href } from "@/lib/route"
import { cn } from "@/lib/utils"

const icon = (key: string) => (key.startsWith("m/") ? Brain : key.startsWith("r/") ? ScrollText : FileText)

// La mémoire locale d'un projet ouvert dans la liste des sessions : entrées de mémoire, règles .claude/rules, CLAUDE.md, AGENTS.md…
// Un clic ouvre le fichier dans la page Mémoire. Relue au retour sur l'onglet.
export function ProjectMemory({ dir }: { dir: string }) {
  const [files, setFiles] = useState<MemFile[] | null>(null)
  const [open, setOpen] = useState(true)
  useEffect(() => {
    let live = true
    const load = () => api.memory().then((r) => live && setFiles(r.projects.find((p) => p.dir === dir)?.files ?? []), () => {})
    load()
    const on = () => document.visibilityState === "visible" && load()
    document.addEventListener("visibilitychange", on)
    return () => {
      live = false
      document.removeEventListener("visibilitychange", on)
    }
  }, [dir])
  if (!files || files.length === 0) return null
  const tokens = files.filter((f) => f.always).reduce((n, f) => n + f.tokens, 0)
  const sorted = [...files].sort((a, b) => +!!b.always - +!!a.always || a.name.localeCompare(b.name, "fr"))
  return (
    <section className="mb-4 rounded-lg border">
      <button onClick={() => setOpen(!open)} className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm" aria-expanded={open}>
        <ChevronRight className={cn("size-4 shrink-0 transition-transform", open && "rotate-90")} />
        <Brain className="size-4 shrink-0 text-muted-foreground" />
        <span className="font-medium">Mémoire du projet</span>
        <span className="text-xs text-muted-foreground">
          {files.length} fichier{files.length > 1 ? "s" : ""}
          {tokens > 0 && ` · ≈ ${tokens.toLocaleString("fr-FR")} tokens chargés à chaque session`}
        </span>
      </button>
      {open && (
        <ul className="grid gap-px border-t p-1 md:grid-cols-2">
          {sorted.map((f) => {
            const Icon = icon(f.key)
            return (
              <li key={f.key}>
                <Link href={href({ kind: "memory", key: f.key })} className="flex items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted" title={f.path}>
                  <Icon className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className={cn("min-w-0 flex-1 truncate", f.off && "line-through opacity-60")}>{f.name}</span>
                  {f.type && <span className="shrink-0 rounded bg-muted px-1.5 text-[10px] text-muted-foreground">{f.type}</span>}
                  {f.paths && <span className="shrink-0 text-[10px] text-muted-foreground">scopée</span>}
                  <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">{f.always ? `${f.tokens} tok.` : "à la demande"}</span>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
