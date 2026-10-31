// Cible où la frappe est du texte, que les raccourcis globaux doivent laisser passer.
// Monaco 0.57 saisit par EditContext : son élément focalisé n'est ni un champ ni contenteditable, d'où .monaco-editor.
const TEXT_INPUT_SELECTOR = "input, textarea, select, [contenteditable='true'], .monaco-editor"

export const isEditableTarget = (target: EventTarget | null) => {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable) return true
  return target.closest(TEXT_INPUT_SELECTOR) !== null
}
