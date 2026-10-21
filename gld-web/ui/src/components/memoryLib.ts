import { ALargeSmall, Clock, HardDrive } from "lucide-react"
import { toast } from "sonner"
import type { MemFile, MemProject, MemTarget } from "@/lib/api"

export type List = { global: MemFile[]; projects: MemProject[]; targets: MemTarget[] }

// Tri des fichiers de mémoire : critère et sens propres à chaque dossier (section), retenus dans le navigateur.
export const SORTS = [
  ["date", "date de modification", Clock],
  ["name", "nom", ALargeSmall],
  ["size", "taille", HardDrive],
] as const
export type Sort = { by: string; dir: string }
export const DEFAULT_SORT: Sort = { by: "date", dir: "desc" }
export const byName = (a: string, b: string) => a.localeCompare(b, "fr", { numeric: true })
// Comparateur de fichiers ; à égalité (la taille est en Ko arrondis), le nom départage.
export const cmpFiles = (by: string, dir: string) => (a: MemFile, b: MemFile) => {
  const d = (by === "name" ? byName(a.name, b.name) : by === "size" ? a.kb - b.kb : a.mtime - b.mtime) || byName(a.name, b.name)
  return dir === "asc" ? d : -d
}

// En-tête YAML (--- … ---) des entrées de mémoire : affiché à part, en petit.
export function split(t: string): [string, string] {
  const m = /^---\n([\s\S]*?)\n---\n?/.exec(t)
  return m ? [m[1], t.slice(m[0].length)] : ["", t]
}

// Seules les entrées de mémoire de projet et les règles globales changent de place (pas les CLAUDE.md).
export const movable = (key: string) => key.startsWith("m/") || key.startsWith("g/rules/")
// Dossier d'une clé : celui du projet (« m/<dossier>/… »), sinon « global ».
export const home = (key: string) => (key.startsWith("m/") ? key.split("/")[1] : "global")

export const copy = (text: string, what: string) =>
  navigator.clipboard.writeText(text).then(
    () => toast.success(`${what} copié`),
    () => toast.error("Copie impossible")
  )

// Clé d'une nouvelle entrée : le nom du front matter (ou le premier titre), simplifié en nom de fichier, sans écraser un fichier existant.
export function newKey(all: MemFile[], scope: string, text: string): string {
  const name = /^name:\s*["']?([^"'\n]+)/m.exec(text)?.[1] ?? /^#\s+(.+)$/m.exec(text)?.[1] ?? "entree"
  const slug =
    name
      .normalize("NFD")
      .replace(/[̀-ͯ]/g, "")
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "") || "entree"
  const base = scope === "global" ? "g/rules/" : `m/${scope}/`
  let key = base + slug + ".md"
  for (let i = 2; all.some((x) => x.key === key); i++) key = `${base}${slug}-${i}.md`
  return key
}

// Modèles d'une entrée écrite à la main (règle globale : un titre ; mémoire de projet : un front matter).
export const BLANK_RULE = "# Nouvelle règle\n\n"
export const BLANK_ENTRY = "---\nname: nouvelle-entree\ndescription: \nmetadata:\n  type: project\n---\n\n"
