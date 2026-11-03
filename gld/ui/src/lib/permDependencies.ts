// Dépendances entre règles de permission de plusieurs couches : une règle neutralisée par une règle plus forte, et les projets
// qui ajoutent un dossier comme dossier supplémentaire.
// Limite assumée : seules les règles au texte identique sont rapprochées, pas les motifs qui se recouvrent (Bash(git:*) et Bash(git push:*)).

import type { ProjectCfg } from "@/lib/api"

type RulesByKind = Record<string, { rule: string; scope: string }[]>

const KINDS_FROM_WEAKEST = ["allow", "ask", "deny"] as const
const HOME_PREFIX = "~/"

export function overridingKind(perms: RulesByKind, kind: string, rule: string): string | undefined {
  const weakerKindIndex = KINDS_FROM_WEAKEST.findIndex((candidateKind) => candidateKind === kind)
  if (weakerKindIndex < 0) return undefined
  const strongerKinds = KINDS_FROM_WEAKEST.slice(weakerKindIndex + 1).reverse()
  return strongerKinds.find((strongerKind) => (perms[strongerKind] ?? []).some((strongerRule) => strongerRule.rule === rule))
}

const absolutePath = (path: string, home: string) => (path.startsWith(HOME_PREFIX) ? home + path.slice(1) : path)

export function projectsAddingDirectory(projects: ProjectCfg[], target: ProjectCfg, home: string): ProjectCfg[] {
  return projects.filter((otherProject) => {
    if (otherProject.dir === target.dir) return false
    const addedDirectories = otherProject.perms.additionalDirectories ?? []
    return addedDirectories.some((addedDirectory) => absolutePath(addedDirectory.rule, home) === target.project)
  })
}
