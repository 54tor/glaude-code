import { useCallback, useEffect, useState } from "react"
import { NotebookPen } from "lucide-react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { tilde } from "@/lib/format"
import { segs } from "@/lib/paths"
import type { Route } from "@/lib/route"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Tip } from "@/components/ui/tooltip"
import { notesOf } from "./Notes"
import { useActions } from "./actions"

// Bloc-notes non vides (un par dossier ou par projet), dans l'en-tête de la barre latérale : absent s'il n'y en a aucun.
export function NotesCount({ go }: { go: (r: Route) => void }) {
  const { t } = useTranslation()
  const a = useActions()
  const [notes, setNotes] = useState<[string, string[]][]>([])
  const load = useCallback(() => api.notes().then((r) => setNotes(Object.entries(r.notes).map(([k, v]) => [k, notesOf(v).filter((x) => x.trim())] as [string, string[]]).filter(([, v]) => v.length)), () => {}), [])
  useEffect(() => {
    void load()
    window.addEventListener("gld:notes", load) // émis par Notes après chaque enregistrement
    return () => window.removeEventListener("gld:notes", load)
  }, [load])
  if (!notes.length) return null
  const open = (key: string) => (key.startsWith("p/") ? go({ kind: "project", name: key.slice(2) }) : go({ kind: "dir", key: segs(key.slice(2), a.home).join("/") }))
  return (
    <DropdownMenu>
      <Tip label={t("shell.notes.title")}>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" className="relative size-9 text-muted-foreground" aria-label={t("shell.notes.count", { count: notes.length })}>
            <NotebookPen className="size-6" />
            <span className="absolute top-0.5 right-0.5 grid min-w-4 place-items-center rounded-full bg-primary px-1 text-2xs leading-4 font-medium text-primary-foreground tabular-nums">{notes.length}</span>
          </Button>
        </DropdownMenuTrigger>
      </Tip>
      <DropdownMenuContent align="start" className="w-72">
        <DropdownMenuLabel>{t("shell.notes.title")}</DropdownMenuLabel>
        {notes.map(([key, items]) => (
          <DropdownMenuItem key={key} onSelect={() => open(key)} className="flex-col items-start gap-0">
            <span className="w-full truncate font-medium">{key.startsWith("p/") ? key.slice(2) : tilde(key.slice(2), a.home)}</span>
            <span className="w-full truncate text-xs text-muted-foreground">{t("shell.notes.lines", { count: items.length })} · {items[0]}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
