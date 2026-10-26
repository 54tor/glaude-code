import { Menu, MessageSquarePlus, MoreHorizontal, Trash2, TriangleAlert, RefreshCcwDot } from "lucide-react"
import { McpStatus } from "./McpStatus"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import type { Group, Item, VGroup } from "@/lib/api"
import type { Route } from "@/lib/route"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import type { Actions, GroupActions } from "./actions"
import { AccountButton } from "./AccountDialog"
import { UserMenu } from "./auth/Profile"
import { useCan } from "@/lib/auth"
import { DD, GroupItems } from "./SessionMenu"
import { SessionTrash } from "./SessionTrash"
import { cn } from "@/lib/utils"

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
  split,
  reload,
  searchBox,
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
  reload: () => Promise<void>
  searchBox?: React.ReactNode // champ de recherche de la vue
  split?: boolean // vue en deux colonnes pleine largeur : l'en-tête suit leur bord gauche plutôt que le conteneur centré
}) {
  const { t } = useTranslation()
  const [trashOpen, setTrashOpen] = useState(false)
  const can = useCan()
  const exec = can("exec") // sans ce droit sur le home, sessions et terminal sont refusés : on ne les propose pas
  // Même axe que le contenu de la vue : le titre ne reste pas collé à gauche quand la liste est centrée sur grand écran.
  const box = cn("flex flex-wrap items-center gap-x-3 px-3 md:px-6", !split && "mx-auto w-full max-w-7xl")
  return (
    <>
      <header className="border-b bg-background">
        <div className={cn(box, "gap-y-2 py-3 md:py-4")}>
          <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={onMenu} aria-label={t("shell.header.openMenu")} aria-controls="menu">
            <Menu />
          </Button>
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-xl font-semibold tracking-tight">{title}</h2>
            <p className="truncate text-sm text-muted-foreground">
              {sub}
              {qt && (route.kind === "recent" || route.kind === "all" || route.kind === "active" || route.kind === "project" || route.kind === "group" || route.kind === "dir") && t("shell.header.search", { q: qt })}
            </p>
          </div>
          {/* Petit écran : avec le titre sur la première ligne ; grand écran : en fin de ligne. */}
          <div className="flex items-center gap-1 md:order-last">
            <AccountButton />
            <UserMenu />
          </div>
          {searchBox}
          {g && nGrouped > 0 && (
            <label className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground select-none hover:text-foreground">
              <Checkbox checked={!!hideGrouped} onCheckedChange={(c) => setHideGrouped(c === true ? "1" : "")} />
              {t("shell.header.hideGrouped", { n: nGrouped })}
            </label>
          )}
          {g && !g.missing && exec && (
            <Button size="sm" onClick={() => actions.newSession(g.project)} title={t("shell.header.newInDirHint")}>
              <MessageSquarePlus /> {t("shell.header.newSession")}
            </Button>
          )}
          {g && !g.missing && exec && (
            <Button variant="outline" size="sm" onClick={() => actions.open({ dir: firstDir! })} title={t("shell.header.resumeRecentHint")}>
              <RefreshCcwDot /> {t("shell.tree.resumeRecent")}
            </Button>
          )}
          {g && !g.missing && firstDir && can("config_write") && <McpStatus dir={firstDir} />}
          {vg && rowCount > 0 && exec && (
            <>
              <Button size="sm" onClick={() => groupActs.prompt(vg)} title={t("shell.header.newWithRefsHint")}>
                <MessageSquarePlus /> {t("shell.header.newSession")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => groupActs.open(vg)} title={t("shell.header.resumeAllHint")}>
                <RefreshCcwDot /> {t("shell.header.resumeAll")}
              </Button>
            </>
          )}
          {vg && exec && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon-sm" aria-label={t("shell.header.groupActions")}>
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-64">
                <GroupItems M={DD} vg={vg} page />
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          {route.kind === "all" && (
            <>
              <Button variant="ghost" size="sm" onClick={() => setTrashOpen(true)}>
                <Trash2 /> <span className="max-md:sr-only">{t("shell.sidebar.trash")}</span>
              </Button>
              <SessionTrash open={trashOpen} onClose={() => setTrashOpen(false)} reload={reload} />
            </>
          )}
        </div>
      </header>

      {g?.missing && (
        <div className="border-b bg-warning/10 text-sm">
          <div className={cn(box, "gap-3 py-3")}>
            <TriangleAlert className="size-4 shrink-0 text-warning" />
            <p className="flex-1">{t("shell.header.missing")}</p>
            <Button size="sm" variant="outline" onClick={() => actions.moveGroup(g, own)}>
              {t("shell.header.reassignShort")}
            </Button>
          </div>
        </div>
      )}
    </>
  )
}
