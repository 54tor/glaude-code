// Vue courante <-> chemin de l'URL (routage wouter). La session lue et la recherche sont dans ?s= et ?q=.

export type Route =
  | { kind: "recent" }
  | { kind: "all" }
  | { kind: "active" } // sessions ouvertes ou au cache chaud
  | { kind: "group"; name: string; key: string } // groupe virtuel du dossier de clé key
  | { kind: "project"; name: string } // projet : ensemble de dossiers de travail
  | { kind: "dir"; key: string } // clé d'un nœud de l'arbre, ex. « ~/Projets/caddy »
  | { kind: "snippets"; path: string }
  | { kind: "tabs" }
  | { kind: "workspace"; dir: string } // espace de travail (IDE) d'un dossier absolu
  | { kind: "projectConfig"; dir: string } // permissions et réglages d'un projet (dossier de transcripts de ses sessions)
  | { kind: "skills"; key: string } // clé de skills.go, ex. « u/revue » ou « p/-home-moi-Projets-x/deploy »
  | { kind: "config"; tab: string } // onglet affiché ("" : Santé) ; configuration de Claude Code (réglages, hooks, permissions, MCP)
  | { kind: "memory"; key: string } // clé de memory.go, ex. « m/-home-moi-Projets-x/MEMORY.md »

const dec = (p: string) => p.split("/").map(decodeURIComponent).join("/")
const enc = (p: string) => p.split("/").map(encodeURIComponent).join("/")

export function parse(loc: string): Route {
  try {
    if (loc === "/toutes") return { kind: "all" }
    if (loc === "/en-cours") return { kind: "active" }
    if (loc.startsWith("/groupe/")) {
      const rest = loc.slice(8)
      const i = rest.indexOf("/")
      if (i > 0) return { kind: "group", name: decodeURIComponent(rest.slice(0, i)), key: dec(rest.slice(i + 1)) }
    }
    if (loc.startsWith("/projet/")) return { kind: "project", name: decodeURIComponent(loc.slice(8)) }
    if (loc.startsWith("/dossier/")) return { kind: "dir", key: dec(loc.slice(9)) }
    if (loc.startsWith("/espace/")) return { kind: "workspace", dir: decodeURIComponent(loc.slice(8)) }
    if (loc.startsWith("/reglages/")) return { kind: "projectConfig", dir: decodeURIComponent(loc.slice(10)) }
    if (loc === "/onglets") return { kind: "tabs" }
    if (loc === "/configuration" || loc.startsWith("/configuration/")) return { kind: "config", tab: decodeURIComponent(loc.slice(15)) }
    if (loc === "/skills" || loc.startsWith("/skills/")) return { kind: "skills", key: dec(loc.slice(8)) }
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
    case "active":
      return "/en-cours"
    case "group":
      return "/groupe/" + encodeURIComponent(r.name) + "/" + enc(r.key)
    case "project":
      return "/projet/" + encodeURIComponent(r.name)
    case "dir":
      return "/dossier/" + enc(r.key)
    case "tabs":
      return "/onglets"
    case "workspace":
      return "/espace/" + encodeURIComponent(r.dir)
    case "projectConfig":
      return "/reglages/" + encodeURIComponent(r.dir)
    case "config":
      return "/configuration" + (r.tab ? "/" + encodeURIComponent(r.tab) : "")
    case "skills":
      return "/skills" + (r.key ? "/" + enc(r.key) : "")
    case "memory":
      return "/memoire" + (r.key ? "/" + enc(r.key) : "")
    case "snippets":
      return "/snippets" + (r.path ? "/" + enc(r.path) : "")
    default:
      return "/"
  }
}
