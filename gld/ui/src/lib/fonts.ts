import { i18n } from "@/i18n"

// Jeux de polices des conversations (texte, code), tous embarqués et couvrant le latin et le cyrillique.
export const FONTS = {
  geist: { label: "Geist", text: '"Geist Variable", sans-serif', mono: '"Geist Mono Variable", monospace' },
  inter: { label: "Inter · JetBrains Mono", text: '"Inter Variable", sans-serif', mono: '"JetBrains Mono Variable", monospace' },
  plex: { label: "IBM Plex Sans · Fira Code", text: '"IBM Plex Sans Variable", sans-serif', mono: '"Fira Code Variable", monospace' },
  jetbrains: { label: "JetBrains Mono", text: '"JetBrains Mono Variable", monospace', mono: '"JetBrains Mono Variable", monospace' },
  comic: { label: "Comic Sans (Comic Relief)", text: '"Comic Relief", "Comic Sans MS", cursive, sans-serif', mono: '"Geist Mono Variable", monospace' },
  adwaita: { get label() { return i18n.t("shell.fonts.adwaita") }, text: '"Adwaita Mono", ui-monospace, monospace', mono: '"Adwaita Mono", ui-monospace, monospace' },
} as const

export type FontId = keyof typeof FONTS
export const SIZES = ["13", "14", "15", "16", "17"]

const DEFAULT_SIZE = "15"
const BROWSER_BASE_FONT_PX = 16

export function applyFont(id: string, size: string) {
  const fontSet = FONTS[id as FontId] ?? FONTS.geist
  const rootStyle = document.documentElement.style
  rootStyle.setProperty("--reader-font", fontSet.text)
  rootStyle.setProperty("--reader-mono", fontSet.mono)
  const sizePx = SIZES.includes(size) ? size : DEFAULT_SIZE
  rootStyle.setProperty("--reader-size", `${sizePx}px`)
  // Tailwind compte en rem : changer la base agrandit toute l'app, pas seulement les conversations.
  rootStyle.fontSize = `${(Number(sizePx) / Number(DEFAULT_SIZE)) * BROWSER_BASE_FONT_PX}px`
}
