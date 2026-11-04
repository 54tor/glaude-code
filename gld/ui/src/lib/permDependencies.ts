// Dépendances entre règles de permission de plusieurs couches : une règle neutralisée par une règle plus forte, et les projets
// qui ajoutent un dossier comme dossier supplémentaire.
// Limite assumée : seules les règles au texte identique sont rapprochées, pas les motifs qui se recouvrent (Bash(git:*) et Bash(git push:*)).

import type { HookInfo, ProjectCfg } from "@/lib/api"

type RulesByKind = Record<string, { rule: string; scope: string }[]>

const KINDS_FROM_WEAKEST = ["allow", "ask", "deny"] as const
const HOME_PREFIX = "~/"
const GLOBAL_SCOPE = "u"

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
    const ownDirectories = (otherProject.perms.additionalDirectories ?? []).filter((addedDirectory) => addedDirectory.scope !== GLOBAL_SCOPE)
    return ownDirectories.some((addedDirectory) => absolutePath(addedDirectory.rule, home) === target.project)
  })
}

const TOOL_NAME_BEFORE_ARGUMENTS = /^([^(\s]+)/

export const toolOfRule = (rule: string) => TOOL_NAME_BEFORE_ARGUMENTS.exec(rule)?.[1] ?? ""

function matcherCoversTool(matcher: string, tool: string): boolean {
  try {
    return new RegExp(`^(?:${matcher})$`).test(tool)
  } catch {
    return false
  }
}

export function hooksCoveringRule(hooks: HookInfo[], rule: string): HookInfo[] {
  const tool = toolOfRule(rule)
  if (!tool) return []
  return hooks.filter((hook) => hook.matcher && hook.matcher !== "*" && matcherCoversTool(hook.matcher, tool))
}

export function skillsUsingRule(skillTools: Record<string, string>, rule: string): string[] {
  const tool = toolOfRule(rule)
  if (!tool) return []
  return Object.entries(skillTools)
    .filter(([, allowedTools]) => allowedTools.split(/[\s,]+/).some((allowedTool) => toolOfRule(allowedTool) === tool))
    .map(([skillName]) => skillName)
}
