import i18next from "i18next"
import { initReactI18next } from "react-i18next"
export type Lang = "fr" | "en"

type CatalogModule = { default: object }

// Un fichier par domaine (fr/<domaine>.json, en/<domaine>.json), dont la clé de premier niveau porte le nom du fichier :
// common, format, shell, conversation, memory, config, skills, auth, ide.
const mergeCatalogModules = (catalogModules: Record<string, CatalogModule>) => {
  const catalogContents = Object.values(catalogModules).map((catalogModule) => catalogModule.default)
  return Object.assign({}, ...catalogContents)
}
// Le français (langue de repli) est dans l'entrée ; l'anglais est un chunk chargé à la demande, pour ne pas alourdir le premier chargement.
export const catalogFiles = {
  fr: import.meta.glob<CatalogModule>("./fr/*.json", { eager: true }),
  en: import.meta.glob<CatalogModule>("./en/*.json"),
}
const frenchTranslations = mergeCatalogModules(catalogFiles.fr)

const TRANSLATION_NAMESPACE = "translation"
const FALLBACK_LANG: Lang = "fr"

export async function loadLang(lang: Lang): Promise<void> {
  const isAlreadyAvailable = lang === "fr" || i18next.hasResourceBundle(lang, TRANSLATION_NAMESPACE)
  if (isAlreadyAvailable) return
  const catalogLoaders = Object.values(catalogFiles[lang])
  const loadedModules = await Promise.all(catalogLoaders.map((loadCatalog) => loadCatalog()))
  const modulesByIndex = Object.fromEntries(loadedModules.map((loadedModule, moduleIndex) => [moduleIndex, loadedModule]))
  i18next.addResourceBundle(lang, TRANSLATION_NAMESPACE, mergeCatalogModules(modulesByIndex))
}

const langFromLocale = (locale: string): Lang | "" => {
  const normalizedLocale = locale.trim().toLowerCase()
  if (normalizedLocale.startsWith("fr")) return "fr"
  if (normalizedLocale.startsWith("en")) return "en"
  return ""
}

// server : le champ « lang » de GET /v1/state (fr, en, other ou vide). nav : la langue du navigateur.
export function resolveFront(server: string | undefined, nav: string): Lang {
  if (server === "fr" || server === "en") return server
  if (server === "other") return "en"
  return langFromLocale(nav) || FALLBACK_LANG
}

const browserLanguage = () => (typeof navigator === "undefined" ? "" : navigator.language)

void i18next.use(initReactI18next).init({
  resources: { fr: { translation: frenchTranslations } },
  lng: resolveFront(undefined, browserLanguage()),
  fallbackLng: FALLBACK_LANG,
  returnNull: false,
  interpolation: { escapeValue: false },
  initAsync: false,
})

export const i18n = i18next

// Synchrone quand le catalogue est déjà chargé (français, ou anglais déjà vu) ; sinon bascule à l'arrivée du chunk.
export function setLang(lang: Lang): void {
  if (typeof document !== "undefined") document.documentElement.lang = lang
  if (i18next.hasResourceBundle(lang, TRANSLATION_NAMESPACE)) {
    void i18n.changeLanguage(lang)
    return
  }
  void loadLang(lang).then(() => i18n.changeLanguage(lang))
}

// Évite le flash de français au démarrage d'un navigateur anglophone.
const initialLang = resolveFront(undefined, browserLanguage())
if (initialLang !== FALLBACK_LANG) {
  await loadLang(initialLang)
  void i18n.changeLanguage(initialLang)
}

// Locale des formats de dates et de nombres.
export const loc = () => (i18n.language === "en" ? "en-GB" : "fr-FR")
