import { useEffect, useState } from "react"
import { NotebookPen } from "lucide-react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { notesOf } from "./Notes"

// Clés des bloc-notes non vides, partagées par toutes les lignes de la barre latérale (une seule requête par rafraîchissement).
let filled = new Set<string>()
const subs = new Set<(keys: Set<string>) => void>()
const hasContent = (noteValue: Parameters<typeof notesOf>[0]) => notesOf(noteValue).some((note) => note.trim())
const load = () =>
  api.notes().then(
    (response) => {
      const filledEntries = Object.entries(response.notes).filter(([, noteValue]) => hasContent(noteValue))
      filled = new Set(filledEntries.map(([noteKey]) => noteKey))
      subs.forEach((notifySubscriber) => notifySubscriber(filled))
    },
    () => {}
  )

// Icône bloc-notes à côté du nombre de sessions : place réservée pour que les colonnes restent alignées.
export function NoteMark({ noteKey }: { noteKey: string }) {
  const { t } = useTranslation()
  const [keys, setKeys] = useState(filled)
  useEffect(() => {
    subs.add(setKeys)
    if (subs.size === 1) {
      void load()
      window.addEventListener("gld:notes", load) // émis par Notes après chaque enregistrement
    }
    return () => {
      subs.delete(setKeys)
      if (!subs.size) window.removeEventListener("gld:notes", load)
    }
  }, [])
  if (!keys.has(noteKey)) return <span className="w-4 shrink-0" aria-hidden />
  return <NotebookPen className="size-4 shrink-0 text-muted-foreground" aria-label={t("shell.notes.title")} />
}
