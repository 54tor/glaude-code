// Focus clavier rendu à une ligne de session à la fermeture d'un dialogue. Faux si la ligne n'est pas (ou plus) affichée :
// le dialogue garde alors son comportement.
export function focusRow(id?: string): boolean {
  const el = id ? document.querySelector<HTMLElement>(`[data-row][data-id="${CSS.escape(id)}"]`) : null
  el?.focus()
  return !!el
}

// Ligne la plus proche qui restera quand ces sessions auront disparu : à calculer avant la suppression.
export function rowAfter(ids: string[]): string | undefined {
  const rows = [...document.querySelectorAll<HTMLElement>("[data-row][data-id]")].map((r) => r.dataset.id ?? "")
  const i = rows.findIndex((id) => ids.includes(id))
  if (i < 0) return undefined
  const stays = (id: string) => !ids.includes(id)
  return rows.slice(i).find(stays) ?? rows.slice(0, i).reverse().find(stays)
}
