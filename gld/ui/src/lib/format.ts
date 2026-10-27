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

// « il y a 3 heures », « hier »… à partir d'un horodatage Unix en secondes.
export function ago(sec: number): string {
  const d = sec - Date.now() / 1000
  if (Math.abs(d) < 60) return i18n.t("format.now")
  // unité suivante dès que l'arrondi l'atteindrait : 59 min 50 s donne « il y a 1 heure », pas « il y a 60 minutes »
  let u = UNITS[0]
  for (const x of UNITS) if (Math.round(Math.abs(d) / u[1]) * u[1] >= x[1]) u = x
  return new Intl.RelativeTimeFormat(loc(), { numeric: "auto" }).format(Math.round(d / u[1]), u[0])
}

export const full = (x: string | number) =>
  new Date(typeof x === "number" ? x * 1000 : x).toLocaleString(loc(), {
    dateStyle: "long",
    timeStyle: "short",
  })

export const brief = (x: string | number) =>
  new Date(typeof x === "number" ? x * 1000 : x).toLocaleString(loc(), { dateStyle: "medium", timeStyle: "short" })

export const size = (kb: number) =>
  kb < 1024 ? `${kb} ${i18n.t("format.kb")}` : `${(kb / 1024).toLocaleString(loc(), { maximumFractionDigits: 1 })} ${i18n.t("format.mb")}`

// Chemin raccourci en ~/… sous le dossier personnel.
export function tilde(p: string, home: string): string {
  const s = segs(p, home)
  return s[0] === "~" ? s.join("/") : p
}

// Tranche de date pour regrouper une liste triée du plus récent au plus ancien.
export function bucket(sec: number): string {
  const day = new Date()
  day.setHours(0, 0, 0, 0)
  const t0 = day.getTime() / 1000
  if (sec >= t0) return i18n.t("shell.format.today")
  if (sec >= t0 - 86400) return i18n.t("shell.format.yesterday")
  if (sec >= t0 - 6 * 86400) return i18n.t("shell.format.thisWeek")
  if (sec >= t0 - 29 * 86400) return i18n.t("shell.format.thisMonth")
  return i18n.t("shell.format.older")
}

// Tokens traités par la session : entrée neuve + sortie + cache écrit (le cache lu, relu à chaque tour, est compté à part).
export const tokTotal = (t: { in: number; out: number; cw: number }) => t.in + t.out + t.cw
export const compact = (n: number) =>
  n < 1000
    ? String(n)
    : n < 999_950 // au-delà, l'arrondi au dixième afficherait « 1 000 k »
      ? `${(n / 1e3).toLocaleString(loc(), { maximumFractionDigits: 1 })} k`
      : `${(n / 1e6).toLocaleString(loc(), { maximumFractionDigits: 1 })} M`

// Un document écrit puis cité n'apparaît qu'une fois : à sa dernière mention.
export const dedupeDocs = <T extends { r: string; t: string }>(ms: T[]): T[] => {
  const last = new Map<string, number>()
  ms.forEach((m, i) => m.r === "doc" && last.set(m.t, i))
  return ms.filter((m, i) => m.r !== "doc" || last.get(m.t) === i)
}
