// Vue courante <-> chemin de l'URL (routage wouter). La session lue et la recherche sont dans ?s= et ?q=.

export type Route =
  | { kind: "recent" }
  | { kind: "all" }
  | { kind: "group"; name: string }
  | { kind: "dir"; key: string } // clé d'un nœud de l'arbre, ex. « ~/Projets/caddy »
  | { kind: "snippets"; path: string }
  | { kind: "tabs" }
  | { kind: "memory"; key: string } // clé de memory.go, ex. « m/-home-moi-Projets-x/MEMORY.md »

const dec = (p: string) => p.split("/").map(decodeURIComponent).join("/")
const enc = (p: string) => p.split("/").map(encodeURIComponent).join("/")

export function parse(loc: string): Route {
  try {
    if (loc === "/toutes") return { kind: "all" }
    if (loc.startsWith("/groupe/")) return { kind: "group", name: decodeURIComponent(loc.slice(8)) }
    if (loc.startsWith("/dossier/")) return { kind: "dir", key: dec(loc.slice(9)) }
    if (loc === "/onglets") return { kind: "tabs" }
    if (loc === "/memoire" || loc.startsWith("/memoire/")) return { kind: "memory", key: dec(loc.slice(9)) }
    if (loc === "/snippets" || loc.startsWith("/snippets/")) return { kind: "snippets", path: dec(loc.slice(10)) }
  } catch {
    /* encodage invalide : retour à l'accueil */
  }
  return { kind: "recent" }
}

export function href(r: Route): string {
  switch (r.kind) {
    case "all":
      return "/toutes"
    case "group":
      return "/groupe/" + encodeURIComponent(r.name)
    case "dir":
      return "/dossier/" + enc(r.key)
    case "tabs":
      return "/onglets"
    case "memory":
      return "/memoire" + (r.key ? "/" + enc(r.key) : "")
    case "snippets":
      return "/snippets" + (r.path ? "/" + enc(r.path) : "")
    default:
      return "/"
  }
}
