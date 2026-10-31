import { useEffect, useRef, useState } from "react"
import { ArrowUp, Folder, Home, Search } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { api, type Ls } from "@/lib/api"
import { join } from "@/lib/paths"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"

export type Pick = { title: string; start: string; children?: boolean; done: (path: string | null, children?: boolean) => void }

// Explorateur de dossiers (serveur : /ls). Un chemin disparu remonte au plus proche parent qui existe ;
// sous Windows « ↑ » depuis la racine d'un lecteur affiche la liste des lecteurs.
export function DirPicker({ pick, home }: { pick: Pick | null; home: string }) {
  const { t } = useTranslation()
  const [currentPath, setCurrentPath] = useState("")
  const [dirs, setDirs] = useState<string[]>([])
  const [parent, setParent] = useState("")
  const [separator, setSeparator] = useState("/")
  const [hidden, setHidden] = useState(false)
  const [typed, setTyped] = useState("")
  const [filter, setFilter] = useState("") // filtre des sous-dossiers du dossier affiché
  const [focusedDir, setFocusedDir] = useState("") // sous-dossier mis en évidence : le dossier d'où l'on part, vu depuis son parent
  const filterRef = useRef<HTMLInputElement>(null)

  const showListing = (listing: Ls, focused = "") => {
    if (listing.error) {
      toast.error(listing.error)
      return
    }
    setCurrentPath(listing.path)
    setTyped(listing.path)
    setFocusedDir(focused)
    setFilter("")
    filterRef.current?.focus() // on tape aussitôt pour filtrer le nouveau dossier
    setDirs(listing.dirs)
    setParent(listing.parent)
    setSeparator(listing.sep)
  }
  const navigateTo = (path: string) => api.ls(path).then(showListing)
  useEffect(() => {
    if (!pick) return
    let isStillMounted = true
    api.ls(pick.start || home).then(async (startListing) => {
      if (!isStillMounted) return
      // Le dossier de départ existe : on s'ouvre sur son parent, positionné sur lui.
      const trimTrailingSeparators = (path: string) => (path.length > 1 ? path.replace(/[\\/]+$/, "") : path)
      const startDirExists = pick.start && !startListing.error && startListing.parent && trimTrailingSeparators(startListing.path) === trimTrailingSeparators(pick.start)
      if (startDirExists) {
        const parentListing = await api.ls(startListing.parent)
        if (!isStillMounted) return
        if (!parentListing.error) return showListing(parentListing, trimTrailingSeparators(startListing.path).split(startListing.sep).pop() ?? "")
      }
      showListing(startListing)
    })
    return () => {
      isStillMounted = false
    }
  }, [pick, home])

  const lowerFilter = filter.toLowerCase()
  const shown = dirs.filter((dirName) => {
    const lowerDirName = dirName.toLowerCase()
    const isHiddenDir = dirName.startsWith(".")
    const filterTargetsHiddenDir = lowerFilter && lowerDirName.startsWith(lowerFilter)
    const isListable = hidden || dirName === focusedDir || !isHiddenDir || filterTargetsHiddenDir
    return isListable && lowerDirName.includes(lowerFilter)
  })
  const visible = dirs.filter((dirName) => !dirName.startsWith(".")) // ceux que « ajouter les sous-dossiers » enregistre
  const isDrives = currentPath === ""
  const close = (path: string | null) => pick?.done(path)
  const target = focusedDir ? join(currentPath, focusedDir, separator) : currentPath

  return (
    <Dialog open={!!pick} onOpenChange={(isOpen) => !isOpen && close(null)}>
      <DialogContent className="max-w-xl gap-3 sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{pick?.title}</DialogTitle>
          <DialogDescription className="sr-only">{t("shell.dirPicker.description")}</DialogDescription>
        </DialogHeader>
        <div className="flex gap-2">
          <Button variant="outline" size="icon" onClick={() => navigateTo(parent)} disabled={parent === ""} title={t("shell.dirPicker.parent")} aria-label={t("shell.dirPicker.parent")}>
            <ArrowUp />
          </Button>
          <Button variant="outline" size="icon" onClick={() => navigateTo(home)} title={t("shell.dirPicker.home")} aria-label={t("shell.dirPicker.home")}>
            <Home />
          </Button>
          <Input
            value={typed}
            spellCheck={false}
            className="font-mono text-xs"
            onChange={(changeEvent) => setTyped(changeEvent.target.value)}
            onKeyDown={(keyEvent) => keyEvent.key === "Enter" && navigateTo(typed.trim())}
            aria-label={t("shell.dirPicker.path")}
          />
        </div>
        <div className="flex items-center gap-2">
          <Checkbox id="hid" checked={hidden} onCheckedChange={(checkedState) => setHidden(checkedState === true)} />
          <Label htmlFor="hid" className="text-xs text-muted-foreground">
            {t("shell.dirPicker.showHidden")}
          </Label>
        </div>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={filterRef}
            value={filter}
            onChange={(changeEvent) => setFilter(changeEvent.target.value)}
            onKeyDown={(keyEvent) => {
              if (keyEvent.key === "Enter" && shown[0]) navigateTo(join(currentPath, shown[0], separator))
              if (keyEvent.key === "Escape" && filter) {
                keyEvent.preventDefault()
                setFilter("")
              }
            }}
            placeholder={t("shell.dirPicker.filterPlaceholder")}
            className="pl-8"
            aria-label={t("shell.dirPicker.filterLabel")}
            autoFocus
          />
        </div>
        <ScrollArea className="h-72 rounded-md border">
          <ul className="p-1">
            {shown.length === 0 && (
              <li className="px-3 py-2 text-xs text-muted-foreground">{filter ? t("shell.dirPicker.noMatch") : t("shell.dirPicker.empty")}</li>
            )}
            {shown.map((dirName) => {
              const isFocused = dirName === focusedDir
              return (
                <li key={dirName}>
                  <button
                    ref={isFocused ? (buttonElement) => buttonElement?.scrollIntoView({ block: "center" }) : undefined}
                    aria-current={isFocused ? "true" : undefined}
                    className={`flex w-full items-center gap-2 rounded-md px-3 py-1.5 text-left hover:bg-muted ${isFocused ? "bg-accent text-accent-foreground" : ""}`}
                    onClick={() => navigateTo(join(currentPath, dirName, separator))}
                  >
                    <Folder className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{dirName}</span>
                  </button>
                </li>
              )
            })}
          </ul>
        </ScrollArea>
        <DialogFooter>
          <Button variant="outline" onClick={() => close(null)}>
            {t("common.cancel")}
          </Button>
          {pick?.children && (
            <Button variant="outline" onClick={() => pick.done(currentPath, true)} disabled={isDrives || visible.length === 0} title={t("shell.dirPicker.addChildrenHint")}>
              {t("shell.dirPicker.addChildren", { count: visible.length })}
            </Button>
          )}
          <Button onClick={() => close(target)} disabled={isDrives}>
            {focusedDir ? t("shell.dirPicker.chooseNamed", { name: focusedDir }) : t("shell.dirPicker.choose")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
