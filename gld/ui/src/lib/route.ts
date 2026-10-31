// Vue courante <-> chemin de l'URL (routage wouter). La session lue et la recherche sont dans ?s= et ?q=.

export type Route =
  | { kind: "recent" }
  | { kind: "home" } // accueil : sessions ouvertes, dernières fermées, MCP, mise à jour
  | { kind: "all" }
  | { kind: "active" } // sessions ouvertes ou au cache chaud
  | { kind: "auto" } // revues de sécurité lancées par des skills, masquées ailleurs
  | { kind: "group"; name: string; key: string } // groupe virtuel du dossier de clé key
  | { kind: "project"; name: string } // projet : ensemble de dossiers de travail
  | { kind: "dir"; key: string } // clé d'un nœud de l'arbre, ex. « ~/Projets/caddy »
  | { kind: "snippets"; path: string }
  | { kind: "tabs" }
  | { kind: "workspace"; dir: string } // espace de travail (IDE) d'un dossier absolu
  | { kind: "projectConfig"; dir: string } // permissions et réglages d'un projet (dossier de transcripts de ses sessions)
  | { kind: "skills"; key: string } // clé de skills.go, ex. « u/revue » ou « p/-home-moi-Projets-x/deploy »
  | { kind: "teams" } // équipes d'agents : composition et lancement
  | { kind: "team"; name: string } // équipe en cours, nom « session-xxxxxxxx »
  | { kind: "extensions" } // plugins, skills, hooks et MCP actifs par portée
  | { kind: "config"; tab: string } // onglet affiché ("" : Santé) ; configuration de Claude Code (réglages, hooks, permissions, MCP)
  | { kind: "memory"; key: string } // clé de memory.go, ex. « m/-home-moi-Projets-x/MEMORY.md »

export const TEAM_NAME = /^session-[0-9a-f]{8}$/

const decodePath = (path: string) => path.split("/").map(decodeURIComponent).join("/")
const encodePath = (path: string) => path.split("/").map(encodeURIComponent).join("/")

export function parse(pathname: string): Route {
  const isSectionOrBelow = (section: string) => pathname === section || pathname.startsWith(section + "/")
  // Suite du chemin après « section/ » ; "" pour la section seule.
  const afterSection = (section: string) => pathname.slice(section.length + 1)
  try {
    if (pathname === "/accueil") return { kind: "home" }
    if (pathname === "/toutes") return { kind: "all" }
    if (pathname === "/en-cours") return { kind: "active" }
    if (pathname === "/revues") return { kind: "auto" }
    if (pathname.startsWith("/groupe/")) {
      const groupPath = afterSection("/groupe")
      const nameEnd = groupPath.indexOf("/")
      if (nameEnd > 0) return { kind: "group", name: decodeURIComponent(groupPath.slice(0, nameEnd)), key: decodePath(groupPath.slice(nameEnd + 1)) }
    }
    if (pathname.startsWith("/projet/")) return { kind: "project", name: decodeURIComponent(afterSection("/projet")) }
    if (pathname.startsWith("/dossier/")) return { kind: "dir", key: decodePath(afterSection("/dossier")) }
    if (pathname.startsWith("/espace/")) return { kind: "workspace", dir: decodeURIComponent(afterSection("/espace")) }
    if (pathname.startsWith("/reglages/")) return { kind: "projectConfig", dir: decodeURIComponent(afterSection("/reglages")) }
    if (pathname === "/onglets") return { kind: "tabs" }
    if (isSectionOrBelow("/configuration")) return { kind: "config", tab: decodeURIComponent(afterSection("/configuration")) }
    if (pathname === "/equipes") return { kind: "teams" }
    if (pathname.startsWith("/equipes/")) {
      const teamName = afterSection("/equipes")
      if (TEAM_NAME.test(teamName)) return { kind: "team", name: teamName }
    }
    if (pathname === "/extensions") return { kind: "extensions" }
    if (pathname === "/plugins") return { kind: "extensions" } // ancienne page, fusionnée dans Extensions
    if (pathname === "/sauvegarde") return { kind: "config", tab: "export" }
    if (isSectionOrBelow("/skills")) return { kind: "skills", key: decodePath(afterSection("/skills")) }
    if (isSectionOrBelow("/memoire")) return { kind: "memory", key: decodePath(afterSection("/memoire")) }
    if (isSectionOrBelow("/snippets")) return { kind: "snippets", path: decodePath(afterSection("/snippets")) }
  } catch {
    /* encodage invalide : retour à l'accueil */
  }
  return { kind: "recent" }
}

export function href(route: Route): string {
  switch (route.kind) {
    case "all":
      return "/toutes"
    case "active":
      return "/en-cours"
    case "auto":
      return "/revues"
    case "group":
      return "/groupe/" + encodeURIComponent(route.name) + "/" + encodePath(route.key)
    case "project":
      return "/projet/" + encodeURIComponent(route.name)
    case "dir":
      return "/dossier/" + encodePath(route.key)
    case "tabs":
      return "/onglets"
    case "workspace":
      return "/espace/" + encodeURIComponent(route.dir)
    case "projectConfig":
      return "/reglages/" + encodeURIComponent(route.dir)
    case "home":
      return "/accueil"
    case "teams":
      return "/equipes"
    case "team":
      return "/equipes/" + route.name
    case "extensions":
      return "/extensions"
    case "config":
      return "/configuration" + (route.tab ? "/" + encodeURIComponent(route.tab) : "")
    case "skills":
      return "/skills" + (route.key ? "/" + encodePath(route.key) : "")
    case "memory":
      return "/memoire" + (route.key ? "/" + encodePath(route.key) : "")
    case "snippets":
      return "/snippets" + (route.path ? "/" + encodePath(route.path) : "")
    default:
      return "/"
  }
}
