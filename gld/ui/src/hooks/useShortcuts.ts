import { useEffect, type RefObject } from "react"
import { i18n } from "@/i18n"
import { isEditableTarget } from "@/lib/editable"
import type { Actions, Row } from "@/components/actions"

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
    const k = (e: KeyboardEvent) => {
      if (isEditableTarget(e.target) || e.ctrlKey || e.metaKey || e.altKey) return
      if (document.querySelector('[role="dialog"],[role="alertdialog"],[role="menu"]')) return
      if (e.key === "/") {
        e.preventDefault()
        search.current?.focus()
        return
      }
      if (e.key === "Escape" && picked.size) return clearPicked()
      const rowsEl = [...document.querySelectorAll<HTMLElement>("main [data-row]")]
      const i = rowsEl.indexOf(document.activeElement as HTMLElement)
      if (e.key === "j" || e.key === "k" || e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault()
        const step = e.key === "j" || e.key === "ArrowDown" ? 1 : -1
        const next = rowsEl[Math.max(0, Math.min(rowsEl.length - 1, i < 0 ? 0 : i + step))]
        next?.focus()
        next?.scrollIntoView({ block: "nearest" })
        return
      }
      const r = i >= 0 ? byId.get(rowsEl[i].dataset.id!) : undefined
      if (!r) return
      const one = { dir: r.s.dir, id: r.s.id }
      if (e.key === "t") actions.open(one)
      else if (e.key === "m") actions.resumeWith(r)
      else if (e.key === "r") actions.rename(r.s)
      else if (e.key === "x") actions.pick(r.s.id)
      else if (e.key === "e") actions.exportMd(r)
      else if (e.key === "Delete") actions.remove([one], i18n.t("shell.menu.quoted", { title: r.s.title }))
      else return
      e.preventDefault()
    }
    window.addEventListener("keydown", k)
    return () => window.removeEventListener("keydown", k)
  })
}
