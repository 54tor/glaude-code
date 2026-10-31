const SESSION_ROW_SELECTOR = "[data-row][data-id]"

// Focus clavier rendu à une ligne de session à la fermeture d'un dialogue. Faux si la ligne n'est pas (ou plus) affichée :
// le dialogue garde alors son comportement.
export function focusRow(id?: string): boolean {
  if (!id) return false
  const rowElement = document.querySelector<HTMLElement>(`[data-row][data-id="${CSS.escape(id)}"]`)
  if (!rowElement) return false
  rowElement.focus()
  return true
}

// Ligne la plus proche qui restera quand ces sessions auront disparu : à calculer avant la suppression.
export function rowAfter(ids: string[]): string | undefined {
  const rowIds = [...document.querySelectorAll<HTMLElement>(SESSION_ROW_SELECTOR)].map((rowElement) => rowElement.dataset.id ?? "")
  const firstRemovedIndex = rowIds.findIndex((rowId) => ids.includes(rowId))
  if (firstRemovedIndex < 0) return undefined
  const staysAfterRemoval = (rowId: string) => !ids.includes(rowId)
  const nextRemaining = rowIds.slice(firstRemovedIndex).find(staysAfterRemoval)
  if (nextRemaining !== undefined) return nextRemaining
  return rowIds.slice(0, firstRemovedIndex).reverse().find(staysAfterRemoval)
}
