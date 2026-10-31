import { strToU8, zipSync } from "fflate"
import { i18n } from "@/i18n"

// Paquet de mémoires échangé entre deux installations : JSON, une entrée par fichier memory/*.md.
// Le projet n'est que le dernier segment du chemin : le destinataire désigne lui-même le projet cible.
export type PackEntry = { project: string; name: string; text: string }

const FORMAT = "gld-memory"
const FORMAT_VERSION = 1
const NAME = /^[^./\\][^/\\]*\.md$/ // même règle que mdName côté serveur
const MAX_TEXT = 1 << 20
const MAX_PROJECT_LENGTH = 200
const MARKDOWN_EXTENSION_LENGTH = ".md".length
const OBJECT_URL_LIFETIME_MS = 1000
export const MAX_PACK = 1000

// Dossier du zip : « global » ou le projet ; les règles dans un sous-dossier « rules ».
export const packDir = (key: string, label: string) => {
  const base = key.startsWith("g/") ? "global" : label
  const isRule = key.startsWith("g/rules/") || key.startsWith("r/")
  if (isRule) return `${base}/rules`
  return base
}

export const projectLabel = (path: string) => path.split(/[\\/]/).filter(Boolean).pop() ?? ""

export const packOf = (entries: PackEntry[]) => JSON.stringify({ format: FORMAT, version: FORMAT_VERSION, entries }, null, 2) + "\n"

export function parsePack(raw: string): PackEntry[] {
  const invalidPackError = () => new Error(i18n.t("memory.pack.invalid"))
  let parsedPack: { format?: unknown; version?: unknown; entries?: unknown }
  try {
    parsedPack = JSON.parse(raw)
  } catch {
    throw invalidPackError()
  }
  const hasValidHeader = parsedPack?.format === FORMAT && parsedPack.version === FORMAT_VERSION
  if (!hasValidHeader || !Array.isArray(parsedPack.entries) || parsedPack.entries.length > MAX_PACK) throw invalidPackError()
  return parsedPack.entries.map((entry: Partial<PackEntry>) => {
    const hasValidName = typeof entry?.name === "string" && NAME.test(entry.name) && entry.name !== "MEMORY.md"
    const hasValidText = typeof entry?.text === "string" && entry.text.length <= MAX_TEXT
    if (!hasValidName || !hasValidText) throw invalidPackError()
    const project = typeof entry.project === "string" ? entry.project.slice(0, MAX_PROJECT_LENGTH) : ""
    return { project, name: entry.name as string, text: entry.text as string }
  })
}

// Premier nom libre « base-2.md », « base-3.md »… parmi les noms déjà pris.
export function freeName(name: string, taken: Set<string>): string {
  const baseName = name.slice(0, -MARKDOWN_EXTENSION_LENGTH)
  for (let suffixNumber = 2; ; suffixNumber++) {
    const candidate = `${baseName}-${suffixNumber}.md`
    if (!taken.has(candidate)) return candidate
  }
}

export function downloadPack(entries: PackEntry[]) {
  const zipFiles: Record<string, Uint8Array> = {}
  for (const entry of entries) {
    const projectDir = (entry.project.replace(/[\\:]/g, "_") || "projet") + "/"
    const takenNames = new Set(
      Object.keys(zipFiles)
        .filter((zipPath) => zipPath.startsWith(projectDir))
        .map((zipPath) => zipPath.slice(projectDir.length))
    )
    const fileName = takenNames.has(entry.name) ? freeName(entry.name, takenNames) : entry.name
    zipFiles[projectDir + fileName] = strToU8(entry.text)
  }
  const downloadLink = document.createElement("a")
  downloadLink.href = URL.createObjectURL(new Blob([zipSync(zipFiles) as BlobPart], { type: "application/zip" }))
  downloadLink.download = "gld-memoires.zip"
  downloadLink.click()
  setTimeout(() => URL.revokeObjectURL(downloadLink.href), OBJECT_URL_LIFETIME_MS)
}
