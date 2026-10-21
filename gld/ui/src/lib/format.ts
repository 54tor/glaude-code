// Mise en forme des dates, tailles et chemins pour l'affichage.
import { segs } from "./paths"

const rtf = new Intl.RelativeTimeFormat("fr", { numeric: "auto" })
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
  if (Math.abs(d) < 60) return "à l'instant"
  let u = UNITS[0]
  for (const x of UNITS) if (Math.abs(d) >= x[1]) u = x
  return rtf.format(Math.round(d / u[1]), u[0])
}

export const full = (x: string | number) =>
  new Date(typeof x === "number" ? x * 1000 : x).toLocaleString("fr-FR", {
    dateStyle: "long",
    timeStyle: "short",
  })

export const size = (kb: number) => (kb < 1024 ? `${kb} Ko` : `${(kb / 1024).toLocaleString("fr-FR", { maximumFractionDigits: 1 })} Mo`)

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
  if (sec >= t0) return "Aujourd'hui"
  if (sec >= t0 - 86400) return "Hier"
  if (sec >= t0 - 6 * 86400) return "Cette semaine"
  if (sec >= t0 - 29 * 86400) return "Ce mois-ci"
  return "Plus ancien"
}

// Tokens traités par la session : entrée neuve + sortie + cache écrit (le cache lu, relu à chaque tour, est compté à part).
export const tokTotal = (t: { in: number; out: number; cw: number }) => t.in + t.out + t.cw
export const compact = (n: number) =>
  n < 1000
    ? String(n)
    : n < 1e6
      ? `${(n / 1e3).toLocaleString("fr-FR", { maximumFractionDigits: 1 })} k`
      : `${(n / 1e6).toLocaleString("fr-FR", { maximumFractionDigits: 1 })} M`
