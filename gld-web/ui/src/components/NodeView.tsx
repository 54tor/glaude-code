import { ChevronRight } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { sorted, type Node } from "@/lib/tree"
import { useActions } from "./actions"
import { SessionRow } from "./SessionRow"

// Un dossier de l'arbre : projet (avec ses sessions) et/ou dossier parent de sous-projets.
export function NodeView({ n }: { n: Node }) {
  const a = useActions()
  const open = a.filtering || a.opened.has(n.key)
  const g = n.g
  const all = n.ss.map((s) => ({ dir: s.dir, id: s.id }))
  const stop = (fn: () => void) => (e: React.MouseEvent) => {
    e.preventDefault()
    fn()
  }
  return (
    <Collapsible open={open} onOpenChange={() => a.toggle(n.key)} className="rounded-lg border bg-card">
      <div className="flex items-center gap-2 px-3 py-2">
        <CollapsibleTrigger asChild>
          <button className="group/trigger flex min-w-0 flex-1 items-center gap-2 text-left">
            <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-data-[state=open]/trigger:rotate-90" />
            <span className={`truncate ${g ? "font-medium" : "text-muted-foreground"}`} title={g ? g.project : n.name}>
              {n.name}
            </span>
          </button>
        </CollapsibleTrigger>
        {g?.missing && <Badge variant="destructive">dossier introuvable</Badge>}
        {g?.missing && (
          <Button variant="outline" size="xs" onClick={stop(() => a.moveGroup(g, all))} title="déplacer ces sessions vers un dossier qui existe">
            Réassigner…
          </Button>
        )}
        {g && !g.missing && (
          <Button variant="outline" size="xs" onClick={stop(() => a.open({ dir: n.ss[0].dir }))} title="gld-matin : un onglet de terminal par session récente">
            Rouvrir (7 j)
          </Button>
        )}
        {g && (
          <Button variant="destructive" size="xs" onClick={stop(() => a.remove(all, `les ${all.length} session(s) de ${g.project}`))}>
            Tout supprimer
          </Button>
        )}
        <Badge variant="outline">
          {n.count} session{n.count > 1 ? "s" : ""}
        </Badge>
      </div>
      <CollapsibleContent>
        {g && (
          <div className="border-t">
            {n.ss.map((s) => (
              <SessionRow key={s.id} s={s} />
            ))}
          </div>
        )}
        {n.kids.size > 0 && (
          <div className="space-y-2 border-t p-2">
            {sorted(n).map((c) => (
              <NodeView key={c.key} n={c} />
            ))}
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}
