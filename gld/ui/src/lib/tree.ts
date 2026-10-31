import type { Group, Session } from "./api"
import { segs } from "./paths"

export type Node = { name: string; key: string; kids: Map<string, Node>; g: Group | null; ss: Session[] }
export type Entry = { g: Group; ss: Session[] }

const emptyNode = (name: string, key: string): Node => ({ name, key, kids: new Map(), g: null, ss: [] })

export const sorted = (node: Node): Node[] => [...node.kids.values()].sort((left, right) => left.name.localeCompare(right.name))

// Arbre des projets sous ~ (ou /, ou un lecteur). Une chaîne de dossiers sans projet et à enfant unique
// devient un seul niveau (a/b/c).
export function buildTree(entries: Entry[], home: string): Node[] {
  const root = emptyNode("", "")
  for (const { g: group, ss: sessions } of entries) {
    let currentNode = root
    const pathSegments = segs(group.project, home)
    pathSegments.forEach((segmentName, segmentIndex) => {
      if (!currentNode.kids.has(segmentName)) {
        const nodeKey = pathSegments.slice(0, segmentIndex + 1).join("/")
        currentNode.kids.set(segmentName, emptyNode(segmentName, nodeKey))
      }
      currentNode = currentNode.kids.get(segmentName)!
    })
    currentNode.g = group
    currentNode.ss = sessions
  }
  const mergeSingleChildChains = (node: Node) => {
    while (!node.g && node.kids.size === 1) {
      const onlyChild = [...node.kids.values()][0]
      const namePrefix = node.name === "/" ? "" : node.name + "/"
      node.name = namePrefix + onlyChild.name
      node.key = onlyChild.key
      node.g = onlyChild.g
      node.ss = onlyChild.ss
      node.kids = onlyChild.kids
    }
    node.kids.forEach(mergeSingleChildChains)
  }
  root.kids.forEach(mergeSingleChildChains)
  return sorted(root)
}

export function find(nodes: Node[], key: string): Node | null {
  for (const node of nodes) {
    if (node.key === key) return node
    const foundInChildren = find([...node.kids.values()], key)
    if (foundInChildren) return foundInChildren
  }
  return null
}

// Toutes les sessions d'un dossier et de ses sous-dossiers, avec leur projet.
export function under(node: Node): { s: Session; g: Group }[] {
  const group = node.g
  const ownSessions = group ? node.ss.map((session) => ({ s: session, g: group })) : []
  const descendantSessions = [...node.kids.values()].flatMap(under)
  return [...ownSessions, ...descendantSessions]
}
