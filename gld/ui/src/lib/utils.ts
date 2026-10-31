export { cn } from "cn"

// Écran tactile sans souris (iPhone) : pas de Maj+Entrée sur le clavier virtuel.
export const touchOnly = () => typeof matchMedia === "function" && matchMedia("(hover: none) and (pointer: coarse)").matches

// Entrée envoie (Maj+Entrée va à la ligne, Ctrl/Cmd+Entrée envoie aussi) ; pas pendant une composition (IME). Au clavier tactile,
// Entrée va à la ligne comme dans Messages : on envoie par le bouton.
export const isSend = (event: { key: string; shiftKey: boolean; ctrlKey?: boolean; metaKey?: boolean; nativeEvent: { isComposing?: boolean } }) => {
  const isPlainEnter = event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing
  if (!isPlainEnter) return false
  const hasSendModifier = !!event.ctrlKey || !!event.metaKey
  return !touchOnly() || hasSendModifier
}

const TRAILING_PARENTHESIZED_HINT = /\s*\([^()]*\)$/

// Retire l'aide clavier finale d'un texte indicatif (« … (Entrée envoie, …) ») là où elle est fausse : au clavier tactile.
export const keyHint = (hintText: string) => {
  if (!touchOnly()) return hintText
  return hintText.replace(TRAILING_PARENTHESIZED_HINT, "")
}

// Copie dans le presse-papiers. L'API Clipboard n'existe qu'en contexte sécurisé (HTTPS ou localhost) : servi en HTTP sur l'IP
// du réseau local (iPhone), on repasse par une sélection et execCommand, encore pris en charge par WebKit pendant un geste.
export function copyText(text: string): Promise<void> {
  if (navigator.clipboard) return navigator.clipboard.writeText(text)
  const hiddenTextarea = document.createElement("textarea")
  hiddenTextarea.value = text
  hiddenTextarea.readOnly = true
  hiddenTextarea.style.cssText = "position:fixed;top:0;left:0;opacity:0;font-size:16px"
  document.body.append(hiddenTextarea)
  hiddenTextarea.select()
  hiddenTextarea.setSelectionRange(0, text.length)
  const copied = document.execCommand("copy")
  hiddenTextarea.remove()
  if (copied) return Promise.resolve()
  return Promise.reject(new Error("copy"))
}

// Insère insertedText à la place de la sélection d'un champ de texte (à la fin sans champ), dans la limite maxLength.
export function spliceAtCaret(textarea: HTMLTextAreaElement | null, currentText: string, insertedText: string, maxLength: number): string {
  const selectionStart = textarea?.selectionStart ?? currentText.length
  const selectionEnd = textarea?.selectionEnd ?? selectionStart
  const textWithInsertion = currentText.slice(0, selectionStart) + insertedText + currentText.slice(selectionEnd)
  return textWithInsertion.slice(0, maxLength)
}
