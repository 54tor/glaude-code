import { describe, expect, it } from "vitest"
import type { ProjectCfg } from "@/lib/api"
import { hooksCoveringRule, overridingKind, projectsAddingDirectory, skillsUsingRule } from "./permDependencies"

const hookWithMatcher = (matcher: string | undefined, command: string) => ({ scope: "p", file: "p/-p", event: "PreToolUse", matcher, type: "command", command })

describe("hooksCoveringRule", () => {
  const hooks = [hookWithMatcher("Bash", "garde-bash"), hookWithMatcher("Edit|Write", "garde-ecriture"), hookWithMatcher(undefined, "tous"), hookWithMatcher("*", "etoile"), hookWithMatcher("(", "regex-cassee")]

  it("relie une règle aux hooks dont le matcher couvre exactement son outil", () => {
    expect(hooksCoveringRule(hooks, "Bash(git status:*)").map((hookObtenu) => hookObtenu.command)).toEqual(["garde-bash"])
    expect(hooksCoveringRule(hooks, "Write(src/**)").map((hookObtenu) => hookObtenu.command)).toEqual(["garde-ecriture"])
  })

  it("ignore les hooks sans matcher précis et les matchers invalides", () => {
    expect(hooksCoveringRule(hooks, "Read(.env)")).toEqual([])
  })
})

describe("skillsUsingRule", () => {
  const skillTools = { revue: "Read Grep", deploy: "Bash(make:*), Bash(ls:*)" }

  it("relie une règle aux skills qui déclarent son outil dans allowed-tools", () => {
    expect(skillsUsingRule(skillTools, "Bash(rm:*)")).toEqual(["deploy"])
    expect(skillsUsingRule(skillTools, "Grep")).toEqual(["revue"])
  })

  it("ne relie rien quand aucun skill n'utilise l'outil", () => {
    expect(skillsUsingRule(skillTools, "WebFetch(domain:x)")).toEqual([])
  })
})

const projectWith = (dir: string, project: string, additionalDirectories: string[]): ProjectCfg => ({
  dir,
  project,
  files: [],
  perms: { additionalDirectories: additionalDirectories.map((rule) => ({ rule, scope: "l" })) },
  hooks: [],
  mcp: [],
  rules: [],
  skills: [],
  docs: [],
  specific: 0,
})

describe("overridingKind", () => {
  const perms = {
    allow: [{ rule: "Bash(rm:*)", scope: "p" }, { rule: "Bash(ls:*)", scope: "u" }, { rule: "Bash(cat:*)", scope: "p" }],
    ask: [{ rule: "Bash(cat:*)", scope: "l" }],
    deny: [{ rule: "Bash(rm:*)", scope: "u" }],
  }

  it("signale une règle autorisée neutralisée par un refus d'une autre couche", () => {
    expect(overridingKind(perms, "allow", "Bash(rm:*)")).toBe("deny")
  })

  it("préfère le refus à la confirmation quand les deux existent", () => {
    const permsAvecLesDeux = { ...perms, deny: [...perms.deny, { rule: "Bash(cat:*)", scope: "u" }] }
    expect(overridingKind(permsAvecLesDeux, "allow", "Bash(cat:*)")).toBe("deny")
  })

  it("signale une règle autorisée neutralisée par une confirmation", () => {
    expect(overridingKind(perms, "allow", "Bash(cat:*)")).toBe("ask")
  })

  it("ne signale rien pour une règle sans équivalent plus fort ni pour un refus", () => {
    expect(overridingKind(perms, "allow", "Bash(ls:*)")).toBeUndefined()
    expect(overridingKind(perms, "deny", "Bash(rm:*)")).toBeUndefined()
  })
})

describe("projectsAddingDirectory", () => {
  const cible = projectWith("-c", "/home/bob/lib", [])
  const ajouteParTilde = projectWith("-a", "/home/bob/app", ["~/lib"])
  const ajouteParChemin = projectWith("-b", "/home/bob/api", ["/home/bob/lib"])
  const sansLien = projectWith("-d", "/home/bob/autre", ["/home/bob/x"])

  it("liste les projets qui ajoutent le dossier, en chemin absolu ou avec ~", () => {
    const projetsObtenus = projectsAddingDirectory([cible, ajouteParTilde, ajouteParChemin, sansLien], cible, "/home/bob").map((projet) => projet.dir)
    expect(projetsObtenus).toEqual(["-a", "-b"])
  })

  it("ne compte pas le projet lui-même", () => {
    const seAjouteLuiMeme = projectWith("-c", "/home/bob/lib", ["/home/bob/lib"])
    expect(projectsAddingDirectory([seAjouteLuiMeme], seAjouteLuiMeme, "/home/bob")).toEqual([])
  })
})
