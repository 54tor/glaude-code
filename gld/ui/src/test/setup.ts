import { loadLang, setLang } from "@/i18n"

// Les tests tournent en français quelle que soit la langue du navigateur simulé.
await loadLang("en")
setLang("fr")
