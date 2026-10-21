// Chemins de projet (Unix ou Windows) : découpage relatif au dossier personnel, et jonction.

const norm = (p: string) => p.replace(/\\/g, "/")
const isDrive = (p: string) => /^[A-Za-z]:\//.test(p)

// ["~", ...] sous le home ; ["/", ...] ailleurs sous Unix ; ["C:", ...] sous Windows ; ["?", nom] si le dossier est inconnu.
export function segs(project: string, home: string): string[] {
  const P = norm(project)
  const H = norm(home)
  const win = isDrive(P)
  if (!P.startsWith("/") && !win) return ["?", project]
  const same = (a: string, b: string) => (win ? a.toLowerCase() === b.toLowerCase() : a === b)
  if (same(P, H)) return ["~"]
  if (same(P.slice(0, H.length), H) && P[H.length] === "/") return ["~", ...P.slice(H.length + 1).split("/").filter(Boolean)]
  const parts = P.split("/").filter(Boolean)
  return win ? parts : ["/", ...parts]
}

// Jonction d'un dossier et d'un nom avec le séparateur du système ; sous Windows la liste des lecteurs donne des chemins complets.
export function join(dir: string, name: string, sep: string): string {
  if (dir === "") return name
  return dir.endsWith(sep) ? dir + name : dir + sep + name
}
