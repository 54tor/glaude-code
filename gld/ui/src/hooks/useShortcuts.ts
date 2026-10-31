import { useEffect, type RefObject } from "react"
import { i18n } from "@/i18n"
import { isEditableTarget } from "@/lib/editable"
import type { Actions, Row } from "@/components/actions"

const OPEN_OVERLAY_SELECTOR = '[role="dialog"],[role="alertdialog"],[role="menu"]'
const ROW_SELECTOR = "main [data-row]"
const NAVIGATION_KEYS = ["j", "k", "ArrowDown", "ArrowUp"]

// Raccourcis clavier, hors champ de saisie et hors fenêtre ou menu ouvert : / (recherche), j/k ou flèches (ligne),
// t (terminal), m (reprendre avec un message), r (renommer), x (cocher), e (exporter), Suppr, Échap (vider la sélection).
export function useShortcuts({
  search,
  picked,
  clearPicked,
  byId,
  actions,
}: {
  search: RefObject<HTMLInputElement | null>
  picked: Set<string>
  clearPicked: () => void
  byId: Map<string, Row>
  actions: Actions
}) {
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const hasModifier = event.ctrlKey || event.metaKey || event.altKey
      if (isEditableTarget(event.target) || hasModifier) return
      if (document.querySelector(OPEN_OVERLAY_SELECTOR)) return
      if (event.key === "/") {
        event.preventDefault()
        search.current?.focus()
        return
      }
      if (event.key === "Escape" && picked.size) return clearPicked()
      const rowElements = [...document.querySelectorAll<HTMLElement>(ROW_SELECTOR)]
      const focusedIndex = rowElements.indexOf(document.activeElement as HTMLElement)
      if (NAVIGATION_KEYS.includes(event.key)) {
        event.preventDefault()
        const step = event.key === "j" || event.key === "ArrowDown" ? 1 : -1
        const wantedIndex = focusedIndex < 0 ? 0 : focusedIndex + step
        const clampedIndex = Math.max(0, Math.min(rowElements.length - 1, wantedIndex))
        const nextRow = rowElements[clampedIndex]
        nextRow?.focus()
        nextRow?.scrollIntoView({ block: "nearest" })
        return
      }
      if (focusedIndex < 0) return
      const row = byId.get(rowElements[focusedIndex].dataset.id!)
      if (!row) return
      const item = { dir: row.s.dir, id: row.s.id }
      if (event.key === "t") actions.open(item)
      else if (event.key === "m") actions.resumeWith(row)
      else if (event.key === "r") actions.rename(row.s)
      else if (event.key === "x") actions.pick(row.s.id)
      else if (event.key === "e") actions.exportMd(row)
      else if (event.key === "Delete") actions.remove([item], i18n.t("shell.menu.quoted", { title: row.s.title }))
      else return
      event.preventDefault()
    }
    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  })
}
