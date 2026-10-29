import { i18n } from "@/i18n"

// Paquet de mémoires échangé entre deux installations : JSON, une entrée par fichier memory/*.md.
// Le projet n'est que le dernier segment du chemin : le destinataire désigne lui-même le projet cible.
export type PackEntry = { project: string; name: string; text: string }

const FORMAT = "gld-memory"
const NAME = /^[^./\\][^/\\]*\.md$/ // même règle que mdName côté serveur
const MAX_TEXT = 1 << 20
export const MAX_PACK = 1000

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
  const a = document.createElement("a")
  a.href = URL.createObjectURL(new Blob([packOf(entries)], { type: "application/json" }))
  a.download = "gld-memoires.json"
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 1000)
}
