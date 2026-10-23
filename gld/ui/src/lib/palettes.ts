// Palettes classiques des éditeurs. Chacune donne, en clair et en sombre, 7 couleurs dont on tire toutes les variables du thème.
// "defaut" ne pose rien : ce sont les couleurs de index.css.
export type Tone = { bg: string; fg: string; surface: string; muted: string; border: string; primary: string; side: string }
export type Palette = { name: string; light?: Tone; dark?: Tone }

export const PALETTES: Record<string, Palette> = {
  defaut: { name: "Pupitre (défaut)" },
  dracula: {
    name: "Dracula",
    dark: { bg: "#282a36", fg: "#f8f8f2", surface: "#343746", muted: "#44475a", border: "#44475a", primary: "#bd93f9", side: "#21222c" },
    light: { bg: "#f8f8f2", fg: "#282a36", surface: "#ffffff", muted: "#e9e9e1", border: "#d6d6cc", primary: "#7c4dcc", side: "#efefe8" },
  },
  nord: {
    name: "Nord",
    dark: { bg: "#2e3440", fg: "#eceff4", surface: "#3b4252", muted: "#434c5e", border: "#4c566a", primary: "#88c0d0", side: "#272c36" },
    light: { bg: "#eceff4", fg: "#2e3440", surface: "#ffffff", muted: "#e5e9f0", border: "#d8dee9", primary: "#5e81ac", side: "#e5e9f0" },
  },
  solarized: {
    name: "Solarized",
    dark: { bg: "#002b36", fg: "#93a1a1", surface: "#073642", muted: "#0d3b47", border: "#164b57", primary: "#268bd2", side: "#00212b" },
    light: { bg: "#fdf6e3", fg: "#586e75", surface: "#fffdf5", muted: "#eee8d5", border: "#ddd6c1", primary: "#268bd2", side: "#f5eed9" },
  },
  gruvbox: {
    name: "Gruvbox",
    dark: { bg: "#282828", fg: "#ebdbb2", surface: "#3c3836", muted: "#504945", border: "#504945", primary: "#fabd2f", side: "#1d2021" },
    light: { bg: "#fbf1c7", fg: "#3c3836", surface: "#fdf7dc", muted: "#ebdbb2", border: "#d5c4a1", primary: "#b57614", side: "#f2e5bc" },
  },
  monokai: {
    name: "Monokai",
    dark: { bg: "#272822", fg: "#f8f8f2", surface: "#3e3d32", muted: "#49483e", border: "#49483e", primary: "#a6e22e", side: "#1e1f1c" },
    light: { bg: "#f9f8f5", fg: "#272822", surface: "#ffffff", muted: "#ebeae3", border: "#d6d5cc", primary: "#5a8a00", side: "#f0efe9" },
  },
  tokyo: {
    name: "Tokyo Night",
    dark: { bg: "#1a1b26", fg: "#c0caf5", surface: "#24283b", muted: "#292e42", border: "#3b4261", primary: "#7aa2f7", side: "#16161e" },
    light: { bg: "#e1e2e7", fg: "#3760bf", surface: "#ebecf0", muted: "#d5d6db", border: "#b4b5b9", primary: "#2e7de9", side: "#d5d6db" },
  },
  onedark: {
    name: "One Dark / One Light",
    dark: { bg: "#282c34", fg: "#abb2bf", surface: "#2c313a", muted: "#3e4451", border: "#3e4451", primary: "#61afef", side: "#21252b" },
    light: { bg: "#fafafa", fg: "#383a42", surface: "#ffffff", muted: "#eaeaeb", border: "#dbdbdc", primary: "#4078f2", side: "#f0f0f1" },
  },
  ayu: {
    name: "Ayu",
    dark: { bg: "#0b0e14", fg: "#bfbdb6", surface: "#131721", muted: "#1c2331", border: "#272d38", primary: "#e6b450", side: "#07090d" },
    light: { bg: "#fcfcfc", fg: "#5c6166", surface: "#ffffff", muted: "#eff0f1", border: "#dcdee0", primary: "#e6a23c", side: "#f3f4f5" },
  },
  material: {
    name: "Material",
    dark: { bg: "#263238", fg: "#eeffff", surface: "#2c3b41", muted: "#32424a", border: "#37474f", primary: "#82aaff", side: "#1f292e" },
    light: { bg: "#fafafa", fg: "#546e7a", surface: "#ffffff", muted: "#eceff1", border: "#cfd8dc", primary: "#6182b8", side: "#eef1f3" },
  },
  github: {
    name: "GitHub",
    dark: { bg: "#0d1117", fg: "#e6edf3", surface: "#161b22", muted: "#21262d", border: "#30363d", primary: "#58a6ff", side: "#010409" },
    light: { bg: "#ffffff", fg: "#1f2328", surface: "#ffffff", muted: "#f6f8fa", border: "#d0d7de", primary: "#0969da", side: "#f6f8fa" },
  },
  rosepine: {
    name: "Rosé Pine",
    dark: { bg: "#191724", fg: "#e0def4", surface: "#1f1d2e", muted: "#26233a", border: "#403d52", primary: "#c4a7e7", side: "#14121d" },
    light: { bg: "#faf4ed", fg: "#575279", surface: "#fffaf3", muted: "#f2e9e1", border: "#dfdad9", primary: "#907aa9", side: "#f4ede8" },
  },
  everforest: {
    name: "Everforest",
    dark: { bg: "#2d353b", fg: "#d3c6aa", surface: "#343f44", muted: "#3d484d", border: "#475258", primary: "#a7c080", side: "#232a2e" },
    light: { bg: "#fdf6e3", fg: "#5c6a72", surface: "#fffbef", muted: "#f4f0d9", border: "#e6e2cc", primary: "#8da101", side: "#f4f0d9" },
  },
  kanagawa: {
    name: "Kanagawa",
    dark: { bg: "#1f1f28", fg: "#dcd7ba", surface: "#2a2a37", muted: "#363646", border: "#54546d", primary: "#7e9cd8", side: "#16161d" },
    light: { bg: "#f2ecbc", fg: "#545464", surface: "#f9f5d7", muted: "#e7dba0", border: "#cfc49c", primary: "#4d699b", side: "#e7dba0" },
  },
  night: {
    name: "Night Owl",
    dark: { bg: "#011627", fg: "#d6deeb", surface: "#0b2942", muted: "#0e293f", border: "#122d42", primary: "#82aaff", side: "#010e1a" },
    light: { bg: "#fbfbfb", fg: "#403f53", surface: "#ffffff", muted: "#f0f0f0", border: "#d9d9d9", primary: "#4876d6", side: "#f0f0f0" },
  },
  catppuccin: {
    name: "Catppuccin",
    dark: { bg: "#1e1e2e", fg: "#cdd6f4", surface: "#313244", muted: "#313244", border: "#45475a", primary: "#cba6f7", side: "#181825" },
    light: { bg: "#eff1f5", fg: "#4c4f69", surface: "#ffffff", muted: "#ccd0da", border: "#bcc0cc", primary: "#8839ef", side: "#e6e9ef" },
  },
}

export function paletteVars(t: Tone): Record<string, string> {
  return {
    background: t.bg, foreground: t.fg, card: t.surface, "card-foreground": t.fg, popover: t.surface, "popover-foreground": t.fg,
    primary: t.primary, "primary-foreground": t.bg, secondary: t.muted, "secondary-foreground": t.fg,
    muted: t.muted, "muted-foreground": `color-mix(in oklab, ${t.fg} 62%, ${t.bg})`, accent: t.muted, "accent-foreground": t.fg,
    border: t.border, input: t.border, ring: t.primary,
    sidebar: t.side, "sidebar-foreground": t.fg, "sidebar-primary": t.primary, "sidebar-primary-foreground": t.bg,
    "sidebar-accent": t.muted, "sidebar-accent-foreground": t.fg, "sidebar-border": t.border, "sidebar-ring": t.primary,
  }
}

const NAMES = Object.keys(paletteVars(PALETTES.nord.dark!))

export function applyPalette(id: string, mode: "light" | "dark") {
  const style = document.documentElement.style
  NAMES.forEach((v) => style.removeProperty("--" + v))
  const tone = PALETTES[id]?.[mode]
  if (!tone) return
  for (const [k, v] of Object.entries(paletteVars(tone))) style.setProperty("--" + k, v)
}
