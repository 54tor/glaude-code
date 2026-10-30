import i18next from "i18next"
import { initReactI18next } from "react-i18next"
export type Lang = "fr" | "en"

// Un fichier par domaine (fr/<domaine>.json, en/<domaine>.json), dont la clé de premier niveau porte le nom du fichier :
// common, format, shell, conversation, memory, config, skills, auth, ide.
const merge = (mods: Record<string, { default: object }>) => Object.assign({}, ...Object.values(mods).map((m) => m.default))
// Le français (langue de repli) est dans l'entrée ; l'anglais est un chunk chargé à la demande, pour ne pas alourdir le premier chargement.
export const catalogFiles = {
  fr: import.meta.glob<{ default: object }>("./fr/*.json", { eager: true }),
  en: import.meta.glob<{ default: object }>("./en/*.json"),
}
const fr = merge(catalogFiles.fr)

export async function loadLang(l: Lang): Promise<void> {
  if (l === "fr" || i18next.hasResourceBundle(l, "translation")) return
  const mods = await Promise.all(Object.values(catalogFiles[l]).map((load) => load()))
  i18next.addResourceBundle(l, "translation", merge(Object.fromEntries(mods.map((m, i) => [i, m]))))
}

const pick = (s: string): Lang | "" => {
  const v = s.trim().toLowerCase()
  return v.startsWith("fr") ? "fr" : v.startsWith("en") ? "en" : ""
}

// server : le champ « lang » de GET /v1/state (fr, en, other ou vide). nav : la langue du navigateur.
export function resolveFront(server: string | undefined, nav: string): Lang {
  if (server === "fr" || server === "en") return server
  if (server === "other") return "en"
  return pick(nav) || "fr"
}

void i18next.use(initReactI18next).init({
  resources: { fr: { translation: fr } },
  lng: resolveFront(undefined, typeof navigator === "undefined" ? "" : navigator.language),
  fallbackLng: "fr",
  returnNull: false,
  interpolation: { escapeValue: false },
  initAsync: false,
})

export const i18n = i18next

// Synchrone quand le catalogue est déjà chargé (français, ou anglais déjà vu) ; sinon bascule à l'arrivée du chunk.
export function setLang(l: Lang): void {
  if (typeof document !== "undefined") document.documentElement.lang = l
  if (i18next.hasResourceBundle(l, "translation")) void i18n.changeLanguage(l)
  else void loadLang(l).then(() => i18n.changeLanguage(l))
}

// Évite le flash de français au démarrage d'un navigateur anglophone.
const first = resolveFront(undefined, typeof navigator === "undefined" ? "" : navigator.language)
if (first !== "fr") {
  await loadLang(first)
  void i18n.changeLanguage(first)
}

// Locale des formats de dates et de nombres.
export const loc = () => (i18n.language === "en" ? "en-GB" : "fr-FR")
