import { i18n } from "@/i18n"

// Jeux de polices des conversations (texte, code), tous embarqués et couvrant le latin et le cyrillique.
export const FONTS = {
  geist: { label: "Geist", text: '"Geist Variable", sans-serif', mono: '"Geist Mono Variable", monospace' },
  inter: { label: "Inter · JetBrains Mono", text: '"Inter Variable", sans-serif', mono: '"JetBrains Mono Variable", monospace' },
  plex: { label: "IBM Plex Sans · Fira Code", text: '"IBM Plex Sans Variable", sans-serif', mono: '"Fira Code Variable", monospace' },
  jetbrains: { label: "JetBrains Mono", text: '"JetBrains Mono Variable", monospace', mono: '"JetBrains Mono Variable", monospace' },
  adwaita: { get label() { return i18n.t("shell.fonts.adwaita") }, text: '"Adwaita Mono", ui-monospace, monospace', mono: '"Adwaita Mono", ui-monospace, monospace' },
} as const

export type FontId = keyof typeof FONTS
export const SIZES = ["13", "14", "15", "16", "17"]

export function applyFont(id: string, size: string) {
  const f = FONTS[id as FontId] ?? FONTS.geist
  const st = document.documentElement.style
  st.setProperty("--reader-font", f.text)
  st.setProperty("--reader-mono", f.mono)
  st.setProperty("--reader-size", `${SIZES.includes(size) ? size : "15"}px`)
}
