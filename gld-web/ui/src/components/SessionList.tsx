import { Copy, FolderInput, MoreHorizontal, SquareTerminal, Trash2 } from "lucide-react"
import type { Group, Session } from "@/lib/api"
import { ago, bucket, full, size, tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useActions } from "./actions"

// Menu des actions secondaires d'une session (aussi utilisé par le lecteur).
export function SessionMenu({ s, after, children }: { s: Session; after?: () => void; children: React.ReactNode }) {
  const a = useActions()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-60">
        <DropdownMenuItem onSelect={() => a.copy(s)}>
          <Copy /> Copier « claude --resume »
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => a.move(s)}>
          <FolderInput /> Déplacer vers un autre dossier…
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => a.remove([{ dir: s.dir, id: s.id }], `« ${s.title} »`, after)}>
          <Trash2 /> Supprimer
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function Row({ s, g, showProject }: { s: Session; g: Group; showProject: boolean }) {
  const a = useActions()
  return (
    <li className="group relative flex items-center gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-muted/60 has-[[data-state=open]]:bg-muted/60">
      <button
        className="min-w-0 flex-1 text-left outline-none after:absolute after:inset-0 after:rounded-lg focus-visible:after:ring-2 focus-visible:after:ring-ring/50"
        onClick={() => a.read(s)}
      >
        <span className={cn("line-clamp-1 text-sm", s.named ? "font-medium" : "text-foreground/80")}>{s.title}</span>
        <span className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <span title={full(s.mtime)} className="shrink-0">
            {ago(s.mtime)}
          </span>
          <span aria-hidden>·</span>
          <span className="shrink-0 tabular-nums">{size(s.kb)}</span>
          {showProject && (
            <>
              <span aria-hidden>·</span>
              <span className="truncate">{tilde(g.project, a.home)}</span>
            </>
          )}
        </span>
      </button>
      {/* Actions au-dessus du bouton étiré : visibles au survol, au focus clavier, ou menu ouvert. */}
      <div className="relative flex items-center gap-1 transition-opacity md:opacity-0 md:group-focus-within:opacity-100 md:group-hover:opacity-100 md:has-[[data-state=open]]:opacity-100">
        {!g.missing && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="icon-sm" onClick={() => a.open({ dir: s.dir, id: s.id })} aria-label="Reprendre dans un terminal">
                <SquareTerminal />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Reprendre dans un terminal</TooltipContent>
          </Tooltip>
        )}
        <SessionMenu s={s}>
          <Button variant="ghost" size="icon-sm" aria-label="Plus d'actions">
            <MoreHorizontal />
          </Button>
        </SessionMenu>
      </div>
    </li>
  )
}

// Sessions triées de la plus récente à la plus ancienne, regroupées par période.
export function SessionList({ items, showProject }: { items: { s: Session; g: Group }[]; showProject: boolean }) {
  const groups: [string, typeof items][] = []
  for (const it of items) {
    const b = bucket(it.s.mtime)
    if (groups.at(-1)?.[0] !== b) groups.push([b, []])
    groups.at(-1)![1].push(it)
  }
  return (
    <div className="space-y-6">
      {groups.map(([b, list]) => (
        <section key={b}>
          <h3 className="sticky top-0 z-10 bg-background/95 px-3 py-1.5 text-xs font-medium text-muted-foreground backdrop-blur">{b}</h3>
          <ul className="space-y-px">
            {list.map(({ s, g }) => (
              <Row key={s.id} s={s} g={g} showProject={showProject} />
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}
