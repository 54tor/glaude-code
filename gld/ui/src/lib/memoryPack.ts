import { strToU8, zipSync } from "fflate"
import { i18n } from "@/i18n"

// Paquet de mémoires échangé entre deux installations : JSON, une entrée par fichier memory/*.md.
// Le projet n'est que le dernier segment du chemin : le destinataire désigne lui-même le projet cible.
export type PackEntry = { project: string; name: string; text: string }

const FORMAT = "gld-memory"
const NAME = /^[^./\\][^/\\]*\.md$/ // même règle que mdName côté serveur
const MAX_TEXT = 1 << 20
export const MAX_PACK = 1000

// Dossier du zip : « global » ou le projet ; les règles dans un sous-dossier « rules ».
export const packDir = (key: string, label: string) => {
  const base = key.startsWith("g/") ? "global" : label
  return key.startsWith("g/rules/") || key.startsWith("r/") ? `${base}/rules` : base
}

export const projectLabel = (path: string) => path.split(/[\\/]/).filter(Boolean).pop() ?? ""

export const packOf = (entries: PackEntry[]) => JSON.stringify({ format: FORMAT, version: 1, entries }, null, 2) + "\n"

export function parsePack(raw: string): PackEntry[] {
  const bad = () => new Error(i18n.t("memory.pack.invalid"))
  let d: { format?: unknown; version?: unknown; entries?: unknown }
  try {
    d = JSON.parse(raw)
  } catch {
    throw bad()
  }
  if (d?.format !== FORMAT || d.version !== 1 || !Array.isArray(d.entries) || d.entries.length > MAX_PACK) throw bad()
  return d.entries.map((e: Partial<PackEntry>) => {
    if (typeof e?.name !== "string" || !NAME.test(e.name) || e.name === "MEMORY.md" || typeof e.text !== "string" || e.text.length > MAX_TEXT) throw bad()
    return { project: typeof e.project === "string" ? e.project.slice(0, 200) : "", name: e.name, text: e.text }
  })
}

// Premier nom libre « base-2.md », « base-3.md »… parmi les noms déjà pris.
export function freeName(name: string, taken: Set<string>): string {
  const base = name.slice(0, -3)
  for (let i = 2; ; i++) if (!taken.has(`${base}-${i}.md`)) return `${base}-${i}.md`
}

export function downloadPack(entries: PackEntry[]) {
  const files: Record<string, Uint8Array> = {}
  for (const e of entries) {
    const dir = (e.project.replace(/[\\:]/g, "_") || "projet") + "/"
    const taken = new Set(Object.keys(files).filter((k) => k.startsWith(dir)).map((k) => k.slice(dir.length)))
    const name = taken.has(e.name) ? freeName(e.name, taken) : e.name
    files[dir + name] = strToU8(e.text)
  }
  const a = document.createElement("a")
  a.href = URL.createObjectURL(new Blob([zipSync(files) as BlobPart], { type: "application/zip" }))
  a.download = "gld-memoires.zip"
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 1000)
}
