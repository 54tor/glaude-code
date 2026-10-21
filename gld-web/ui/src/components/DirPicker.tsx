import { useEffect, useRef, useState } from "react"
import { ArrowUp, Folder, Home, Search } from "lucide-react"
import { toast } from "sonner"
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
  const [cur, setCur] = useState("")
  const [dirs, setDirs] = useState<string[]>([])
  const [parent, setParent] = useState("")
  const [sep, setSep] = useState("/")
  const [hidden, setHidden] = useState(false)
  const [typed, setTyped] = useState("")
  const [filter, setFilter] = useState("") // filtre des sous-dossiers du dossier affiché
  const filterRef = useRef<HTMLInputElement>(null)

  const show = (r: Ls) => {
    if (r.error) {
      toast.error(r.error)
      return
    }
    setCur(r.path)
    setTyped(r.path)
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
    api.ls(pick.start || home).then((r) => live && show(r))
    return () => {
      live = false
    }
  }, [pick, home])

  const f = filter.toLowerCase()
  const shown = dirs.filter((d) => (hidden || !d.startsWith(".") || (f && d.toLowerCase().startsWith(f))) && d.toLowerCase().includes(f))
  const isDrives = cur === ""
  const close = (path: string | null) => pick?.done(path)

  return (
    <Dialog open={!!pick} onOpenChange={(o) => !o && close(null)}>
      <DialogContent className="max-w-xl gap-3 sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{pick?.title}</DialogTitle>
          <DialogDescription className="sr-only">Choisir un dossier existant</DialogDescription>
        </DialogHeader>
        <div className="flex gap-2">
          <Button variant="outline" size="icon" onClick={() => go(parent)} disabled={parent === ""} title="dossier parent">
            <ArrowUp />
          </Button>
          <Button variant="outline" size="icon" onClick={() => go(home)} title="dossier personnel">
            <Home />
          </Button>
          <Input
            value={typed}
            spellCheck={false}
            className="font-mono text-xs"
            onChange={(e) => setTyped(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && go(typed.trim())}
            aria-label="Chemin"
          />
        </div>
        <div className="flex items-center gap-2">
          <Checkbox id="hid" checked={hidden} onCheckedChange={(c) => setHidden(c === true)} />
          <Label htmlFor="hid" className="text-xs text-muted-foreground">
            afficher les dossiers cachés
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
            placeholder="Filtrer les dossiers…"
            className="pl-8"
            aria-label="Filtrer les sous-dossiers"
            autoFocus
          />
        </div>
        <ScrollArea className="h-72 rounded-md border">
          <ul className="p-1">
            {shown.length === 0 && (
              <li className="px-3 py-2 text-xs text-muted-foreground">{filter ? "(aucun dossier ne correspond)" : "(aucun sous-dossier)"}</li>
            )}
            {shown.map((d) => (
              <li key={d}>
                <button
                  className="flex w-full items-center gap-2 rounded-md px-3 py-1.5 text-left hover:bg-muted"
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
            Annuler
          </Button>
          <Button onClick={() => close(cur)} disabled={isDrives}>
            Choisir ce dossier
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
