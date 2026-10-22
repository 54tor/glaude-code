export { cn } from "cn"

// Entrée envoie (Maj+Entrée va à la ligne, Ctrl/Cmd+Entrée envoie aussi) ; pas pendant une composition (IME).
export const isSend = (e: { key: string; shiftKey: boolean; nativeEvent: { isComposing?: boolean } }) => e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing
