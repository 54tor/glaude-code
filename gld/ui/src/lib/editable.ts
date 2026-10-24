// Cible où la frappe est du texte, que les raccourcis globaux doivent laisser passer.
// Monaco 0.57 saisit par EditContext : son élément focalisé n'est ni un champ ni contenteditable, d'où .monaco-editor.
export const isEditableTarget = (t: EventTarget | null) =>
  t instanceof HTMLElement && (t.isContentEditable || !!t.closest("input, textarea, select, [contenteditable='true'], .monaco-editor"))
