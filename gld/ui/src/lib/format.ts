// Mise en forme des dates, tailles et chemins pour l'affichage.
import { i18n, loc } from "@/i18n"
import { segs } from "./paths"

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["minute", 60],
  ["hour", 3600],
  ["day", 86400],
  ["week", 604800],
  ["month", 2629800],
  ["year", 31557600],
]

const SECONDS_PER_MINUTE = 60
const SECONDS_PER_DAY = 86400
const KB_PER_MB = 1024
const ONE_THOUSAND = 1e3
const ONE_MILLION = 1e6
// Au-delà, l'arrondi au dixième afficherait « 1 000 k ».
const COMPACT_THOUSANDS_LIMIT = 999_950

// « il y a 3 heures », « hier »… à partir d'un horodatage Unix en secondes.
export function ago(sec: number): string {
  const deltaSeconds = sec - Date.now() / 1000
  const absoluteDelta = Math.abs(deltaSeconds)
  if (absoluteDelta < SECONDS_PER_MINUTE) return i18n.t("format.now")
  // unité suivante dès que l'arrondi l'atteindrait : 59 min 50 s donne « il y a 1 heure », pas « il y a 60 minutes »
  let chosenUnit = UNITS[0]
  for (const candidateUnit of UNITS) {
    const [, chosenUnitSeconds] = chosenUnit
    const roundedSeconds = Math.round(absoluteDelta / chosenUnitSeconds) * chosenUnitSeconds
    if (roundedSeconds >= candidateUnit[1]) chosenUnit = candidateUnit
  }
  const [unitName, unitSeconds] = chosenUnit
  return new Intl.RelativeTimeFormat(loc(), { numeric: "auto" }).format(Math.round(deltaSeconds / unitSeconds), unitName)
}

const toDate = (timestamp: string | number) => {
  if (typeof timestamp === "number") return new Date(timestamp * 1000)
  return new Date(timestamp)
}

export const full = (timestamp: string | number) => toDate(timestamp).toLocaleString(loc(), { dateStyle: "long", timeStyle: "short" })

export const brief = (timestamp: string | number) => toDate(timestamp).toLocaleString(loc(), { dateStyle: "medium", timeStyle: "short" })

export const size = (kb: number) => {
  if (kb < KB_PER_MB) return `${kb} ${i18n.t("format.kb")}`
  const megabytes = (kb / KB_PER_MB).toLocaleString(loc(), { maximumFractionDigits: 1 })
  return `${megabytes} ${i18n.t("format.mb")}`
}

// Chemin raccourci en ~/… sous le dossier personnel.
export function tilde(path: string, home: string): string {
  const pathSegments = segs(path, home)
  if (pathSegments[0] === "~") return pathSegments.join("/")
  return path
}

// Tranche de date pour regrouper une liste triée du plus récent au plus ancien.
export function bucket(sec: number): string {
  const todayStart = new Date()
  todayStart.setHours(0, 0, 0, 0)
  const todayStartSeconds = todayStart.getTime() / 1000
  if (sec >= todayStartSeconds) return i18n.t("shell.format.today")
  if (sec >= todayStartSeconds - SECONDS_PER_DAY) return i18n.t("shell.format.yesterday")
  if (sec >= todayStartSeconds - 6 * SECONDS_PER_DAY) return i18n.t("shell.format.thisWeek")
  if (sec >= todayStartSeconds - 29 * SECONDS_PER_DAY) return i18n.t("shell.format.thisMonth")
  return i18n.t("shell.format.older")
}

// Tokens traités par la session : entrée neuve + sortie + cache écrit (le cache lu, relu à chaque tour, est compté à part).
export const tokTotal = (tokens: { in: number; out: number; cw: number }) => tokens.in + tokens.out + tokens.cw

export const compact = (value: number) => {
  if (value < ONE_THOUSAND) return String(value)
  const formatOneDecimal = (amount: number) => amount.toLocaleString(loc(), { maximumFractionDigits: 1 })
  if (value < COMPACT_THOUSANDS_LIMIT) return `${formatOneDecimal(value / ONE_THOUSAND)} k`
  return `${formatOneDecimal(value / ONE_MILLION)} M`
}

// Un document écrit puis cité n'apparaît qu'une fois : à sa dernière mention.
export const dedupeDocs = <T extends { r: string; t: string }>(messages: T[]): T[] => {
  const lastIndexByDocument = new Map<string, number>()
  messages.forEach((message, messageIndex) => {
    if (message.r === "doc") lastIndexByDocument.set(message.t, messageIndex)
  })
  return messages.filter((message, messageIndex) => message.r !== "doc" || lastIndexByDocument.get(message.t) === messageIndex)
}
