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

export type Pick = { title: string; start: string; done: (path: string | null) => void }

// Explorateur de dossiers (serveur : /ls). Un chemin disparu remonte au plus proche parent qui existe ;
// sous Windows « ↑ » depuis la racine d'un lecteur affiche la liste des lecteurs.
export function DirPicker({ pick, home }: { pick: Pick | null; home: string }) {
  const { t } = useTranslation()
  const [cur, setCur] = useState("")
  const [dirs, setDirs] = useState<string[]>([])
  const [parent, setParent] = useState("")
  const [sep, setSep] = useState("/")
  const [hidden, setHidden] = useState(false)
  const [typed, setTyped] = useState("")
  const [filter, setFilter] = useState("") // filtre des sous-dossiers du dossier affiché
  const [focus, setFocus] = useState("") // sous-dossier mis en évidence : le dossier d'où l'on part, vu depuis son parent
  const filterRef = useRef<HTMLInputElement>(null)

  const show = (r: Ls, focused = "") => {
    if (r.error) {
      toast.error(r.error)
      return
    }
    setCur(r.path)
    setTyped(r.path)
    setFocus(focused)
    setFilter("")
    filterRef.current?.focus() // on tape aussitôt pour filtrer le nouveau dossier
    setDirs(r.dirs)
    setParent(r.parent)
    setSep(r.sep)
  }
  const go = (p: string) => api.ls(p).then(show)
  useEffect(() => {
    if (!pick) return
    let live = true
    api.ls(pick.start || home).then(async (r) => {
      if (!live) return
      // Le dossier de départ existe : on s'ouvre sur son parent, positionné sur lui.
      const trim = (x: string) => (x.length > 1 ? x.replace(/[\\/]+$/, "") : x)
      if (pick.start && !r.error && r.parent && trim(r.path) === trim(pick.start)) {
        const p = await api.ls(r.parent)
        if (!live) return
        if (!p.error) return show(p, trim(r.path).split(r.sep).pop() ?? "")
      }
      show(r)
    })
    return () => {
      live = false
    }
  }, [pick, home])

  const f = filter.toLowerCase()
  const shown = dirs.filter((d) => (hidden || d === focus || !d.startsWith(".") || (f && d.toLowerCase().startsWith(f))) && d.toLowerCase().includes(f))
  const isDrives = cur === ""
  const close = (path: string | null) => pick?.done(path)
  const target = focus ? join(cur, focus, sep) : cur

  return (
    <Dialog open={!!pick} onOpenChange={(o) => !o && close(null)}>
      <DialogContent className="max-w-xl gap-3 sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{pick?.title}</DialogTitle>
          <DialogDescription className="sr-only">{t("shell.dirPicker.description")}</DialogDescription>
        </DialogHeader>
        <div className="flex gap-2">
          <Button variant="outline" size="icon" onClick={() => go(parent)} disabled={parent === ""} title={t("shell.dirPicker.parent")}>
            <ArrowUp />
          </Button>
          <Button variant="outline" size="icon" onClick={() => go(home)} title={t("shell.dirPicker.home")}>
            <Home />
          </Button>
          <Input
            value={typed}
            spellCheck={false}
            className="font-mono text-xs"
            onChange={(e) => setTyped(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && go(typed.trim())}
            aria-label={t("shell.dirPicker.path")}
          />
        </div>
        <div className="flex items-center gap-2">
          <Checkbox id="hid" checked={hidden} onCheckedChange={(c) => setHidden(c === true)} />
          <Label htmlFor="hid" className="text-xs text-muted-foreground">
            {t("shell.dirPicker.showHidden")}
          </Label>
        </div>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={filterRef}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && shown[0]) go(join(cur, shown[0], sep))
              if (e.key === "Escape" && filter) {
                e.preventDefault()
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
            {shown.map((d) => (
              <li key={d}>
                <button
                  ref={d === focus ? (el) => el?.scrollIntoView({ block: "center" }) : undefined}
                  aria-current={d === focus ? "true" : undefined}
                  className={`flex w-full items-center gap-2 rounded-md px-3 py-1.5 text-left hover:bg-muted ${d === focus ? "bg-accent text-accent-foreground" : ""}`}
                  onClick={() => go(join(cur, d, sep))}
                >
                  <Folder className="size-4 shrink-0 text-muted-foreground" />
                  <span className="truncate">{d}</span>
                </button>
              </li>
            ))}
          </ul>
        </ScrollArea>
        <DialogFooter>
          <Button variant="outline" onClick={() => close(null)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => close(target)} disabled={isDrives}>
            {focus ? t("shell.dirPicker.chooseNamed", { name: focus }) : t("shell.dirPicker.choose")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
