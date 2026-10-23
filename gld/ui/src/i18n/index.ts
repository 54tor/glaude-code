import i18next from "i18next"
import { initReactI18next } from "react-i18next"
export type Lang = "fr" | "en"

// Un fichier par domaine (fr/<domaine>.json, en/<domaine>.json), dont la clé de premier niveau porte le nom du fichier :
// common, format, shell, conversation, memory, config, skills.
const merge = (mods: Record<string, { default: object }>) => Object.assign({}, ...Object.values(mods).map((m) => m.default))
export const catalogFiles = {
  fr: import.meta.glob<{ default: object }>("./fr/*.json", { eager: true }),
  en: import.meta.glob<{ default: object }>("./en/*.json", { eager: true }),
}
const fr = merge(catalogFiles.fr)
const en = merge(catalogFiles.en)

const pick = (s: string): Lang | "" => {
  const v = s.trim().toLowerCase()
  return v.startsWith("fr") ? "fr" : v.startsWith("en") ? "en" : ""
}

// server : le champ « lang » de /api (fr, en, other ou vide). nav : la langue du navigateur.
export function resolveFront(server: string | undefined, nav: string): Lang {
  if (server === "fr" || server === "en") return server
  if (server === "other") return "en"
  return pick(nav) || "fr"
}

void i18next.use(initReactI18next).init({
  resources: { fr: { translation: fr }, en: { translation: en } },
  lng: resolveFront(undefined, typeof navigator === "undefined" ? "" : navigator.language),
  fallbackLng: "fr",
  returnNull: false,
  interpolation: { escapeValue: false },
  initAsync: false,
})

export const i18n = i18next

export function setLang(l: Lang): void {
  void i18n.changeLanguage(l)
  if (typeof document !== "undefined") document.documentElement.lang = l
}

// Locale des formats de dates et de nombres.
export const loc = () => (i18n.language === "en" ? "en-GB" : "fr-FR")
