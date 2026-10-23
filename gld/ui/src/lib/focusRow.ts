// Focus clavier rendu à une ligne de session à la fermeture d'un dialogue. gone : ces lignes vont disparaître (suppression),
// le focus va à la plus proche qui reste. Faux si aucune ligne ne convient : le dialogue garde alors son comportement.
export function focusRow(ids: string[], gone = false): boolean {
  const rows = [...document.querySelectorAll<HTMLElement>("[data-row][data-id]")]
  const i = rows.findIndex((r) => ids.includes(r.dataset.id ?? ""))
  if (i < 0) return false
  const stays = (r: HTMLElement) => !ids.includes(r.dataset.id ?? "")
  const target = gone ? (rows.slice(i).find(stays) ?? rows.slice(0, i).reverse().find(stays)) : rows[i]
  target?.focus()
  return !!target
}
