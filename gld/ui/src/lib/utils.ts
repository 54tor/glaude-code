export { cn } from "cn"

// Écran tactile sans souris (iPhone) : pas de Maj+Entrée sur le clavier virtuel.
export const touchOnly = () => typeof matchMedia === "function" && matchMedia("(hover: none) and (pointer: coarse)").matches

// Entrée envoie (Maj+Entrée va à la ligne, Ctrl/Cmd+Entrée envoie aussi) ; pas pendant une composition (IME). Au clavier tactile,
// Entrée va à la ligne comme dans Messages : on envoie par le bouton.
export const isSend = (e: { key: string; shiftKey: boolean; ctrlKey?: boolean; metaKey?: boolean; nativeEvent: { isComposing?: boolean } }) =>
  e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing && (!touchOnly() || !!e.ctrlKey || !!e.metaKey)

// Retire l'aide clavier finale d'un texte indicatif (« … (Entrée envoie, …) ») là où elle est fausse : au clavier tactile.
export const keyHint = (s: string) => (touchOnly() ? s.replace(/\s*\([^()]*\)$/, "") : s)

// Copie dans le presse-papiers. L'API Clipboard n'existe qu'en contexte sécurisé (HTTPS ou localhost) : servi en HTTP sur l'IP
// du réseau local (iPhone), on repasse par une sélection et execCommand, encore pris en charge par WebKit pendant un geste.
export function copyText(text: string): Promise<void> {
  if (navigator.clipboard) return navigator.clipboard.writeText(text)
  const ta = document.createElement("textarea")
  ta.value = text
  ta.readOnly = true
  ta.style.cssText = "position:fixed;top:0;left:0;opacity:0;font-size:16px"
  document.body.append(ta)
  ta.select()
  ta.setSelectionRange(0, text.length)
  const ok = document.execCommand("copy")
  ta.remove()
  return ok ? Promise.resolve() : Promise.reject(new Error("copy"))
}
