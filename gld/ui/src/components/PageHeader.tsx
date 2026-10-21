import { FolderInput, Menu, MessageSquarePlus, MoreHorizontal, SquareTerminal, Trash2, TriangleAlert } from "lucide-react"
import type { Group, Item, VGroup } from "@/lib/api"
import { plural } from "@/lib/sessionsView"
import type { Route } from "@/lib/route"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import type { Actions, GroupActions } from "./actions"
import { AccountButton } from "./AccountDialog"
import { DD, GroupItems } from "./SessionMenu"

// En-tête de la page : titre, sous-titre, actions du dossier ou du groupe affiché, et avertissement d'un dossier disparu.
export function PageHeader({
  onMenu,
  title,
  sub,
  qt,
  route,
  g,
  vg,
  firstDir,
  own,
  rowCount,
  nGrouped,
  hideGrouped,
  setHideGrouped,
  actions,
  groupActs,
}: {
  onMenu: () => void // mobile : ouvre le menu
  title: string
  sub: string
  qt: string
  route: Route
  g: Group | null // dossier affiché
  vg: VGroup | undefined // groupe affiché
  firstDir: string | undefined // dossier de transcripts de la première session du dossier
  own: Item[] // sessions du dossier
  rowCount: number
  nGrouped: number
  hideGrouped: string
  setHideGrouped: (v: string) => void
  actions: Actions
  groupActs: GroupActions
}) {
  return (
    <>
      <header className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b px-3 py-3 md:px-6 md:py-4">
        <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={onMenu} aria-label="Ouvrir le menu" aria-controls="menu">
          <Menu />
        </Button>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-lg font-semibold tracking-tight">{title}</h2>
          <p className="truncate text-xs text-muted-foreground">
            {sub}
            {qt && (route.kind === "recent" || route.kind === "all" || route.kind === "active" || route.kind === "group" || route.kind === "dir") && ` · recherche « ${qt} »`}
          </p>
        </div>
        {g && nGrouped > 0 && (
          <label className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground select-none hover:text-foreground">
            <Checkbox checked={!!hideGrouped} onCheckedChange={(c) => setHideGrouped(c === true ? "1" : "")} />
            Masquer les sessions groupées ({nGrouped})
          </label>
        )}
        {g && !g.missing && (
          <Button variant="outline" size="sm" onClick={() => actions.newSession(g.project)} title="Nouvelle session Claude dans ce dossier">
            <MessageSquarePlus /> Nouvelle session
          </Button>
        )}
        {g && !g.missing && (
          <Button variant="outline" size="sm" onClick={() => actions.open({ dir: firstDir! })} title="Un onglet de terminal par session des 7 derniers jours">
            <SquareTerminal /> Reprendre les récentes
          </Button>
        )}
        {vg && rowCount > 0 && (
          <>
            <Button variant="outline" size="sm" onClick={() => groupActs.prompt(vg)} title="Nouvelle session avec les références de ces sessions">
              <MessageSquarePlus /> Nouvelle session
            </Button>
            <Button variant="outline" size="sm" onClick={() => groupActs.open(vg)} title="Un onglet de terminal par session (hors sessions déjà ouvertes)">
              <SquareTerminal /> Tout reprendre
            </Button>
          </>
        )}
        {(g || vg) && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon-sm" aria-label={g ? "Actions du projet" : "Actions du groupe"}>
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-64">
              {g && (
                <>
                  <DropdownMenuItem onSelect={() => actions.moveGroup(g, own)}>
                    <FolderInput /> Réassigner les {plural(own.length)}…
                  </DropdownMenuItem>
                  <DropdownMenuItem variant="destructive" onSelect={() => actions.remove(own, `les ${plural(own.length)} de ${g.project}`)}>
                    <Trash2 /> Supprimer les {plural(own.length)} du projet
                  </DropdownMenuItem>
                </>
              )}
              {vg && <GroupItems M={DD} vg={vg} page />}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
        <AccountButton />
      </header>

      {g?.missing && (
        <div className="flex items-center gap-3 border-b bg-amber-500/10 px-6 py-3 text-sm">
          <TriangleAlert className="size-4 shrink-0 text-amber-600 dark:text-amber-400" />
          <p className="flex-1">Ce dossier n'existe plus : ses sessions ne peuvent pas être reprises tant qu'elles ne sont pas rattachées à un dossier existant.</p>
          <Button size="sm" variant="outline" onClick={() => actions.moveGroup(g, own)}>
            Réassigner…
          </Button>
        </div>
      )}
    </>
  )
}
