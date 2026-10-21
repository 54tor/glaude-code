import type { Group, Session } from "./api"
import { segs } from "./paths"

export type Node = { name: string; key: string; kids: Map<string, Node>; g: Group | null; ss: Session[] }
export type Entry = { g: Group; ss: Session[] }

const blank = (name: string, key: string): Node => ({ name, key, kids: new Map(), g: null, ss: [] })

export const sorted = (n: Node): Node[] => [...n.kids.values()].sort((a, b) => a.name.localeCompare(b.name))

// Arbre des projets sous ~ (ou /, ou un lecteur). Une chaîne de dossiers sans projet et à enfant unique
// devient un seul niveau (a/b/c).
export function buildTree(entries: Entry[], home: string): Node[] {
  const root = blank("", "")
  for (const { g, ss } of entries) {
    let n = root
    const sg = segs(g.project, home)
    sg.forEach((name, i) => {
      if (!n.kids.has(name)) n.kids.set(name, blank(name, sg.slice(0, i + 1).join("/")))
      n = n.kids.get(name)!
    })
    n.g = g
    n.ss = ss
  }
  const squash = (n: Node) => {
    while (!n.g && n.kids.size === 1) {
      const c = [...n.kids.values()][0]
      n.name = (n.name === "/" ? "" : n.name + "/") + c.name
      n.key = c.key
      n.g = c.g
      n.ss = c.ss
      n.kids = c.kids
    }
    n.kids.forEach(squash)
  }
  root.kids.forEach(squash)
  return sorted(root)
}

export function find(nodes: Node[], key: string): Node | null {
  for (const n of nodes) {
    if (n.key === key) return n
    const k = find([...n.kids.values()], key)
    if (k) return k
  }
  return null
}

// Toutes les sessions d'un dossier et de ses sous-dossiers, avec leur projet.
export function under(n: Node): { s: Session; g: Group }[] {
  const own = n.g ? n.ss.map((s) => ({ s, g: n.g! })) : []
  return [...own, ...[...n.kids.values()].flatMap(under)]
}
